// handlers/admin_locks_backfill.go
// ============================================================================
// ADMIN LOCKS BACKFILL HANDLER
// ============================================================================
// One-shot maintenance endpoint that walks every budget in the DB and
// applies the same auto-lock logic the UI runs on load (autoLockPastMonths
// in importConverter.ts):
//
//   for each past month of the budget's currentYear that is not already
//   present in `lockedMonths`, set lockedMonths[<frenchMonthName>] = true
//
// This exists because the persistence bug fixed on 2026-05-15 left existing
// budgets with empty lockedMonths maps. New loads of the UI converge to the
// correct state on their own, but dormant budgets need a server-side push.
//
// POST /api/v1/admin/maintenance/backfill-locks
// Header: X-Admin-Secret
// Body: { "dry_run": bool, "limit": int }
// ============================================================================

package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LovationAdmin/budget-api/services"
	"github.com/LovationAdmin/budget-api/utils"
)

// frMonthsCanonicalFR mirrors the UI's MONTHS array and the backend's
// services.frMonthsCanonical. Keep all three in sync.
var frMonthsCanonicalFR = [12]string{
	"Janvier", "Février", "Mars", "Avril", "Mai", "Juin",
	"Juillet", "Août", "Septembre", "Octobre", "Novembre", "Décembre",
}

const backfillThrottle = 100 * time.Millisecond

type AdminLocksBackfillHandler struct {
	DB     *sql.DB
	Budget *services.BudgetService
}

func NewAdminLocksBackfillHandler(db *sql.DB, budget *services.BudgetService) *AdminLocksBackfillHandler {
	return &AdminLocksBackfillHandler{DB: db, Budget: budget}
}

type backfillRequest struct {
	DryRun bool `json:"dry_run"`
	Limit  int  `json:"limit,omitempty"`
}

type backfillResponse struct {
	DryRun        bool              `json:"dry_run"`
	TotalScanned  int               `json:"total_scanned"`
	Modified      int               `json:"modified"`
	Unchanged     int               `json:"unchanged"`
	Skipped       int               `json:"skipped"`
	Failed        int               `json:"failed"`
	DurationMs    int64             `json:"duration_ms"`
	Failures      []backfillFailure `json:"failures,omitempty"`
}

type backfillFailure struct {
	BudgetID string `json:"budget_id"`
	Reason   string `json:"reason"`
}

// BackfillLocks — POST /api/v1/admin/maintenance/backfill-locks.
func (h *AdminLocksBackfillHandler) BackfillLocks(c *gin.Context) {
	if !requireAdminSecret(c) {
		return
	}

	var req backfillRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	ctx := c.Request.Context()
	start := time.Now()
	res := backfillResponse{DryRun: req.DryRun}

	ids, err := h.listBudgetIDs(ctx, req.Limit)
	if err != nil {
		utils.SafeError("admin/locks-backfill: list budgets failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list budgets failed"})
		return
	}
	res.TotalScanned = len(ids)

	now := time.Now()

	for _, id := range ids {
		select {
		case <-ctx.Done():
			res.DurationMs = time.Since(start).Milliseconds()
			c.JSON(http.StatusOK, res)
			return
		default:
		}

		outcome := h.backfillOne(ctx, id, now, req.DryRun)
		switch outcome.kind {
		case backfillModified:
			res.Modified++
		case backfillUnchanged:
			res.Unchanged++
		case backfillSkipped:
			res.Skipped++
		case backfillFailed:
			res.Failed++
			res.Failures = append(res.Failures, backfillFailure{BudgetID: id, Reason: outcome.reason})
		}

		select {
		case <-ctx.Done():
			res.DurationMs = time.Since(start).Milliseconds()
			c.JSON(http.StatusOK, res)
			return
		case <-time.After(backfillThrottle):
		}
	}

	res.DurationMs = time.Since(start).Milliseconds()
	utils.SafeInfo("admin/locks-backfill: done dry_run=%v scanned=%d modified=%d unchanged=%d skipped=%d failed=%d duration=%dms",
		req.DryRun, res.TotalScanned, res.Modified, res.Unchanged, res.Skipped, res.Failed, res.DurationMs)
	c.JSON(http.StatusOK, res)
}

// ----------------------------------------------------------------------------

type backfillKind int

const (
	backfillUnchanged backfillKind = iota
	backfillModified
	backfillSkipped
	backfillFailed
)

type backfillOutcome struct {
	kind   backfillKind
	reason string
}

func (h *AdminLocksBackfillHandler) backfillOne(ctx context.Context, budgetID string, now time.Time, dryRun bool) backfillOutcome {
	raw, err := h.Budget.GetData(ctx, budgetID)
	if err != nil {
		return backfillOutcome{kind: backfillFailed, reason: "get data: " + err.Error()}
	}
	if raw == nil {
		return backfillOutcome{kind: backfillSkipped, reason: "no data"}
	}

	m, ok := raw.(map[string]interface{})
	if !ok {
		return backfillOutcome{kind: backfillSkipped, reason: "data not an object"}
	}

	currentYear := pickYear(m, now)
	todayYear := now.Year()
	if currentYear > todayYear {
		return backfillOutcome{kind: backfillUnchanged}
	}
	todayMonthIdx := int(now.Month()) - 1

	locked, _ := m["lockedMonths"].(map[string]interface{})
	if locked == nil {
		locked = map[string]interface{}{}
	}

	changed := false
	for idx, month := range frMonthsCanonicalFR {
		isPast := currentYear < todayYear || idx < todayMonthIdx
		if !isPast {
			continue
		}
		if _, exists := locked[month]; exists {
			continue
		}
		locked[month] = true
		changed = true
	}

	if !changed {
		return backfillOutcome{kind: backfillUnchanged}
	}

	if dryRun {
		return backfillOutcome{kind: backfillModified}
	}

	m["lockedMonths"] = locked
	if err := h.Budget.UpdateData(ctx, budgetID, m, "", "system-backfill"); err != nil {
		return backfillOutcome{kind: backfillFailed, reason: "update data: " + err.Error()}
	}
	return backfillOutcome{kind: backfillModified}
}

// pickYear returns the year the auto-lock should anchor on. We prefer the
// budget's stored `currentYear` (matching the UI's behaviour on load); if
// missing or non-numeric, we fall back to the current calendar year.
func pickYear(m map[string]interface{}, now time.Time) int {
	switch v := m["currentYear"].(type) {
	case float64:
		if int(v) > 0 {
			return int(v)
		}
	case int:
		if v > 0 {
			return v
		}
	}
	return now.Year()
}

func (h *AdminLocksBackfillHandler) listBudgetIDs(ctx context.Context, limit int) ([]string, error) {
	q := `SELECT id::text FROM budgets ORDER BY updated_at DESC`
	args := []any{}
	if limit > 0 {
		q += " LIMIT $1"
		args = append(args, limit)
	}

	queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	rows, err := h.DB.QueryContext(queryCtx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

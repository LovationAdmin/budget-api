// handlers/admin_monthly_recap.go
// ============================================================================
// ADMIN MONTHLY RECAP HANDLER
// ============================================================================
// POST /api/v1/admin/campaigns/monthly-recap
//
// Sends one monthly recap email per verified user, scoped to the user's most
// recently updated budget. The same handler is also invoked by the in-process
// scheduler (main.go) so we keep the work loop in one place.
//
// Idempotency: each send is recorded in email_campaign_sends with the
// campaign_id `monthly_recap_YYYY_MM` (where YYYY_MM is the month that just
// ended). Re-running the endpoint with skip_sent=true is therefore safe.
// ============================================================================

package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LovationAdmin/budget-api/services"
	"github.com/LovationAdmin/budget-api/utils"
)

// AdminMonthlyRecapHandler exposes the manual trigger for the monthly recap
// campaign. The scheduler (main.go) calls RunMonthlyRecap directly without
// going through HTTP.
type AdminMonthlyRecapHandler struct {
	DB           *sql.DB
	EmailService *services.EmailService
	RecapService *services.MonthlyRecapService
}

func NewAdminMonthlyRecapHandler(db *sql.DB, email *services.EmailService, recap *services.MonthlyRecapService) *AdminMonthlyRecapHandler {
	return &AdminMonthlyRecapHandler{DB: db, EmailService: email, RecapService: recap}
}

type monthlyRecapRequest struct {
	// CampaignID lets the operator override the auto-generated identifier
	// (`monthly_recap_YYYY_MM`). Useful for backfills.
	CampaignID string `json:"campaign_id,omitempty"`
	// DryRun renders the email but does not send.
	DryRun bool `json:"dry_run"`
	// SkipSent skips users already recorded under the same campaign_id.
	SkipSent bool `json:"skip_sent"`
	// Limit caps the number of recipients (useful for tests).
	Limit int `json:"limit,omitempty"`
	// Anchor is an optional ISO date ("YYYY-MM-DD") used to set the "now"
	// reference; defaults to time.Now(). Useful to re-run a past month.
	Anchor string `json:"anchor,omitempty"`
}

type monthlyRecapResponse struct {
	CampaignID string                  `json:"campaign_id"`
	DryRun     bool                    `json:"dry_run"`
	Total      int                     `json:"total"`
	Sent       int                     `json:"sent"`
	Skipped    int                     `json:"skipped"`
	Failed     int                     `json:"failed"`
	DurationMs int64                   `json:"duration_ms"`
	Failures   []monthlyRecapFailure   `json:"failures,omitempty"`
}

type monthlyRecapFailure struct {
	UserID string `json:"user_id"`
	Reason string `json:"reason"`
}

const monthlyRecapThrottle = 150 * time.Millisecond

// SendMonthlyRecap — POST /api/v1/admin/campaigns/monthly-recap.
// Re-uses requireAdminSecret() (defined alongside the other admin endpoints).
func (h *AdminMonthlyRecapHandler) SendMonthlyRecap(c *gin.Context) {
	if !requireAdminSecret(c) {
		return
	}

	var req monthlyRecapRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	anchor := time.Now()
	if req.Anchor != "" {
		if t, err := time.Parse("2006-01-02", req.Anchor); err == nil {
			anchor = t
		}
	}

	campaignID := req.CampaignID
	if campaignID == "" {
		// The "reporting month" is the month that just ended.
		prev := anchor.AddDate(0, 0, -1)
		campaignID = fmt.Sprintf("monthly_recap_%04d_%02d", prev.Year(), int(prev.Month()))
	}

	result := h.RunMonthlyRecap(c.Request.Context(), MonthlyRecapRunOptions{
		CampaignID: campaignID,
		DryRun:     req.DryRun,
		SkipSent:   req.SkipSent,
		Limit:      req.Limit,
		Anchor:     anchor,
	})

	c.JSON(http.StatusOK, result)
}

// MonthlyRecapRunOptions are exported so the scheduler in main.go can build
// them without going through HTTP.
type MonthlyRecapRunOptions struct {
	CampaignID string
	DryRun     bool
	SkipSent   bool
	Limit      int
	Anchor     time.Time
}

// RunMonthlyRecap drives the send loop. Exported so the scheduler can call it
// with a background context.
func (h *AdminMonthlyRecapHandler) RunMonthlyRecap(ctx context.Context, opts MonthlyRecapRunOptions) *monthlyRecapResponse {
	start := time.Now()
	out := &monthlyRecapResponse{
		CampaignID: opts.CampaignID,
		DryRun:     opts.DryRun,
	}

	users, err := h.RecapService.ListVerifiedUsers(ctx, opts.Limit)
	if err != nil {
		utils.SafeError("monthly-recap: list verified users failed: %v", err)
		out.Failures = append(out.Failures, monthlyRecapFailure{UserID: "-", Reason: "list verified: " + err.Error()})
		out.DurationMs = time.Since(start).Milliseconds()
		return out
	}
	out.Total = len(users)

	appURL := frontendURLOrDefault()

	for _, u := range users {
		if opts.SkipSent {
			already, aerr := h.alreadySent(ctx, opts.CampaignID, u.ID)
			if aerr != nil {
				utils.SafeWarn("monthly-recap: AlreadySent check failed for %s: %v", u.ID, aerr)
			} else if already {
				out.Skipped++
				continue
			}
		}

		bs, count, perr := h.RecapService.PickRecapBudget(ctx, u.ID)
		if perr != nil {
			out.Failed++
			out.Failures = append(out.Failures, monthlyRecapFailure{UserID: u.ID, Reason: "pick budget: " + perr.Error()})
			continue
		}
		if bs == nil {
			// No budget yet — nothing to recap.
			out.Skipped++
			continue
		}

		recap, rerr := h.RecapService.BuildRecap(ctx, u, bs, count, opts.Anchor, opts.CampaignID, appURL)
		if rerr != nil {
			out.Failed++
			out.Failures = append(out.Failures, monthlyRecapFailure{UserID: u.ID, Reason: "build recap: " + rerr.Error()})
			_ = h.recordSend(ctx, opts.CampaignID, u.ID, "failed", "", rerr.Error())
			continue
		}

		if opts.DryRun {
			utils.SafeInfo("monthly-recap: dry-run send campaign=%s to=%s", opts.CampaignID, utils.MaskEmailForLog(u.Email))
			out.Sent++
			h.throttle(ctx)
			continue
		}

		msgID, serr := h.EmailService.SendMonthlyRecapEmail(u.Email, recap.Locale, recap.CurrentMonth.Label, recap.BudgetName, recap)
		if serr != nil {
			out.Failed++
			out.Failures = append(out.Failures, monthlyRecapFailure{UserID: u.ID, Reason: serr.Error()})
			_ = h.recordSend(ctx, opts.CampaignID, u.ID, "failed", "", serr.Error())
			utils.SafeError("monthly-recap: send failed campaign=%s user=%s: %v", opts.CampaignID, u.ID, serr)
		} else {
			out.Sent++
			_ = h.recordSend(ctx, opts.CampaignID, u.ID, "sent", msgID, "")
			utils.SafeInfo("monthly-recap: sent campaign=%s to=%s msg_id=%s",
				opts.CampaignID, utils.MaskEmailForLog(u.Email), msgID)
		}

		if err := h.throttle(ctx); err != nil {
			out.DurationMs = time.Since(start).Milliseconds()
			return out
		}
	}

	out.DurationMs = time.Since(start).Milliseconds()
	return out
}

func (h *AdminMonthlyRecapHandler) recordSend(ctx context.Context, campaignID, userID, status, msgID, errMsg string) error {
	const q = `
		INSERT INTO email_campaign_sends
			(campaign_id, user_id, status, provider_msg_id, error_message)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''))
		ON CONFLICT (campaign_id, user_id) DO UPDATE
		SET status = EXCLUDED.status,
		    provider_msg_id = EXCLUDED.provider_msg_id,
		    error_message = EXCLUDED.error_message,
		    created_at = NOW()
	`
	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := h.DB.ExecContext(queryCtx, q, campaignID, userID, status, msgID, errMsg)
	return err
}

func (h *AdminMonthlyRecapHandler) alreadySent(ctx context.Context, campaignID, userID string) (bool, error) {
	const q = `
		SELECT 1 FROM email_campaign_sends
		WHERE campaign_id = $1 AND user_id = $2 AND status = 'sent'
		LIMIT 1
	`
	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var one int
	err := h.DB.QueryRowContext(queryCtx, q, campaignID, userID).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (h *AdminMonthlyRecapHandler) throttle(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(monthlyRecapThrottle):
		return nil
	}
}

// frontendURLOrDefault mirrors the helper used by the campaign render path so
// CTA buttons land on the right host.
func frontendURLOrDefault() string {
	if v := os.Getenv("FRONTEND_URL"); v != "" {
		return v
	}
	return "http://localhost:3000"
}

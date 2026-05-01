// handlers/admin_campaigns.go
// ============================================================================
// ADMIN CAMPAIGNS HANDLER
// ============================================================================
// Endpoint protégé par ADMIN_SECRET (header X-Admin-Secret) qui envoie une
// campagne d'emails de réengagement.
//
// POST /api/v1/admin/campaigns/send
//
// Modes :
//   - auto:true   → envoie aux deux segments (verified + unverified) avec leurs
//                   templates respectifs en un seul appel.
//   - auto:false  → mode manuel, exige type + segment explicites (utile pour
//                   tester un seul template à la fois).
//
// Idempotence : la table email_campaign_sends a un index unique sur
// (campaign_id, user_id), donc relancer la même requête ne double-envoie pas
// quand skip_sent:true.
//
// Throttling : 150ms entre chaque envoi pour rester sous la limite Resend
// (10/sec en free tier).
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

// AdminCampaignsHandler gère les endpoints /admin/campaigns/*.
type AdminCampaignsHandler struct {
	DB           *sql.DB
	EmailService *services.EmailService
}

// NewAdminCampaignsHandler crée le handler.
func NewAdminCampaignsHandler(db *sql.DB, emailService *services.EmailService) *AdminCampaignsHandler {
	return &AdminCampaignsHandler{DB: db, EmailService: emailService}
}

// ============================================================================
// REQUEST / RESPONSE TYPES
// ============================================================================

type sendCampaignRequest struct {
	CampaignID string `json:"campaign_id" binding:"required"`
	Auto       bool   `json:"auto"`

	// Required only when Auto == false.
	Variant string `json:"variant,omitempty"` // "reengagement_verified" | "reengagement_unverified"
	Segment string `json:"segment,omitempty"` // "verified" | "unverified"

	DryRun   bool `json:"dry_run"`
	SkipSent bool `json:"skip_sent"`
	Limit    int  `json:"limit,omitempty"`
}

type segmentResult struct {
	CampaignID string         `json:"campaign_id"`
	Variant    string         `json:"variant"`
	Segment    string         `json:"segment"`
	DryRun     bool           `json:"dry_run"`
	Total      int            `json:"total"`
	Sent       int            `json:"sent"`
	Skipped    int            `json:"skipped"`
	Failed     int            `json:"failed"`
	DurationMs int64          `json:"duration_ms"`
	Failures   []failureEntry `json:"failures,omitempty"`
}

type failureEntry struct {
	UserID string `json:"user_id"`
	Reason string `json:"reason"`
}

type sendCampaignResponse struct {
	CampaignID string         `json:"campaign_id"`
	Auto       bool           `json:"auto"`
	Verified   *segmentResult `json:"verified,omitempty"`
	Unverified *segmentResult `json:"unverified,omitempty"`
	Single     *segmentResult `json:"single,omitempty"`
}

// ============================================================================
// HANDLER
// ============================================================================

// SendReengagementCampaign — POST /api/v1/admin/campaigns/send.
// Reuses requireAdminSecret() from handlers/admin_stats.go (same package).
func (h *AdminCampaignsHandler) SendReengagementCampaign(c *gin.Context) {
	if !requireAdminSecret(c) {
		return
	}

	var req sendCampaignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	ctx := c.Request.Context()

	// ── Auto mode: fan out to both segments with matching templates ───────
	if req.Auto {
		verified := h.runSegment(ctx, segmentInput{
			CampaignID: req.CampaignID + "_verified",
			Variant:    utils.CampaignReengagementVerified,
			Segment:    "verified",
			DryRun:     req.DryRun,
			SkipSent:   req.SkipSent,
			Limit:      req.Limit,
		})
		unverified := h.runSegment(ctx, segmentInput{
			CampaignID: req.CampaignID + "_unverified",
			Variant:    utils.CampaignReengagementUnverified,
			Segment:    "unverified",
			DryRun:     req.DryRun,
			SkipSent:   req.SkipSent,
			Limit:      req.Limit,
		})
		c.JSON(http.StatusOK, sendCampaignResponse{
			CampaignID: req.CampaignID,
			Auto:       true,
			Verified:   verified,
			Unverified: unverified,
		})
		return
	}

	// ── Manual mode: one segment, one template ─────────────────────────────
	if req.Variant == "" || req.Segment == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "manual mode requires both 'variant' and 'segment' (or set 'auto': true)",
		})
		return
	}
	if req.Segment != "verified" && req.Segment != "unverified" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "segment must be 'verified' or 'unverified'"})
		return
	}

	single := h.runSegment(ctx, segmentInput{
		CampaignID: req.CampaignID,
		Variant:    utils.CampaignVariant(req.Variant),
		Segment:    req.Segment,
		DryRun:     req.DryRun,
		SkipSent:   req.SkipSent,
		Limit:      req.Limit,
	})
	c.JSON(http.StatusOK, sendCampaignResponse{
		CampaignID: req.CampaignID,
		Auto:       false,
		Single:     single,
	})
}

// ============================================================================
// CORE SEGMENT RUNNER
// ============================================================================

type segmentInput struct {
	CampaignID string
	Variant    utils.CampaignVariant
	Segment    string // "verified" | "unverified"
	DryRun     bool
	SkipSent   bool
	Limit      int
}

type targetUser struct {
	ID    string
	Email string
	Name  string
}

const sendThrottle = 150 * time.Millisecond

func (h *AdminCampaignsHandler) runSegment(ctx context.Context, in segmentInput) *segmentResult {
	start := time.Now()
	res := &segmentResult{
		CampaignID: in.CampaignID,
		Variant:    string(in.Variant),
		Segment:    in.Segment,
		DryRun:     in.DryRun,
	}

	targets, err := h.listTargets(ctx, in.Segment, in.Limit)
	if err != nil {
		utils.SafeError("admin/campaigns: list targets failed: %v", err)
		res.Failures = append(res.Failures, failureEntry{UserID: "-", Reason: "list targets: " + err.Error()})
		res.DurationMs = time.Since(start).Milliseconds()
		return res
	}
	res.Total = len(targets)

	for _, t := range targets {
		if in.SkipSent {
			already, aerr := h.alreadySent(ctx, in.CampaignID, t.ID)
			if aerr != nil {
				utils.SafeWarn("admin/campaigns: AlreadySent check failed for %s: %v", t.ID, aerr)
			} else if already {
				res.Skipped++
				continue
			}
		}

		// Render the email body once per user (name varies).
		_, _, renderErr := utils.RenderCampaignEmail(in.Variant, t.Name, in.CampaignID)
		if renderErr != nil {
			res.Failed++
			res.Failures = append(res.Failures, failureEntry{UserID: t.ID, Reason: "render: " + renderErr.Error()})
			utils.SafeError("admin/campaigns: render failed for user %s: %v", t.ID, renderErr)
			continue
		}

		if in.DryRun {
			utils.SafeInfo("admin/campaigns: dry-run send campaign=%s to=%s", in.CampaignID, utils.MaskEmailForLog(t.Email))
			res.Sent++
			h.throttle(ctx)
			continue
		}

		msgID, sendErr := h.EmailService.SendReengagementEmail(t.Email, t.Name, in.CampaignID, in.Variant)
		if sendErr != nil {
			res.Failed++
			res.Failures = append(res.Failures, failureEntry{UserID: t.ID, Reason: sendErr.Error()})
			_ = h.recordSend(ctx, in.CampaignID, t.ID, "failed", "", sendErr.Error())
			utils.SafeError("admin/campaigns: send failed campaign=%s user=%s: %v", in.CampaignID, t.ID, sendErr)
		} else {
			res.Sent++
			_ = h.recordSend(ctx, in.CampaignID, t.ID, "sent", msgID, "")
			utils.SafeInfo("admin/campaigns: sent campaign=%s to=%s msg_id=%s",
				in.CampaignID, utils.MaskEmailForLog(t.Email), msgID)
		}

		if err := h.throttle(ctx); err != nil {
			res.DurationMs = time.Since(start).Milliseconds()
			return res
		}
	}

	res.DurationMs = time.Since(start).Milliseconds()
	return res
}

// ============================================================================
// DB QUERIES (raw SQL, matching the codebase pattern)
// ============================================================================

func (h *AdminCampaignsHandler) listTargets(ctx context.Context, segment string, limit int) ([]targetUser, error) {
	q := `
		SELECT id::text, email, COALESCE(name, '')
		FROM users
		WHERE email_verified = $1
		ORDER BY created_at ASC
	`
	args := []any{segment == "verified"}
	if limit > 0 {
		q += " LIMIT $2"
		args = append(args, limit)
	}

	queryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	rows, err := h.DB.QueryContext(queryCtx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]targetUser, 0)
	for rows.Next() {
		var t targetUser
		if err := rows.Scan(&t.ID, &t.Email, &t.Name); err != nil {
			return nil, err
		}
		// Skip users whose email is empty for any reason — defensive.
		if t.Email == "" {
			continue
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (h *AdminCampaignsHandler) recordSend(ctx context.Context, campaignID, userID, status, msgID, errMsg string) error {
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

func (h *AdminCampaignsHandler) alreadySent(ctx context.Context, campaignID, userID string) (bool, error) {
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

// throttle waits sendThrottle to stay under Resend rate limit.
func (h *AdminCampaignsHandler) throttle(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(sendThrottle):
		return nil
	}
}

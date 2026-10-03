// handlers/admin_growth.go
// ============================================================================
// ADMIN STATS — growth: the signup funnel, weekly signups, activity, where
// budgets come from and how campaigns went. Read-only, aggregated counts
// only (no personal data leaves the database).
// ============================================================================

package handlers

import (
	"context"
	"encoding/json"

	"github.com/pkg/errors"

	"github.com/LovationAdmin/budget-api/utils"
)

// GrowthStats answers "are we growing, and where do people drop off?".
type GrowthStats struct {
	Funnel              FunnelStats     `json:"funnel"`
	Weekly              []WeekPoint     `json:"weekly"`
	ActiveUsers7Days    int             `json:"active_users_7_days"`
	ActiveUsers30Days   int             `json:"active_users_30_days"`
	UnverifiedOver7Days int             `json:"unverified_over_7_days"`
	Locations           []LocationCount `json:"locations"`
	Campaigns           []CampaignStat  `json:"campaigns"`
}

// FunnelStats counts users at each step, from signup to collaboration.
// "Active" means one of their budgets was opened or changed in 30 days.
type FunnelStats struct {
	SignedUp      int `json:"signed_up"`
	Verified      int `json:"verified"`
	WithBudget    int `json:"with_budget"`
	Active30Days  int `json:"active_30_days"`
	Collaborating int `json:"collaborating"`
}

// WeekPoint is one week (Monday) of signups, the last 12 weeks, zero-filled.
type WeekPoint struct {
	Week     string `json:"week"`
	Signups  int    `json:"signups"`
	Verified int    `json:"verified"`
}

// LocationCount is the number of budgets per country code.
type LocationCount struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

// CampaignStat sums one email campaign.
type CampaignStat struct {
	ID     string `json:"id"`
	Sent   int    `json:"sent"`
	Failed int    `json:"failed"`
	LastAt string `json:"last_at"`
}

const activeBudgetUsersSQL = `
	SELECT COUNT(DISTINCT m.user_id)
	FROM budget_members m
	JOIN budgets b ON b.id = m.budget_id
	LEFT JOIN budget_data d ON d.budget_id = b.id
	WHERE b.last_viewed_at > NOW() - $1::interval OR d.updated_at > NOW() - $1::interval`

func (h *AdminStatsHandler) growth(ctx context.Context) (GrowthStats, error) {
	var g GrowthStats
	q := h.DB.QueryRowContext(ctx, `
		WITH u AS (
			SELECT u.id, u.email_verified, u.created_at,
				EXISTS (SELECT 1 FROM budget_members m WHERE m.user_id = u.id)
					OR EXISTS (SELECT 1 FROM budgets b WHERE b.owner_id = u.id) AS has_budget,
				EXISTS (
					SELECT 1 FROM budget_members m
					WHERE m.user_id = u.id
					  AND (SELECT COUNT(*) FROM budget_members x WHERE x.budget_id = m.budget_id) >= 2
				) AS collaborating
			FROM users u
		)
		SELECT COUNT(*),
			COUNT(*) FILTER (WHERE email_verified),
			COUNT(*) FILTER (WHERE has_budget),
			COUNT(*) FILTER (WHERE collaborating),
			COUNT(*) FILTER (WHERE NOT email_verified AND created_at < NOW() - INTERVAL '7 days')
		FROM u`)
	if err := q.Scan(&g.Funnel.SignedUp, &g.Funnel.Verified, &g.Funnel.WithBudget, &g.Funnel.Collaborating, &g.UnverifiedOver7Days); err != nil {
		return g, errors.Wrap(err, "funnel")
	}
	if err := h.DB.QueryRowContext(ctx, activeBudgetUsersSQL, "30 days").Scan(&g.ActiveUsers30Days); err != nil {
		return g, errors.Wrap(err, "active 30 days")
	}
	if err := h.DB.QueryRowContext(ctx, activeBudgetUsersSQL, "7 days").Scan(&g.ActiveUsers7Days); err != nil {
		return g, errors.Wrap(err, "active 7 days")
	}
	g.Funnel.Active30Days = g.ActiveUsers30Days

	var weekly, locations, campaigns []byte
	if err := h.DB.QueryRowContext(ctx, `
		SELECT
			COALESCE((SELECT json_agg(t ORDER BY t.week) FROM (
				SELECT to_char(w, 'YYYY-MM-DD') AS week,
					COUNT(u.id) AS signups,
					COUNT(u.id) FILTER (WHERE u.email_verified) AS verified
				FROM generate_series(date_trunc('week', NOW()) - INTERVAL '11 weeks', date_trunc('week', NOW()), INTERVAL '1 week') w
				LEFT JOIN users u ON date_trunc('week', u.created_at) = w
				GROUP BY w
			) t), '[]'),
			COALESCE((SELECT json_agg(t) FROM (
				SELECT COALESCE(NULLIF(UPPER(location), ''), '?') AS code, COUNT(*) AS count
				FROM budgets GROUP BY 1 ORDER BY 2 DESC LIMIT 8
			) t), '[]'),
			COALESCE((SELECT json_agg(t ORDER BY t.last_at DESC) FROM (
				SELECT campaign_id AS id,
					COUNT(*) FILTER (WHERE status = 'sent') AS sent,
					COUNT(*) FILTER (WHERE status = 'failed') AS failed,
					to_char(MAX(created_at), 'YYYY-MM-DD') AS last_at
				FROM email_campaign_sends GROUP BY campaign_id
				ORDER BY MAX(created_at) DESC LIMIT 8
			) t), '[]')
	`).Scan(&weekly, &locations, &campaigns); err != nil {
		return g, errors.Wrap(err, "series")
	}
	if err := json.Unmarshal(weekly, &g.Weekly); err != nil {
		return g, errors.Wrap(err, "weekly json")
	}
	if err := json.Unmarshal(locations, &g.Locations); err != nil {
		return g, errors.Wrap(err, "locations json")
	}
	if err := json.Unmarshal(campaigns, &g.Campaigns); err != nil {
		return g, errors.Wrap(err, "campaigns json")
	}
	return g, nil
}

// attachGrowth fills resp.Growth, logging (not failing) on error.
func (h *AdminStatsHandler) attachGrowth(ctx context.Context, resp *StatsResponse) {
	g, err := h.growth(ctx)
	if err != nil {
		utils.SafeWarn("admin/stats: growth failed: %v", err)
		return
	}
	resp.Growth = &g
}

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
	Funnel              FunnelStats      `json:"funnel"`
	Weekly              []WeekPoint      `json:"weekly"`
	ActiveUsers7Days    int              `json:"active_users_7_days"`
	ActiveUsers30Days   int              `json:"active_users_30_days"`
	UnverifiedOver7Days int              `json:"unverified_over_7_days"`
	Locations           []LocationCount  `json:"locations"`
	Campaigns           []CampaignStat   `json:"campaigns"`
	Acquisition         AcquisitionStats `json:"acquisition"`
}

// AcquisitionStats: over the last 90 days, what each source (search engine,
// AI assistant, social network…) brought, from anonymous events
// (acquisition.go). Since is the first event's day ("" before any).
type AcquisitionStats struct {
	Since    string           `json:"since"`
	Sources  []AcquisitionRow `json:"sources"`
	Landings []LandingRow     `json:"landings"`
}

// AcquisitionRow is one source: template downloads (PDF, Excel) and signups.
type AcquisitionRow struct {
	Source         string `json:"source"`
	Downloads      int    `json:"downloads"`
	DownloadsPDF   int    `json:"downloads_pdf"`
	DownloadsExcel int    `json:"downloads_excel"`
	Signups        int    `json:"signups"`
}

// LandingRow is the first page seen by people who then signed up.
type LandingRow struct {
	Page    string `json:"page"`
	Signups int    `json:"signups"`
}

// FunnelStats counts users at each step, from signup to collaboration.
// "Active" means the person has a budget and opened the app in the period —
// a session issued or renewed then (access tokens last 15 minutes, so any
// visit renews it). Counted per person: a shared budget opened by one member
// doesn't make the others active.
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
				) AS collaborating,
				(SELECT MAX(t.issued_at) FROM refresh_tokens t WHERE t.user_id = u.id) AS seen_at
			FROM users u
		)
		SELECT COUNT(*),
			COUNT(*) FILTER (WHERE email_verified),
			COUNT(*) FILTER (WHERE has_budget),
			COUNT(*) FILTER (WHERE collaborating),
			COUNT(*) FILTER (WHERE NOT email_verified AND created_at < NOW() - INTERVAL '7 days'),
			COUNT(*) FILTER (WHERE has_budget AND seen_at > NOW() - INTERVAL '30 days'),
			COUNT(*) FILTER (WHERE has_budget AND seen_at > NOW() - INTERVAL '7 days')
		FROM u`)
	if err := q.Scan(&g.Funnel.SignedUp, &g.Funnel.Verified, &g.Funnel.WithBudget, &g.Funnel.Collaborating,
		&g.UnverifiedOver7Days, &g.ActiveUsers30Days, &g.ActiveUsers7Days); err != nil {
		return g, errors.Wrap(err, "funnel")
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
	acquisition, err := h.acquisition(ctx)
	if err != nil {
		return g, err
	}
	g.Acquisition = acquisition
	return g, nil
}

func (h *AdminStatsHandler) acquisition(ctx context.Context) (AcquisitionStats, error) {
	a := AcquisitionStats{Sources: []AcquisitionRow{}, Landings: []LandingRow{}}
	var sources, landings []byte
	if err := h.DB.QueryRowContext(ctx, `
		WITH e AS (SELECT * FROM acquisition_events WHERE created_at > NOW() - INTERVAL '90 days')
		SELECT
			COALESCE((SELECT to_char(MIN(created_at), 'YYYY-MM-DD') FROM acquisition_events), ''),
			COALESCE((SELECT json_agg(t ORDER BY t.signups DESC, t.downloads DESC, t.source) FROM (
				SELECT source,
					COUNT(*) FILTER (WHERE kind = 'download') AS downloads,
					COUNT(*) FILTER (WHERE kind = 'download' AND detail LIKE '%.pdf') AS downloads_pdf,
					COUNT(*) FILTER (WHERE kind = 'download' AND detail LIKE '%.xlsx') AS downloads_excel,
					COUNT(*) FILTER (WHERE kind = 'signup') AS signups
				FROM e GROUP BY source
			) t), '[]'),
			COALESCE((SELECT json_agg(t ORDER BY t.signups DESC, t.page) FROM (
				SELECT detail AS page, COUNT(*) AS signups
				FROM e WHERE kind = 'signup' AND detail <> ''
				GROUP BY detail ORDER BY COUNT(*) DESC, detail LIMIT 6
			) t), '[]')
	`).Scan(&a.Since, &sources, &landings); err != nil {
		return a, errors.Wrap(err, "acquisition")
	}
	if err := json.Unmarshal(sources, &a.Sources); err != nil {
		return a, errors.Wrap(err, "acquisition sources json")
	}
	if err := json.Unmarshal(landings, &a.Landings); err != nil {
		return a, errors.Wrap(err, "acquisition landings json")
	}
	return a, nil
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

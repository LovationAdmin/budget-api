// handlers/acquisition.go
// ============================================================================
// ACQUISITION — where visitors come from, counted without identifying anyone.
// The site remembers, in the visitor's browser, the search engine or site of
// their first visit (a category: "bing", "google"…). It is sent here when they
// download a free template and when they create an account, and stored as an
// anonymous event: no account, IP or identifier, so the admin page can tell
// which sources bring downloads and signups.
// ============================================================================

package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/LovationAdmin/budget-api/utils"
)

// acquisitionSources are the categories the site sends; anything else is "other".
var acquisitionSources = map[string]bool{
	"google": true, "bing": true, "chatgpt": true, "copilot": true, "perplexity": true,
	"duckduckgo": true, "ecosia": true, "qwant": true, "yahoo": true, "other_search": true,
	"social": true, "email": true, "referral": true, "direct": true,
}

// templateFiles are the free templates (public/telechargements in the site).
var templateFiles = map[string]bool{
	"tableau-budget-familial-budgetfamille.xlsx": true,
	"tableau-budget-familial-a-imprimer.pdf":     true,
	"budget-template.xlsx":                       true,
	"budget-template-printable.pdf":              true,
}

// AcquisitionTouch is the first visit's source and landing page, as the site sends it.
type AcquisitionTouch struct {
	Source  string `json:"source"`
	Landing string `json:"landing"`
}

func normalizeSource(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if acquisitionSources[s] {
		return s
	}
	return "other"
}

// normalizeLanding keeps a site path only: no query string, no host, bounded.
func normalizeLanding(p string) string {
	p = strings.TrimSpace(p)
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") {
		return ""
	}
	if len(p) > 120 {
		p = p[:120]
	}
	return p
}

// recordAcquisition stores one anonymous event; failures are logged, never shown.
func recordAcquisition(ctx context.Context, db *sql.DB, kind, source, detail string) {
	if _, err := db.ExecContext(ctx,
		`INSERT INTO acquisition_events (kind, source, detail) VALUES ($1, $2, $3)`,
		kind, normalizeSource(source), detail); err != nil {
		utils.SafeWarn("acquisition: %s not recorded: %v", kind, err)
	}
}

// AcquisitionHandler counts template downloads (public, rate-limited).
type AcquisitionHandler struct {
	DB *sql.DB
}

type trackDownloadRequest struct {
	File   string `json:"file"`
	Source string `json:"source"`
}

// TrackDownload records a download of a free template. The site sends it with
// navigator.sendBeacon (text/plain, so no CORS preflight): the body is JSON
// whatever its Content-Type.
func (h *AcquisitionHandler) TrackDownload(c *gin.Context) {
	var req trackDownloadRequest
	if err := c.ShouldBindJSON(&req); err != nil || !templateFiles[req.File] {
		c.Status(http.StatusBadRequest)
		return
	}
	recordAcquisition(c.Request.Context(), h.DB, "download", req.Source, req.File)
	c.Status(http.StatusNoContent)
}

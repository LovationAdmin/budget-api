// handlers/admin_stats.go
// ============================================================================
// ADMIN STATS HANDLER
// ============================================================================
// Endpoint protégé par ADMIN_SECRET (header X-Admin-Secret) qui retourne
// un instantané de l'état de la base.
//
// Utilisation : curl, Sentry/Grafana cron, ou simple dashboard maison.
//
// Toutes les requêtes ont un timeout de 5s pour éviter de bloquer la DB
// si une requête traîne.
// ============================================================================

package handlers

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LovationAdmin/budget-api/utils"
)

// AdminStatsHandler gère l'endpoint /admin/stats.
type AdminStatsHandler struct {
	DB *sql.DB
}

// NewAdminStatsHandler crée le handler.
func NewAdminStatsHandler(db *sql.DB) *AdminStatsHandler {
	return &AdminStatsHandler{DB: db}
}

// ============================================================================
// AUTH
// ============================================================================

// requireAdminSecret vérifie le header X-Admin-Secret en temps constant
// (évite les timing attacks).
func requireAdminSecret(c *gin.Context) bool {
	expected := os.Getenv("ADMIN_SECRET")
	if expected == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Admin endpoints disabled (ADMIN_SECRET not configured)",
		})
		return false
	}

	provided := c.GetHeader("X-Admin-Secret")
	// subtle.ConstantTimeCompare : timing-safe
	if subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) != 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return false
	}
	return true
}

// ============================================================================
// HANDLER
// ============================================================================

// StatsResponse est le payload retourné par /admin/stats.
type StatsResponse struct {
	GeneratedAt   time.Time   `json:"generated_at"`
	Environment   string      `json:"environment"`
	Users         UserStats   `json:"users"`
	Budgets       BudgetStats `json:"budgets"`
	Sessions      SessionStats `json:"sessions"`
	Suggestions   CacheStats  `json:"suggestions_cache"`
}

type UserStats struct {
	Total           int `json:"total"`
	Verified        int `json:"verified"`
	WithTOTP        int `json:"with_totp"`
	NewLast24h      int `json:"new_last_24h"`
	ActiveLast7Days int `json:"active_last_7_days"`
}

type BudgetStats struct {
	Total           int `json:"total"`
	UpdatedLast24h  int `json:"updated_last_24h"`
	UpdatedLast7Days int `json:"updated_last_7_days"`
}

type SessionStats struct {
	ActiveRefreshTokens   int `json:"active_refresh_tokens"`
	UniqueUsersWithSession int `json:"unique_users_with_session"`
	IssuedLast24h         int `json:"issued_last_24h"`
}

type CacheStats struct {
	Total       int `json:"total"`
	NotExpired  int `json:"not_expired"`
}

// GetStats retourne un snapshot de la base.
// GET /api/v1/admin/stats — header X-Admin-Secret requis.
func (h *AdminStatsHandler) GetStats(c *gin.Context) {
	if !requireAdminSecret(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp := StatsResponse{
		GeneratedAt: time.Now(),
		Environment: utils.GetEnvMode(),
	}

	// USER STATS
	if err := h.DB.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE email_verified = true),
			COUNT(*) FILTER (WHERE totp_enabled = true),
			COUNT(*) FILTER (WHERE created_at > NOW() - INTERVAL '24 hours')
		FROM users
	`).Scan(&resp.Users.Total, &resp.Users.Verified, &resp.Users.WithTOTP, &resp.Users.NewLast24h); err != nil {
		utils.SafeWarn("admin/stats: user stats failed: %v", err)
	}

	// Active = a un refresh token non révoqué émis dans les 7 jours
	if err := h.DB.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT user_id)
		FROM refresh_tokens
		WHERE revoked_at IS NULL
		  AND issued_at > NOW() - INTERVAL '7 days'
	`).Scan(&resp.Users.ActiveLast7Days); err != nil {
		utils.SafeWarn("admin/stats: active users failed: %v", err)
	}

	// BUDGET STATS
	if err := h.DB.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE updated_at > NOW() - INTERVAL '24 hours'),
			COUNT(*) FILTER (WHERE updated_at > NOW() - INTERVAL '7 days')
		FROM budgets
	`).Scan(&resp.Budgets.Total, &resp.Budgets.UpdatedLast24h, &resp.Budgets.UpdatedLast7Days); err != nil {
		utils.SafeWarn("admin/stats: budget stats failed: %v", err)
	}

	// SESSION STATS
	if err := h.DB.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE revoked_at IS NULL AND expires_at > NOW()),
			COUNT(DISTINCT user_id) FILTER (WHERE revoked_at IS NULL AND expires_at > NOW()),
			COUNT(*) FILTER (WHERE issued_at > NOW() - INTERVAL '24 hours')
		FROM refresh_tokens
	`).Scan(
		&resp.Sessions.ActiveRefreshTokens,
		&resp.Sessions.UniqueUsersWithSession,
		&resp.Sessions.IssuedLast24h,
	); err != nil {
		utils.SafeWarn("admin/stats: session stats failed: %v", err)
	}

	// SUGGESTIONS CACHE
	if err := h.DB.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE expires_at > NOW())
		FROM market_suggestions
	`).Scan(&resp.Suggestions.Total, &resp.Suggestions.NotExpired); err != nil {
		utils.SafeWarn("admin/stats: suggestions stats failed: %v", err)
	}

	c.JSON(http.StatusOK, resp)
}

// handlers/auth_refresh.go
// ============================================================================
// REFRESH & LOGOUT HANDLERS
// ============================================================================
// Endpoints :
//   POST /api/v1/auth/refresh    (public)  — échange un refresh token contre
//                                            un nouvel access token + nouveau
//                                            refresh token (rotation).
//   POST /api/v1/auth/logout     (public)  — révoque un refresh token précis.
//                                            Pas besoin d'être authentifié
//                                            (l'utilisateur n'a peut-être plus
//                                            d'access token valide).
//   POST /api/v1/user/logout-all (protégé) — révoque TOUS les refresh tokens
//                                            de l'utilisateur authentifié.
// ============================================================================

package handlers

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/LovationAdmin/budget-api/middleware"
	"github.com/LovationAdmin/budget-api/services"
	"github.com/LovationAdmin/budget-api/utils"

	"github.com/gin-gonic/gin"
)

type RefreshTokenHandler struct {
	DB             *sql.DB
	RefreshService *services.RefreshTokenService
}

func NewRefreshTokenHandler(db *sql.DB) *RefreshTokenHandler {
	return &RefreshTokenHandler{
		DB:             db,
		RefreshService: services.NewRefreshTokenService(db),
	}
}

// ----------------------------------------------------------------------------
// REFRESH
// ----------------------------------------------------------------------------

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

// Refresh échange un refresh token valide contre un nouveau couple
// (access_token, refresh_token). L'ancien refresh token est révoqué (rotation).
func (h *RefreshTokenHandler) Refresh(c *gin.Context) {
	var req RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Refresh token required"})
		return
	}

	ctx := c.Request.Context()

	// 1. Valider le refresh token reçu
	userID, err := h.RefreshService.Validate(ctx, req.RefreshToken)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrRefreshTokenNotFound):
			utils.SafeWarn("Refresh attempt with unknown token")
		case errors.Is(err, services.ErrRefreshTokenRevoked):
			utils.SafeWarn("Refresh attempt with revoked token (possible reuse)")
		case errors.Is(err, services.ErrRefreshTokenExpired):
			utils.SafeInfo("Refresh attempt with expired token")
		default:
			utils.SafeError("Refresh validation error: %v", err)
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired refresh token"})
		return
	}

	// 2. Récupérer l'email pour générer le nouveau JWT
	var userEmail string
	if err := h.DB.QueryRowContext(ctx,
		"SELECT email FROM users WHERE id = $1", userID,
	).Scan(&userEmail); err != nil {
		utils.SafeError("Failed to fetch user during refresh: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid refresh token"})
		return
	}

	// 3. Générer le nouvel access token
	accessToken, err := utils.GenerateAccessToken(userID, userEmail)
	if err != nil {
		utils.SafeError("Failed to generate access token during refresh: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to refresh"})
		return
	}

	// 4. Rotation : révoquer l'ancien et émettre un nouveau refresh token
	if err := h.RefreshService.Revoke(ctx, req.RefreshToken); err != nil {
		utils.SafeWarn("Failed to revoke old refresh token: %v", err)
		// On continue : la rotation est best-effort. Pire cas, l'ancien restera
		// utilisable jusqu'à expiration, mais le nouveau fonctionne déjà.
	}

	newRefresh, err := h.RefreshService.Issue(ctx,
		userID,
		c.Request.UserAgent(),
		c.ClientIP(),
	)
	if err != nil {
		utils.SafeError("Failed to issue new refresh token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to refresh"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token":  accessToken,
		"refresh_token": newRefresh,
		"token":         accessToken, // alias pour rétrocompat avec l'ancien frontend
		"token_type":    "Bearer",
	})
}

// ----------------------------------------------------------------------------
// LOGOUT (single device)
// ----------------------------------------------------------------------------

type LogoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// Logout révoque un refresh token précis. Idempotent : retourne 200 même si
// le token est inconnu (sinon on permettrait à un attaquant de tester quels
// tokens sont valides).
func (h *RefreshTokenHandler) Logout(c *gin.Context) {
	var req LogoutRequest
	_ = c.ShouldBindJSON(&req) // body vide accepté

	if req.RefreshToken != "" {
		// Best effort : on ne révèle pas si le token existait
		_ = h.RefreshService.Revoke(c.Request.Context(), req.RefreshToken)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Logged out"})
}

// ----------------------------------------------------------------------------
// LOGOUT ALL DEVICES (protégé)
// ----------------------------------------------------------------------------

// LogoutAll révoque TOUS les refresh tokens actifs de l'utilisateur authentifié.
// Utile depuis la page Profil pour "se déconnecter de tous les appareils".
func (h *RefreshTokenHandler) LogoutAll(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	if err := h.RefreshService.RevokeAllForUser(c.Request.Context(), userID); err != nil {
		utils.SafeError("Failed to revoke all refresh tokens for user %s: %v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to logout from all devices"})
		return
	}

	utils.SafeInfo("User logged out from all devices")
	c.JSON(http.StatusOK, gin.H{"message": "Logged out from all devices"})
}

// ----------------------------------------------------------------------------
// ACTIVE SESSIONS (protégé) — bonus, pour une future page "Sessions actives"
// ----------------------------------------------------------------------------

// ActiveSessionsCount retourne le nombre de sessions actives (refresh tokens
// non-révoqués et non-expirés) pour l'utilisateur authentifié.
func (h *RefreshTokenHandler) ActiveSessionsCount(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	count, err := h.RefreshService.CountActiveForUser(c.Request.Context(), userID)
	if err != nil {
		utils.SafeError("Failed to count sessions: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch sessions"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"active_sessions": count})
}

// handlers/auth_refresh.go
// ============================================================================
// AUTH REFRESH / LOGOUT HANDLERS
// ============================================================================
// Trois handlers complémentaires à Login/Signup :
//   - POST /auth/refresh    : échange le refresh cookie contre un nouvel access token
//   - POST /auth/logout     : révoque le refresh courant et clear le cookie
//   - POST /auth/logout-all : révoque tous les refresh tokens du user (auth requise)
//
// Cookie : "rt", HttpOnly; Secure; SameSite=None; Path=/api/v1/auth; Max-Age=604800
// SameSite=None requis pour le cross-origin (front Vercel ↔ API Render).
// CSRF mitigé par CORS allowlist (AllowCredentials true uniquement pour origines connues).
// ============================================================================

package handlers

import (
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LovationAdmin/budget-api/middleware"
	"github.com/LovationAdmin/budget-api/services"
	"github.com/LovationAdmin/budget-api/utils"
)

// ============================================================================
// CONSTANTES & HELPERS COOKIES
// ============================================================================

const (
	refreshCookieName = "rt"
	refreshCookiePath = "/api/v1/auth"
)

func isProd() bool {
	env := os.Getenv("ENVIRONMENT")
	return env == "production" || env == "prod"
}

// setRefreshCookie pose le cookie de refresh sur la réponse.
// maxAge en secondes : >0 = durée ; <0 = supprime ; 0 = session.
func setRefreshCookie(c *gin.Context, value string, maxAgeSeconds int) {
	secure := isProd()
	sameSite := http.SameSiteNoneMode
	if !secure {
		// SameSite=None exige Secure=true. En dev (HTTP), on retombe sur Lax.
		sameSite = http.SameSiteLaxMode
	}
	c.SetSameSite(sameSite)
	c.SetCookie(
		refreshCookieName,
		value,
		maxAgeSeconds,
		refreshCookiePath,
		"",     // domain : laisser vide → host de la requête
		secure, // Secure
		true,   // HttpOnly
	)
}

func clearRefreshCookie(c *gin.Context) {
	setRefreshCookie(c, "", -1)
}

// ============================================================================
// REFRESH HANDLER
// ============================================================================

// Refresh échange un refresh token (cookie) contre un nouvel access token.
// POST /api/v1/auth/refresh
func (h *AuthHandler) Refresh(c *gin.Context) {
	if h.RefreshTokens == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Refresh service not configured"})
		return
	}

	rawToken, err := c.Cookie(refreshCookieName)
	if err != nil || rawToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing refresh token"})
		return
	}

	userAgent := c.Request.UserAgent()
	ipAddress := c.ClientIP()

	newRaw, _, userID, rotErr := h.RefreshTokens.Rotate(c.Request.Context(), rawToken, userAgent, ipAddress)
	if rotErr != nil {
		clearRefreshCookie(c)
		switch {
		case errors.Is(rotErr, services.ErrRefreshTokenReused):
			utils.SafeWarn("Refresh token reuse detected — family revoked")
			utils.CaptureSecurityEvent(
				"refresh_token_reuse",
				"Refresh token reuse detected — family revoked",
				map[string]string{
					"ip":         c.ClientIP(),
					"user_agent": c.Request.UserAgent(),
				},
			)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Session compromised, please log in again"})
		case errors.Is(rotErr, services.ErrRefreshTokenExpired):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Session expired, please log in again"})
		case errors.Is(rotErr, services.ErrRefreshTokenRevoked),
			errors.Is(rotErr, services.ErrRefreshTokenNotFound):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
		default:
			utils.SafeError("Refresh failed: %v", rotErr)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to refresh session"})
		}
		return
	}

	// Charger l'email pour générer le nouvel access token
	var email string
	err = h.DB.QueryRow(`SELECT email FROM users WHERE id = $1`, userID).Scan(&email)
	if err != nil {
		utils.SafeError("Refresh: failed to load user: %v", err)
		clearRefreshCookie(c)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
		return
	}

	accessToken, err := utils.GenerateAccessToken(userID, email)
	if err != nil {
		utils.SafeError("Refresh: failed to generate access token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to issue token"})
		return
	}

	maxAge := int(h.RefreshTokens.Lifetime().Seconds())
	setRefreshCookie(c, newRaw, maxAge)

	c.JSON(http.StatusOK, gin.H{
		"access_token": accessToken,
		"expires_in":   15 * 60, // 15 min — à aligner avec JWT_EXPIRY
	})
}

// ============================================================================
// LOGOUT
// ============================================================================

// Logout révoque le refresh courant et clear le cookie.
// POST /api/v1/auth/logout — pas d'auth Bearer requise (cookie suffit).
func (h *AuthHandler) Logout(c *gin.Context) {
	rawToken, err := c.Cookie(refreshCookieName)
	if err == nil && rawToken != "" && h.RefreshTokens != nil {
		// Best-effort
		if revokeErr := h.RefreshTokens.Revoke(c.Request.Context(), rawToken); revokeErr != nil {
			utils.SafeWarn("Logout: revoke failed: %v", revokeErr)
		}
	}
	clearRefreshCookie(c)
	c.JSON(http.StatusOK, gin.H{"message": "Logged out"})
}

// ============================================================================
// LOGOUT ALL
// ============================================================================

// LogoutAll révoque tous les refresh tokens actifs du user authentifié.
// POST /api/v1/auth/logout-all — protégé par AuthMiddleware.
func (h *AuthHandler) LogoutAll(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	if h.RefreshTokens == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Refresh service not configured"})
		return
	}

	count, err := h.RefreshTokens.RevokeAllForUser(c.Request.Context(), userID)
	if err != nil {
		utils.SafeError("LogoutAll: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to log out all devices"})
		return
	}

	clearRefreshCookie(c)
	c.JSON(http.StatusOK, gin.H{
		"message":         "Logged out from all devices",
		"sessions_closed": count,
	})
}

// ============================================================================
// HELPER POUR LOGIN
// ============================================================================

// IssueRefreshAndSetCookie crée un refresh token et pose le cookie.
// Appelé depuis Login() après vérification des credentials.
// Best-effort : si le refresh échoue, on continue sans (l'utilisateur sera
// juste forcé de se reconnecter à l'expiration de l'access token).
func (h *AuthHandler) IssueRefreshAndSetCookie(c *gin.Context, userID string) {
	if h.RefreshTokens == nil {
		return
	}
	userAgent := c.Request.UserAgent()
	ipAddress := c.ClientIP()

	rawToken, _, err := h.RefreshTokens.Issue(c.Request.Context(), userID, userAgent, ipAddress)
	if err != nil {
		utils.SafeWarn("Failed to issue refresh token at login: %v", err)
		return
	}
	maxAge := int(h.RefreshTokens.Lifetime().Seconds())
	setRefreshCookie(c, rawToken, maxAge)
}

// ============================================================================
// PARSE REFRESH_EXPIRY
// ============================================================================

// ParseRefreshLifetime lit REFRESH_EXPIRY (ex: "168h") ; default 7j.
func ParseRefreshLifetime() time.Duration {
	raw := os.Getenv("REFRESH_EXPIRY")
	if raw == "" {
		return 7 * 24 * time.Hour
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 7 * 24 * time.Hour
	}
	return d
}

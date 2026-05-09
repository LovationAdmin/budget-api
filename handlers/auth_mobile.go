// handlers/auth_mobile.go
// ============================================================================
// MOBILE AUTH HANDLERS
// ============================================================================
// Variants of /auth/login, /auth/refresh, /auth/logout that return the refresh
// token in the JSON body instead of a Set-Cookie header. Mobile clients (Expo)
// store the token in SecureStore (keychain on iOS, Keystore on Android).
//
// Security notes:
//   - Same rotation + reuse-detection as the cookie variant.
//   - Refresh token is opaque (256-bit base64url). It's never logged.
//   - The browser cookie path is intentionally untouched — web users keep the
//     httpOnly cookie flow.
// ============================================================================

package handlers

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/LovationAdmin/budget-api/models"
	"github.com/LovationAdmin/budget-api/services"
	"github.com/LovationAdmin/budget-api/utils"
)

type mobileLoginResponse struct {
	Token        string             `json:"token"`
	RefreshToken string             `json:"refresh_token"`
	ExpiresIn    int                `json:"expires_in"`
	User         mobileUserPayload  `json:"user"`
}

type mobileUserPayload struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	Name        string `json:"name"`
	Avatar      string `json:"avatar"`
	TOTPEnabled bool   `json:"totp_enabled"`
}

// MobileLogin — POST /api/v1/auth/mobile/login
// Same validation as Login() but returns refresh_token in body (no cookie).
func (h *AuthHandler) MobileLogin(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	utils.SafeInfo("Mobile login attempt")

	var user models.User
	var passwordHash string
	var totpSecret sql.NullString

	err := h.DB.QueryRow(`
		SELECT id, email, password_hash, name, COALESCE(avatar, ''),
		       totp_enabled, totp_secret, email_verified, created_at, updated_at
		FROM users WHERE email = $1
	`, req.Email).Scan(
		&user.ID, &user.Email, &passwordHash, &user.Name, &user.Avatar,
		&user.TOTPEnabled, &totpSecret, &user.EmailVerified, &user.CreatedAt, &user.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		utils.LogAuthAction("MobileLogin", req.Email, false)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
		return
	}
	if err != nil {
		utils.SafeError("Database error during mobile login: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Login failed"})
		return
	}

	if !utils.CheckPassword(req.Password, passwordHash) {
		utils.LogAuthAction("MobileLogin", req.Email, false)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
		return
	}

	if !user.EmailVerified {
		utils.LogAuthAction("MobileLogin-Unverified", req.Email, false)
		c.JSON(http.StatusForbidden, gin.H{
			"error":              "Email not verified",
			"email_not_verified": true,
		})
		return
	}

	if user.TOTPEnabled && totpSecret.Valid {
		if req.TOTPCode == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "2FA code required", "requires_2fa": true})
			return
		}
		valid, vErr := utils.VerifyTOTP(totpSecret.String, req.TOTPCode)
		if vErr != nil || !valid {
			utils.LogAuthAction("MobileLogin-2FA", req.Email, false)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid 2FA code"})
			return
		}
	}

	accessToken, err := utils.GenerateAccessToken(user.ID, user.Email)
	if err != nil {
		utils.SafeError("Failed to generate access token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Login failed"})
		return
	}

	if h.RefreshTokens == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Refresh service not configured"})
		return
	}
	rawRefresh, _, rtErr := h.RefreshTokens.Issue(c.Request.Context(), user.ID,
		c.Request.UserAgent(), c.ClientIP())
	if rtErr != nil {
		utils.SafeError("Failed to issue mobile refresh token: %v", rtErr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Login failed"})
		return
	}

	utils.LogAuthAction("MobileLogin", req.Email, true)

	c.JSON(http.StatusOK, mobileLoginResponse{
		Token:        accessToken,
		RefreshToken: rawRefresh,
		ExpiresIn:    15 * 60,
		User: mobileUserPayload{
			ID:          user.ID,
			Email:       user.Email,
			Name:        user.Name,
			Avatar:      user.Avatar,
			TOTPEnabled: user.TOTPEnabled,
		},
	})
}

// MobileRefresh — POST /api/v1/auth/mobile/refresh
// Body: { "refresh_token": "..." }
// Returns a new access + refresh pair (rotation), no cookie.
func (h *AuthHandler) MobileRefresh(c *gin.Context) {
	if h.RefreshTokens == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Refresh service not configured"})
		return
	}

	var req struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "refresh_token required"})
		return
	}

	newRaw, _, userID, rotErr := h.RefreshTokens.Rotate(c.Request.Context(), req.RefreshToken,
		c.Request.UserAgent(), c.ClientIP())
	if rotErr != nil {
		switch {
		case errors.Is(rotErr, services.ErrRefreshTokenReused):
			utils.SafeWarn("Mobile refresh reuse — family revoked")
			utils.CaptureSecurityEvent("refresh_token_reuse",
				"Mobile refresh token reuse — family revoked",
				map[string]string{"ip": c.ClientIP(), "ua": c.Request.UserAgent()})
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Session compromised, please log in again"})
		case errors.Is(rotErr, services.ErrRefreshTokenExpired):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Session expired, please log in again"})
		case errors.Is(rotErr, services.ErrRefreshTokenRevoked),
			errors.Is(rotErr, services.ErrRefreshTokenNotFound):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
		default:
			utils.SafeError("Mobile refresh failed: %v", rotErr)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to refresh session"})
		}
		return
	}

	var email string
	if err := h.DB.QueryRow(`SELECT email FROM users WHERE id = $1`, userID).Scan(&email); err != nil {
		utils.SafeError("Mobile refresh: failed to load user: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
		return
	}

	accessToken, err := utils.GenerateAccessToken(userID, email)
	if err != nil {
		utils.SafeError("Mobile refresh: failed to generate access token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to issue token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token":  accessToken,
		"refresh_token": newRaw,
		"expires_in":    15 * 60,
	})
}

// MobileLogout — POST /api/v1/auth/mobile/logout
// Body: { "refresh_token": "..." } — best-effort revoke.
func (h *AuthHandler) MobileLogout(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = c.ShouldBindJSON(&req)

	if req.RefreshToken != "" && h.RefreshTokens != nil {
		if err := h.RefreshTokens.Revoke(c.Request.Context(), req.RefreshToken); err != nil {
			utils.SafeWarn("MobileLogout: revoke failed: %v", err)
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "Logged out"})
}

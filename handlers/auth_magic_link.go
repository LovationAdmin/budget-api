// handlers/auth_magic_link.go
// ============================================================================
// MAGIC LINK AUTH
// ============================================================================
// Passwordless sign-in: user enters their email on mobile -> server emails a
// short-lived single-use token -> user taps the link -> deep link opens the
// mobile app with `?token=...` -> client POSTs token to /auth/magic-link/verify
// -> server returns the same payload as /auth/mobile/login.
//
// Security:
//   - Token = 32 random bytes base64url (256 bits) — not predictable.
//   - SHA-256 hashed at rest (same approach as refresh tokens).
//   - Single-use: row marked `used_at` on first verification; reuse rejected.
//   - 15 minute TTL.
//   - email_verified gate is enforced (no first-time bypass).
//   - Account-enumeration safe: always returns 200 on /request.
// ============================================================================

package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LovationAdmin/budget-api/services"
	"github.com/LovationAdmin/budget-api/utils"
)

type MagicLinkHandler struct {
	DB            *sql.DB
	EmailService  *services.EmailService
	RefreshTokens *services.RefreshTokenService
}

func NewMagicLinkHandler(db *sql.DB, rt *services.RefreshTokenService) *MagicLinkHandler {
	return &MagicLinkHandler{
		DB:            db,
		EmailService:  services.NewEmailService(),
		RefreshTokens: rt,
	}
}

const magicLinkTTL = 15 * time.Minute

func generateMagicToken() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(raw))
	hash = hex.EncodeToString(sum[:])
	return raw, hash, nil
}

// Request — POST /api/v1/auth/magic-link/request  { email, device_id?, platform? }
// Always returns 200 to avoid account enumeration. Mailing happens async.
func (h *MagicLinkHandler) Request(c *gin.Context) {
	var req struct {
		Email    string `json:"email"    binding:"required,email"`
		DeviceID string `json:"device_id"`
		Platform string `json:"platform"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	utils.SafeInfo("Magic link request")

	respond := func() {
		c.JSON(http.StatusOK, gin.H{"message": "If the account exists, a sign-in link has been sent."})
	}

	var userID, name string
	var emailVerified bool
	err := h.DB.QueryRow(`SELECT id, name, email_verified FROM users WHERE email = $1`, req.Email).
		Scan(&userID, &name, &emailVerified)

	if err == sql.ErrNoRows || (err == nil && !emailVerified) {
		respond()
		return
	}
	if err != nil {
		utils.SafeError("magic-link DB lookup: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process request"})
		return
	}

	rawToken, tokenHash, err := generateMagicToken()
	if err != nil {
		utils.SafeError("magic-link token gen: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process request"})
		return
	}

	expires := time.Now().Add(magicLinkTTL)

	_, err = h.DB.Exec(`
		INSERT INTO magic_link_tokens (token, user_id, expires_at, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5)
	`, tokenHash, userID, expires, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		utils.SafeError("magic-link insert: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process request"})
		return
	}

	go func() {
		if mErr := h.EmailService.SendMagicLinkEmail(req.Email, name, magicLinkURL(rawToken)); mErr != nil {
			utils.SafeWarn("magic-link email send: %v", mErr)
		}
	}()

	respond()
}

func magicLinkURL(token string) string {
	base := os.Getenv("FRONTEND_URL")
	if base == "" {
		base = "https://budgetfamille.com"
	}
	return base + "/m/magic-link?token=" + token
}

// Verify — POST /api/v1/auth/magic-link/verify  { token, device_id? }
// Returns same shape as MobileLogin — access_token + refresh_token + user.
func (h *MagicLinkHandler) Verify(c *gin.Context) {
	var req struct {
		Token    string `json:"token" binding:"required"`
		DeviceID string `json:"device_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tokenHash := func() string {
		sum := sha256.Sum256([]byte(req.Token))
		return hex.EncodeToString(sum[:])
	}()

	var userID, email, name, avatar string
	var totpEnabled bool
	var expiresAt time.Time
	var usedAt sql.NullTime

	err := h.DB.QueryRow(`
		SELECT m.user_id, u.email, u.name, COALESCE(u.avatar,''), u.totp_enabled,
		       m.expires_at, m.used_at
		FROM magic_link_tokens m
		JOIN users u ON u.id = m.user_id
		WHERE m.token = $1
	`, tokenHash).Scan(&userID, &email, &name, &avatar, &totpEnabled, &expiresAt, &usedAt)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired link"})
		return
	}
	if err != nil {
		utils.SafeError("magic-link verify lookup: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify link"})
		return
	}

	if usedAt.Valid {
		utils.SafeWarn("magic-link reuse attempt")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Link already used"})
		return
	}
	if time.Now().After(expiresAt) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Link expired"})
		return
	}

	if _, err = h.DB.Exec(`UPDATE magic_link_tokens SET used_at = NOW() WHERE token = $1`, tokenHash); err != nil {
		utils.SafeError("magic-link mark used: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify link"})
		return
	}

	if totpEnabled {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "TOTP required — use password login", "requires_2fa": true})
		return
	}

	accessToken, err := utils.GenerateAccessToken(userID, email)
	if err != nil {
		utils.SafeError("magic-link issue access: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to issue token"})
		return
	}

	if h.RefreshTokens == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Refresh service not configured"})
		return
	}
	rawRefresh, _, rtErr := h.RefreshTokens.Issue(c.Request.Context(), userID,
		c.Request.UserAgent(), c.ClientIP())
	if rtErr != nil {
		utils.SafeError("magic-link issue refresh: %v", rtErr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to issue token"})
		return
	}

	utils.LogAuthAction("MagicLink", email, true)

	c.JSON(http.StatusOK, gin.H{
		"token":         accessToken,
		"refresh_token": rawRefresh,
		"expires_in":    15 * 60,
		"user": gin.H{
			"id":           userID,
			"email":        email,
			"name":         name,
			"avatar":       avatar,
			"totp_enabled": totpEnabled,
		},
	})
}

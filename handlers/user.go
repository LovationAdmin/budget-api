// handlers/user.go
// ✅ VERSION FINALE CORRIGÉE - SANS location management

package handlers

import (
	"database/sql"
	"log"
	"net/http"
	"time"

	"github.com/LovationAdmin/budget-api/middleware"
	"github.com/LovationAdmin/budget-api/models"
	"github.com/LovationAdmin/budget-api/services"
	"github.com/LovationAdmin/budget-api/utils"
	"github.com/gin-gonic/gin"
)

type UserHandler struct {
	DB            *sql.DB
	RefreshTokens *services.RefreshTokenService
}

// ============================================================================
// PROFILE MANAGEMENT
// ============================================================================

func (h *UserHandler) GetProfile(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var user models.User
	err := h.DB.QueryRow(`
		SELECT id, email, name, 
		       COALESCE(avatar, ''), 
		       totp_enabled, email_verified, COALESCE(locale, ''),
		       created_at, updated_at
		FROM users
		WHERE id = $1
	`, userID).Scan(
		&user.ID,
		&user.Email,
		&user.Name,
		&user.Avatar,
		&user.TOTPEnabled,
		&user.EmailVerified,
		&user.Locale,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}
	if err != nil {
		log.Printf("Error fetching profile: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch profile"})
		return
	}

	c.JSON(http.StatusOK, user)
}

type UpdateProfileRequest struct {
	Name   string `json:"name" binding:"required"`
	Avatar string `json:"avatar"`
}

func (h *UserHandler) UpdateProfile(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, err := h.DB.Exec(`
		UPDATE users
		SET name = $1, avatar = $2, updated_at = NOW()
		WHERE id = $3
	`, req.Name, req.Avatar, userID)

	if err != nil {
		log.Printf("Error updating profile: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update profile"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Profile updated successfully",
		"user": gin.H{
			"name":   req.Name,
			"avatar": req.Avatar,
		},
	})
}

// ============================================================================
// LANGUAGE
// ============================================================================

type UpdateLocaleRequest struct {
	Locale string `json:"locale" binding:"required"`
}

// UpdateLocale saves the account's language ("fr" or "en"): the app's and
// that of the emails sent to the account.
func (h *UserHandler) UpdateLocale(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req UpdateLocaleRequest
	locale := ""
	if err := c.ShouldBindJSON(&req); err == nil {
		locale = utils.NormalizeLocale(req.Locale)
	}
	if locale == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": `locale must be "fr" or "en"`})
		return
	}

	if _, err := h.DB.Exec(`UPDATE users SET locale = $1, updated_at = NOW() WHERE id = $2`, locale, userID); err != nil {
		log.Printf("Error updating locale: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update locale"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"locale": locale})
}

// ============================================================================
// ❌ LOCATION MANAGEMENT SUPPRIMÉ
// ============================================================================
// UpdateLocation() et GetLocation() ont été supprimés
// La localisation est maintenant gérée au niveau du budget

// ============================================================================
// PASSWORD MANAGEMENT
// ============================================================================

type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required,min=10,max=72"`
}

func (h *UserHandler) ChangePassword(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var currentHash, userEmail, userName string
	err := h.DB.QueryRow(`
		SELECT password_hash, email, name FROM users WHERE id = $1
	`, userID).Scan(&currentHash, &userEmail, &userName)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify password"})
		return
	}

	if !utils.CheckPassword(req.CurrentPassword, currentHash) {
		c.JSON(http.StatusForbidden, gin.H{"error": tr(c, "Mot de passe actuel incorrect.", "Current password is incorrect.")})
		return
	}

	// Empêcher la réutilisation du même mot de passe
	if utils.CheckPassword(req.NewPassword, currentHash) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "New password must be different from the current one"})
		return
	}

	// Validation forte du nouveau mot de passe
	if err := utils.ValidatePassword(req.NewPassword, userEmail, userName); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	newHash, err := utils.HashPassword(req.NewPassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash new password"})
		return
	}

	_, err = h.DB.Exec(`
		UPDATE users
		SET password_hash = $1, updated_at = NOW()
		WHERE id = $2
	`, newHash, userID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update password"})
		return
	}

	log.Printf("✅ User %s password changed successfully", userID)

	// Sécurité : révoquer toutes les sessions actives.
	// Si un attaquant connaissait l'ancien mot de passe, ses sessions tombent.
	if h.RefreshTokens != nil {
		if _, err := h.RefreshTokens.RevokeAllForUser(c.Request.Context(), userID); err != nil {
			log.Printf("⚠️ Failed to revoke refresh tokens after password change: %v", err)
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Password changed successfully"})
}

// ============================================================================
// 2FA MANAGEMENT
// ============================================================================

func (h *UserHandler) SetupTOTP(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var email string
	err := h.DB.QueryRow(`SELECT email FROM users WHERE id = $1`, userID).Scan(&email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get user"})
		return
	}

	// ✅ CORRIGÉ : utils.GenerateTOTPSecret (pas GenerateTOTP)
	secret, qrCode, err := utils.GenerateTOTPSecret(email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate TOTP"})
		return
	}

	_, err = h.DB.Exec(`
		UPDATE users
		SET totp_secret = $1, updated_at = NOW()
		WHERE id = $2
	`, secret, userID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store TOTP secret"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"secret":  secret,
		"qr_code": qrCode,
	})
}

type VerifyTOTPRequest struct {
	Code string `json:"code" binding:"required"`
}

func (h *UserHandler) VerifyTOTP(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req VerifyTOTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var secret sql.NullString
	err := h.DB.QueryRow(`
		SELECT totp_secret FROM users WHERE id = $1
	`, userID).Scan(&secret)

	if err != nil || !secret.Valid {
		c.JSON(http.StatusBadRequest, gin.H{"error": "TOTP not set up"})
		return
	}

	// ✅ CORRIGÉ : utils.VerifyTOTP retourne (bool, error)
	valid, err := utils.VerifyTOTP(secret.String, req.Code)
	if err != nil || !valid {
		c.JSON(http.StatusForbidden, gin.H{"error": wrongCode(c)})
		return
	}

	_, err = h.DB.Exec(`
		UPDATE users
		SET totp_enabled = TRUE, updated_at = NOW()
		WHERE id = $1
	`, userID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to enable 2FA"})
		return
	}

	log.Printf("✅ 2FA enabled for user %s", userID)

	c.JSON(http.StatusOK, gin.H{
		"message": "2FA enabled successfully",
		"enabled": true,
	})
}

type DisableTOTPRequest struct {
	Password string `json:"password" binding:"required"`
	Code     string `json:"code" binding:"required"`
}

func (h *UserHandler) DisableTOTP(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req DisableTOTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var passwordHash string
	var secret sql.NullString
	err := h.DB.QueryRow(`
		SELECT password_hash, totp_secret FROM users WHERE id = $1
	`, userID).Scan(&passwordHash, &secret)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify credentials"})
		return
	}

	if !utils.CheckPassword(req.Password, passwordHash) {
		c.JSON(http.StatusForbidden, gin.H{"error": wrongPassword(c)})
		return
	}

	if secret.Valid {
		valid, err := utils.VerifyTOTP(secret.String, req.Code)
		if err != nil || !valid {
			c.JSON(http.StatusForbidden, gin.H{"error": wrongCode(c)})
			return
		}
	}

	_, err = h.DB.Exec(`
		UPDATE users
		SET totp_enabled = FALSE, totp_secret = NULL, updated_at = NOW()
		WHERE id = $1
	`, userID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to disable 2FA"})
		return
	}

	log.Printf("✅ 2FA disabled for user %s", userID)

	c.JSON(http.StatusOK, gin.H{
		"message": "2FA disabled successfully",
		"enabled": false,
	})
}

// ============================================================================
// ACCOUNT DELETION
// ============================================================================

type DeleteAccountRequest struct {
	Password string `json:"password" binding:"required"`
}

// deleteUser deletes the account and what is only theirs (ON DELETE CASCADE),
// after detaching them from what outlives them (services.ClearUserReferences).
func deleteUser(c *gin.Context, db *sql.DB, userID string) error {
	ctx := c.Request.Context()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if err := services.ClearUserReferences(ctx, tx, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// A wrong password or code to confirm an action is 403, not 401: the session
// is fine, and the app treats a 401 as an expired session (refresh, then
// sign-out).
func wrongPassword(c *gin.Context) string {
	return tr(c, "Mot de passe incorrect.", "Incorrect password.")
}

func wrongCode(c *gin.Context) string {
	return tr(c, "Code de vérification incorrect.", "Incorrect verification code.")
}

func (h *UserHandler) DeleteAccount(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req DeleteAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var passwordHash string
	err := h.DB.QueryRow(`
		SELECT password_hash FROM users WHERE id = $1
	`, userID).Scan(&passwordHash)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify password"})
		return
	}

	if !utils.CheckPassword(req.Password, passwordHash) {
		c.JSON(http.StatusForbidden, gin.H{"error": wrongPassword(c)})
		return
	}

	if err := deleteUser(c, h.DB, userID); err != nil {
		log.Printf("Error deleting account: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": tr(c, "La suppression du compte a échoué. Réessayez dans un instant.", "Deleting the account failed. Try again in a moment.")})
		return
	}

	log.Printf("✅ User %s account deleted", userID)

	c.JSON(http.StatusOK, gin.H{"message": "Account deleted successfully"})
}

// ============================================================================
// GDPR DATA EXPORT
// ============================================================================

func (h *UserHandler) ExportUserData(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	log.Printf("📊 [GDPR Export] User %s requested data export", userID)

	// Get user profile
	var user models.User
	err := h.DB.QueryRow(`
		SELECT id, email, name, 
		       COALESCE(avatar, ''), 
		       totp_enabled, email_verified, 
		       created_at, updated_at
		FROM users
		WHERE id = $1
	`, userID).Scan(
		&user.ID,
		&user.Email,
		&user.Name,
		&user.Avatar,
		&user.TOTPEnabled,
		&user.EmailVerified,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		log.Printf("❌ [GDPR Export] Failed to fetch user profile: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch user data"})
		return
	}

	// Build export (simplifié pour éviter les erreurs de déchiffrement)
	exportData := gin.H{
		"export_info": gin.H{
			"generated_at": time.Now().Format(time.RFC3339),
			"user_id":      userID,
			"format":       "JSON",
			"compliance":   "GDPR Article 20 - Right to Data Portability",
		},
		"user_profile": gin.H{
			"id":             user.ID,
			"email":          user.Email,
			"name":           user.Name,
			"avatar":         user.Avatar,
			"totp_enabled":   user.TOTPEnabled,
			"email_verified": user.EmailVerified,
			"created_at":     user.CreatedAt.Format(time.RFC3339),
			"updated_at":     user.UpdatedAt.Format(time.RFC3339),
		},
	}

	// Private items (names of private personal charges…) belong to this user only.
	if items, err := services.NewPrivateItemService(h.DB).ExportForUser(c.Request.Context(), userID); err == nil && len(items) > 0 {
		exportData["private_items_by_budget"] = items
	}

	log.Printf("✅ [GDPR Export] Successfully generated export for user %s", userID)

	c.JSON(http.StatusOK, exportData)
}

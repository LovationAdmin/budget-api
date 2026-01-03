// handlers/user.go
// ✅ VERSION FINALE CORRIGÉE - SANS location management

package handlers

import (
	"database/sql"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/LovationAdmin/budget-api/middleware"
	"github.com/LovationAdmin/budget-api/models"
	"github.com/LovationAdmin/budget-api/utils"
)

type UserHandler struct {
	DB *sql.DB
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
// ❌ LOCATION MANAGEMENT SUPPRIMÉ
// ============================================================================
// UpdateLocation() et GetLocation() ont été supprimés
// La localisation est maintenant gérée au niveau du budget

// ============================================================================
// PASSWORD MANAGEMENT
// ============================================================================

type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required,min=6"`
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

	var currentHash string
	err := h.DB.QueryRow(`
		SELECT password_hash FROM users WHERE id = $1
	`, userID).Scan(&currentHash)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify password"})
		return
	}

	if !utils.CheckPassword(req.CurrentPassword, currentHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Current password is incorrect"})
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
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid TOTP code"})
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
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid password"})
		return
	}

	if secret.Valid {
		valid, err := utils.VerifyTOTP(secret.String, req.Code)
		if err != nil || !valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid 2FA code"})
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
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid password"})
		return
	}

	_, err = h.DB.Exec(`DELETE FROM users WHERE id = $1`, userID)

	if err != nil {
		log.Printf("Error deleting account: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete account"})
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

	log.Printf("✅ [GDPR Export] Successfully generated export for user %s", userID)

	c.JSON(http.StatusOK, exportData)
}
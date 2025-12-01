package handlers

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
	"budget-api/middleware"
	"budget-api/models"
	"budget-api/utils"
)

type UserHandler struct {
	DB *sql.DB
}

// GetProfile returns the current user's profile
func (h *UserHandler) GetProfile(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var user models.User
	err := h.DB.QueryRow(`
		SELECT id, email, name, totp_enabled, email_verified, created_at, updated_at
		FROM users
		WHERE id = $1
	`, userID).Scan(&user.ID, &user.Email, &user.Name, &user.TOTPEnabled,
		&user.EmailVerified, &user.CreatedAt, &user.UpdatedAt)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch profile"})
		return
	}

	c.JSON(http.StatusOK, user)
}

// UpdateProfile updates user profile information
func (h *UserHandler) UpdateProfile(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req struct {
		Name string `json:"name" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, err := h.DB.Exec(`
		UPDATE users
		SET name = $1, updated_at = NOW()
		WHERE id = $2
	`, req.Name, userID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update profile"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Profile updated successfully"})
}

// ChangePassword changes the user's password
func (h *UserHandler) ChangePassword(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req struct {
		CurrentPassword string `json:"current_password" binding:"required"`
		NewPassword     string `json:"new_password" binding:"required,min=8"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get current password hash
	var currentHash string
	err := h.DB.QueryRow(`
		SELECT password_hash FROM users WHERE id = $1
	`, userID).Scan(&currentHash)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify password"})
		return
	}

	// Verify current password
	if !utils.CheckPassword(req.CurrentPassword, currentHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Current password is incorrect"})
		return
	}

	// Hash new password
	newHash, err := utils.HashPassword(req.NewPassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	// Update password
	_, err = h.DB.Exec(`
		UPDATE users
		SET password_hash = $1, updated_at = NOW()
		WHERE id = $2
	`, newHash, userID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update password"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Password changed successfully"})
}

// SetupTOTP generates a TOTP secret for 2FA setup
func (h *UserHandler) SetupTOTP(c *gin.Context) {
	userID := middleware.GetUserID(c)
	email := middleware.GetUserEmail(c)

	if userID == "" || email == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	// Check if already enabled
	var totpEnabled bool
	err := h.DB.QueryRow(`
		SELECT totp_enabled FROM users WHERE id = $1
	`, userID).Scan(&totpEnabled)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check 2FA status"})
		return
	}

	if totpEnabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "2FA already enabled"})
		return
	}

	// Generate TOTP secret
	secret, qrURL, err := utils.GenerateTOTPSecret(email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate 2FA secret"})
		return
	}

	// Store secret (but don't enable yet, wait for verification)
	_, err = h.DB.Exec(`
		UPDATE users
		SET totp_secret = $1
		WHERE id = $2
	`, secret, userID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store 2FA secret"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"secret":       secret,
		"qr_code_url":  qrURL,
		"instructions": "Scan the QR code with Google Authenticator or similar app, then verify with a code",
	})
}

// VerifyTOTP verifies and enables 2FA
func (h *UserHandler) VerifyTOTP(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req struct {
		Code string `json:"code" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get TOTP secret
	var secret sql.NullString
	err := h.DB.QueryRow(`
		SELECT totp_secret FROM users WHERE id = $1
	`, userID).Scan(&secret)

	if err != nil || !secret.Valid {
		c.JSON(http.StatusBadRequest, gin.H{"error": "2FA not set up"})
		return
	}

	// Verify code
	valid, err := utils.VerifyTOTP(secret.String, req.Code)
	if err != nil || !valid {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid 2FA code"})
		return
	}

	// Enable 2FA
	_, err = h.DB.Exec(`
		UPDATE users
		SET totp_enabled = TRUE, updated_at = NOW()
		WHERE id = $1
	`, userID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to enable 2FA"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "2FA enabled successfully",
		"enabled": true,
	})
}

// DisableTOTP disables 2FA
func (h *UserHandler) DisableTOTP(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req struct {
		Password string `json:"password" binding:"required"`
		Code     string `json:"code" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify password and get TOTP secret
	var passwordHash string
	var secret sql.NullString
	err := h.DB.QueryRow(`
		SELECT password_hash, totp_secret FROM users WHERE id = $1
	`, userID).Scan(&passwordHash, &secret)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify credentials"})
		return
	}

	// Check password
	if !utils.CheckPassword(req.Password, passwordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid password"})
		return
	}

	// Verify TOTP code
	if secret.Valid {
		valid, err := utils.VerifyTOTP(secret.String, req.Code)
		if err != nil || !valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid 2FA code"})
			return
		}
	}

	// Disable 2FA
	_, err = h.DB.Exec(`
		UPDATE users
		SET totp_enabled = FALSE, totp_secret = NULL, updated_at = NOW()
		WHERE id = $1
	`, userID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to disable 2FA"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "2FA disabled successfully",
		"enabled": false,
	})
}

// DeleteAccount deletes the user's account
func (h *UserHandler) DeleteAccount(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req struct {
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify password
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

	// Delete user (cascade will handle related data)
	_, err = h.DB.Exec(`DELETE FROM users WHERE id = $1`, userID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete account"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Account deleted successfully"})
}
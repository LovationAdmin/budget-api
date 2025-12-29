package handlers

import (
	"database/sql"
	"net/http"
	"strings"
	"log"

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

// GetProfile returns the current user's profile
func (h *UserHandler) GetProfile(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var user models.User
	// ✅ Query includes country and postal_code
	err := h.DB.QueryRow(`
		SELECT id, email, name, 
		       COALESCE(avatar, ''), 
		       COALESCE(country, 'FR'),
		       COALESCE(postal_code, ''),
		       totp_enabled, email_verified, 
		       created_at, updated_at
		FROM users
		WHERE id = $1
	`, userID).Scan(
		&user.ID, 
		&user.Email, 
		&user.Name, 
		&user.Avatar, 
		&user.Country,
		&user.PostalCode,
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

// UpdateProfileRequest struct to validate input
type UpdateProfileRequest struct {
	Name   string `json:"name" binding:"required"`
	Avatar string `json:"avatar"` // Optional: Base64 string or Gradient CSS
}

// UpdateProfile updates user profile information
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
// ✅ LOCATION MANAGEMENT (NEW)
// ============================================================================

// UpdateLocationRequest struct for location updates
type UpdateLocationRequest struct {
	Country    string `json:"country" binding:"required,len=2"`
	PostalCode string `json:"postal_code"`
}

// UpdateLocation updates user's country and postal code
func (h *UserHandler) UpdateLocation(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req UpdateLocationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid country code (must be 2 characters)"})
		return
	}

	// Validate country code (list of supported countries)
	validCountries := map[string]bool{
		"FR": true, "BE": true, "DE": true, "ES": true, "IT": true,
		"PT": true, "NL": true, "LU": true, "AT": true, "IE": true,
	}
	
	countryUpper := strings.ToUpper(req.Country)
	if !validCountries[countryUpper] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Country not supported. Supported countries: FR, BE, DE, ES, IT, PT, NL, LU, AT, IE",
		})
		return
	}

	// Update database
	_, err := h.DB.Exec(`
		UPDATE users
		SET country = $1, postal_code = $2, updated_at = NOW()
		WHERE id = $3
	`, countryUpper, req.PostalCode, userID)

	if err != nil {
		log.Printf("Failed to update location for user %s: %v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update location"})
		return
	}

	log.Printf("✅ User %s location updated: %s %s", userID, countryUpper, req.PostalCode)

	c.JSON(http.StatusOK, gin.H{
		"message":     "Location updated successfully",
		"country":     countryUpper,
		"postal_code": req.PostalCode,
	})
}

// GetLocation returns user's current location
func (h *UserHandler) GetLocation(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var country, postalCode string
	err := h.DB.QueryRow(`
		SELECT COALESCE(country, 'FR'), COALESCE(postal_code, '')
		FROM users
		WHERE id = $1
	`, userID).Scan(&country, &postalCode)

	if err != nil {
		log.Printf("Error fetching location: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch location"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"country":     country,
		"postal_code": postalCode,
	})
}

// ============================================================================
// PASSWORD MANAGEMENT
// ============================================================================

// ChangePasswordRequest struct for password updates
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required,min=6"`
}

// ChangePassword changes the user's password
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash new password"})
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

	log.Printf("✅ User %s password changed successfully", userID)

	c.JSON(http.StatusOK, gin.H{"message": "Password changed successfully"})
}

// ============================================================================
// 2FA MANAGEMENT
// ============================================================================

// SetupTOTP generates a TOTP secret for the user
func (h *UserHandler) SetupTOTP(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	// Get user email
	var email string
	err := h.DB.QueryRow(`SELECT email FROM users WHERE id = $1`, userID).Scan(&email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get user"})
		return
	}

	// Generate TOTP secret
	secret, qrCode, err := utils.GenerateTOTPSecret(email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate TOTP"})
		return
	}

	// Store secret temporarily (not enabled yet)
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

// VerifyTOTPRequest struct for TOTP verification
type VerifyTOTPRequest struct {
	Code string `json:"code" binding:"required"`
}

// VerifyTOTP enables 2FA after successful verification
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

	// Get TOTP secret
	var secret sql.NullString
	err := h.DB.QueryRow(`
		SELECT totp_secret FROM users WHERE id = $1
	`, userID).Scan(&secret)

	if err != nil || !secret.Valid {
		c.JSON(http.StatusBadRequest, gin.H{"error": "TOTP not set up"})
		return
	}

	// Verify code
	valid, err := utils.VerifyTOTP(secret.String, req.Code)
	if err != nil || !valid {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid TOTP code"})
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

	log.Printf("✅ 2FA enabled for user %s", userID)

	c.JSON(http.StatusOK, gin.H{
		"message": "2FA enabled successfully",
		"enabled": true,
	})
}

// DisableTOTPRequest struct for disabling 2FA
type DisableTOTPRequest struct {
	Password string `json:"password" binding:"required"`
	Code     string `json:"code" binding:"required"`
}

// DisableTOTP disables 2FA after verification
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

	// Get password hash and TOTP secret
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

	log.Printf("✅ 2FA disabled for user %s", userID)

	c.JSON(http.StatusOK, gin.H{
		"message": "2FA disabled successfully",
		"enabled": false,
	})
}

// ============================================================================
// ACCOUNT DELETION
// ============================================================================

// DeleteAccountRequest struct for account deletion
type DeleteAccountRequest struct {
	Password string `json:"password" binding:"required"`
}

// DeleteAccount deletes the user's account
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
		log.Printf("Error deleting account: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete account"})
		return
	}

	log.Printf("✅ User %s account deleted", userID)

	c.JSON(http.StatusOK, gin.H{"message": "Account deleted successfully"})
}
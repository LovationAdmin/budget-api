// handlers/user.go
// ✅ VERSION CORRIGÉE - UpdateLocation et GetLocation SUPPRIMÉS

package handlers

import (
	"database/sql"
	"encoding/json"
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

	c.JSON(http.StatusOK, gin.H{"message": "Profile updated successfully"})
}

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

	// Get current password hash
	var currentHash string
	err := h.DB.QueryRow(`SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&currentHash)
	if err != nil {
		log.Printf("Error fetching user: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to change password"})
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
		log.Printf("Error hashing password: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to change password"})
		return
	}

	// Update password
	_, err = h.DB.Exec(`
		UPDATE users
		SET password_hash = $1, updated_at = NOW()
		WHERE id = $2
	`, newHash, userID)

	if err != nil {
		log.Printf("Error updating password: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to change password"})
		return
	}

	// Invalidate all sessions except current one
	refreshToken := c.GetHeader("X-Refresh-Token")
	if refreshToken != "" {
		_, err = h.DB.Exec(`
			DELETE FROM sessions
			WHERE user_id = $1 AND refresh_token != $2
		`, userID, refreshToken)
		if err != nil {
			log.Printf("Error invalidating sessions: %v", err)
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Password changed successfully"})
}

// ============================================================================
// 2FA MANAGEMENT
// ============================================================================

type Setup2FAResponse struct {
	Secret string `json:"secret"`
	QRCode string `json:"qr_code"`
}

func (h *UserHandler) Setup2FA(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var email string
	err := h.DB.QueryRow(`SELECT email FROM users WHERE id = $1`, userID).Scan(&email)
	if err != nil {
		log.Printf("Error fetching user: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to setup 2FA"})
		return
	}

	secret, qrCode, err := utils.GenerateTOTP(email)
	if err != nil {
		log.Printf("Error generating TOTP: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to setup 2FA"})
		return
	}

	// Store secret temporarily (not enabled yet)
	_, err = h.DB.Exec(`
		UPDATE users
		SET totp_secret = $1, updated_at = NOW()
		WHERE id = $2
	`, secret, userID)

	if err != nil {
		log.Printf("Error storing TOTP secret: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to setup 2FA"})
		return
	}

	c.JSON(http.StatusOK, Setup2FAResponse{
		Secret: secret,
		QRCode: qrCode,
	})
}

type Verify2FARequest struct {
	Code string `json:"code" binding:"required"`
}

func (h *UserHandler) Verify2FA(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req Verify2FARequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var secret string
	err := h.DB.QueryRow(`SELECT totp_secret FROM users WHERE id = $1`, userID).Scan(&secret)
	if err != nil {
		log.Printf("Error fetching TOTP secret: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify 2FA"})
		return
	}

	if secret == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "2FA not set up"})
		return
	}

	if !utils.VerifyTOTP(secret, req.Code) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid code"})
		return
	}

	// Enable 2FA
	_, err = h.DB.Exec(`
		UPDATE users
		SET totp_enabled = true, updated_at = NOW()
		WHERE id = $1
	`, userID)

	if err != nil {
		log.Printf("Error enabling 2FA: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to enable 2FA"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "2FA enabled successfully"})
}

func (h *UserHandler) Disable2FA(c *gin.Context) {
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
	err := h.DB.QueryRow(`SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&passwordHash)
	if err != nil {
		log.Printf("Error fetching user: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to disable 2FA"})
		return
	}

	if !utils.CheckPassword(req.Password, passwordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid password"})
		return
	}

	// Disable 2FA
	_, err = h.DB.Exec(`
		UPDATE users
		SET totp_enabled = false, totp_secret = '', updated_at = NOW()
		WHERE id = $1
	`, userID)

	if err != nil {
		log.Printf("Error disabling 2FA: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to disable 2FA"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "2FA disabled successfully"})
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

	// Verify password
	var passwordHash string
	err := h.DB.QueryRow(`SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&passwordHash)
	if err != nil {
		log.Printf("Error fetching user: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete account"})
		return
	}

	if !utils.CheckPassword(req.Password, passwordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid password"})
		return
	}

	// Delete user (CASCADE will handle related data)
	_, err = h.DB.Exec(`DELETE FROM users WHERE id = $1`, userID)
	if err != nil {
		log.Printf("Error deleting user: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete account"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Account deleted successfully"})
}

// ============================================================================
// GDPR EXPORT
// ============================================================================

func (h *UserHandler) ExportUserData(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	// Get user data
	var user models.User
	err := h.DB.QueryRow(`
		SELECT id, email, name, COALESCE(avatar, ''), 
		       totp_enabled, email_verified, created_at, updated_at
		FROM users
		WHERE id = $1
	`, userID).Scan(
		&user.ID, &user.Email, &user.Name, &user.Avatar,
		&user.TOTPEnabled, &user.EmailVerified, &user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		log.Printf("Error fetching user: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to export data"})
		return
	}

	// Get owned budgets
	rows, err := h.DB.Query(`
		SELECT id, name, created_at
		FROM budgets
		WHERE owner_id = $1
		ORDER BY created_at DESC
	`, userID)

	if err != nil {
		log.Printf("Error fetching budgets: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to export data"})
		return
	}
	defer rows.Close()

	budgets := []map[string]interface{}{}
	for rows.Next() {
		var id, name string
		var createdAt time.Time
		if err := rows.Scan(&id, &name, &createdAt); err != nil {
			continue
		}
		budgets = append(budgets, map[string]interface{}{
			"id":         id,
			"name":       name,
			"created_at": createdAt,
		})
	}

	// Compile export data
	exportData := map[string]interface{}{
		"user": map[string]interface{}{
			"id":             user.ID,
			"email":          user.Email,
			"name":           user.Name,
			"avatar":         user.Avatar,
			"totp_enabled":   user.TOTPEnabled,
			"email_verified": user.EmailVerified,
			"created_at":     user.CreatedAt,
			"updated_at":     user.UpdatedAt,
		},
		"owned_budgets": budgets,
		"export_date":   time.Now(),
		"format_version": "1.0",
	}

	c.Header("Content-Disposition", "attachment; filename=user-data-export.json")
	c.Header("Content-Type", "application/json")
	c.JSON(http.StatusOK, exportData)
}
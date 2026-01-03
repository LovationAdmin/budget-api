// handlers/auth.go
// ✅ VERSION CORRIGÉE - Signup sans country/postal_code

package handlers

import (
	"database/sql"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/LovationAdmin/budget-api/models"
	"github.com/LovationAdmin/budget-api/utils"
)

type AuthHandler struct {
	DB *sql.DB
}

// ============================================================================
// SIGNUP
// ============================================================================

func (h *AuthHandler) Signup(c *gin.Context) {
	var req models.SignupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check if user already exists
	var exists bool
	err := h.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)", req.Email).Scan(&exists)
	if err != nil {
		log.Printf("Error checking user existence: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	if exists {
		c.JSON(http.StatusConflict, gin.H{"error": "Email already registered"})
		return
	}

	// Hash password
	hashedPassword, err := utils.HashPassword(req.Password)
	if err != nil {
		log.Printf("Error hashing password: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create account"})
		return
	}

	// Create user
	userID := uuid.New().String()
	now := time.Now()

	// ✅ CORRIGÉ : Plus de country/postal_code dans Signup
	query := `
		INSERT INTO users (id, email, password_hash, name, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	
	_, err = h.DB.Exec(query, userID, req.Email, hashedPassword, req.Name, now, now)
	if err != nil {
		log.Printf("Error creating user: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create account"})
		return
	}

	// Generate email verification token
	verificationToken := uuid.New().String()
	expiresAt := time.Now().Add(24 * time.Hour)

	_, err = h.DB.Exec(`
		INSERT INTO email_verifications (id, user_id, token, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, uuid.New().String(), userID, verificationToken, expiresAt, now)

	if err != nil {
		log.Printf("Error creating verification token: %v", err)
	}

	// Send verification email
	go func() {
		if err := utils.SendVerificationEmail(req.Email, req.Name, verificationToken); err != nil {
			log.Printf("Error sending verification email: %v", err)
		}
	}()

	c.JSON(http.StatusCreated, gin.H{
		"message": "Account created successfully. Please check your email to verify your account.",
		"user_id": userID,
	})
}

// ============================================================================
// LOGIN
// ============================================================================

func (h *AuthHandler) Login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get user
	var user models.User
	err := h.DB.QueryRow(`
		SELECT id, email, password_hash, name, COALESCE(avatar, ''), 
		       totp_enabled, totp_secret, email_verified, created_at, updated_at
		FROM users
		WHERE email = $1
	`, req.Email).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.Name, &user.Avatar,
		&user.TOTPEnabled, &user.TOTPSecret, &user.EmailVerified,
		&user.CreatedAt, &user.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}
	if err != nil {
		log.Printf("Error fetching user: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	// Verify password
	if !utils.CheckPassword(req.Password, user.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Check if email is verified
	if !user.EmailVerified {
		c.JSON(http.StatusForbidden, gin.H{
			"error":          "Email not verified",
			"requires_verification": true,
		})
		return
	}

	// Check 2FA
	if user.TOTPEnabled {
		if req.TOTPCode == "" {
			c.JSON(http.StatusOK, gin.H{
				"requires_2fa": true,
				"message":      "2FA code required",
			})
			return
		}

		if !utils.VerifyTOTP(user.TOTPSecret, req.TOTPCode) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid 2FA code"})
			return
		}
	}

	// Generate tokens
	token, err := utils.GenerateJWT(user.ID, user.Email)
	if err != nil {
		log.Printf("Error generating JWT: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	refreshToken := uuid.New().String()
	expiresAt := time.Now().Add(30 * 24 * time.Hour)

	_, err = h.DB.Exec(`
		INSERT INTO sessions (id, user_id, refresh_token, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, uuid.New().String(), user.ID, refreshToken, expiresAt, time.Now())

	if err != nil {
		log.Printf("Error creating session: %v", err)
	}

	// Clear password hash before returning
	user.PasswordHash = ""
	user.TOTPSecret = ""

	c.JSON(http.StatusOK, models.LoginResponse{
		Token:        token,
		RefreshToken: refreshToken,
		User:         user,
	})
}

// ============================================================================
// EMAIL VERIFICATION
// ============================================================================

func (h *AuthHandler) VerifyEmail(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Token is required"})
		return
	}

	var userID string
	var expiresAt time.Time

	err := h.DB.QueryRow(`
		SELECT user_id, expires_at
		FROM email_verifications
		WHERE token = $1
	`, token).Scan(&userID, &expiresAt)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invalid or expired token"})
		return
	}
	if err != nil {
		log.Printf("Error fetching verification: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	if time.Now().After(expiresAt) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Token has expired"})
		return
	}

	// Update user
	_, err = h.DB.Exec(`UPDATE users SET email_verified = true WHERE id = $1`, userID)
	if err != nil {
		log.Printf("Error verifying email: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify email"})
		return
	}

	// Delete verification token
	_, err = h.DB.Exec(`DELETE FROM email_verifications WHERE token = $1`, token)
	if err != nil {
		log.Printf("Error deleting verification token: %v", err)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Email verified successfully"})
}

func (h *AuthHandler) ResendVerification(c *gin.Context) {
	var req struct {
		Email string `json:"email" binding:"required,email"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var userID, name string
	var emailVerified bool

	err := h.DB.QueryRow(`
		SELECT id, name, email_verified
		FROM users
		WHERE email = $1
	`, req.Email).Scan(&userID, &name, &emailVerified)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}
	if err != nil {
		log.Printf("Error fetching user: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	if emailVerified {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email already verified"})
		return
	}

	// Delete old tokens
	_, err = h.DB.Exec(`DELETE FROM email_verifications WHERE user_id = $1`, userID)
	if err != nil {
		log.Printf("Error deleting old tokens: %v", err)
	}

	// Generate new token
	verificationToken := uuid.New().String()
	expiresAt := time.Now().Add(24 * time.Hour)

	_, err = h.DB.Exec(`
		INSERT INTO email_verifications (id, user_id, token, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, uuid.New().String(), userID, verificationToken, expiresAt, time.Now())

	if err != nil {
		log.Printf("Error creating verification token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create verification token"})
		return
	}

	// Send email
	go func() {
		if err := utils.SendVerificationEmail(req.Email, name, verificationToken); err != nil {
			log.Printf("Error sending verification email: %v", err)
		}
	}()

	c.JSON(http.StatusOK, gin.H{"message": "Verification email sent"})
}

// ============================================================================
// PASSWORD RESET
// ============================================================================

func (h *AuthHandler) ForgotPassword(c *gin.Context) {
	var req struct {
		Email string `json:"email" binding:"required,email"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var userID, name string
	err := h.DB.QueryRow(`SELECT id, name FROM users WHERE email = $1`, req.Email).Scan(&userID, &name)

	if err == sql.ErrNoRows {
		// Don't reveal if email exists
		c.JSON(http.StatusOK, gin.H{"message": "If the email exists, a reset link will be sent"})
		return
	}
	if err != nil {
		log.Printf("Error fetching user: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	// Generate reset token
	resetToken := uuid.New().String()
	expiresAt := time.Now().Add(1 * time.Hour)

	_, err = h.DB.Exec(`
		INSERT INTO password_resets (id, user_id, token, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, uuid.New().String(), userID, resetToken, expiresAt, time.Now())

	if err != nil {
		log.Printf("Error creating reset token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create reset token"})
		return
	}

	// Send email
	go func() {
		if err := utils.SendPasswordResetEmail(req.Email, name, resetToken); err != nil {
			log.Printf("Error sending reset email: %v", err)
		}
	}()

	c.JSON(http.StatusOK, gin.H{"message": "If the email exists, a reset link will be sent"})
}

func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var req struct {
		Token    string `json:"token" binding:"required"`
		Password string `json:"password" binding:"required,min=8"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var userID string
	var expiresAt time.Time
	var used bool

	err := h.DB.QueryRow(`
		SELECT user_id, expires_at, used
		FROM password_resets
		WHERE token = $1
	`, req.Token).Scan(&userID, &expiresAt, &used)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invalid or expired token"})
		return
	}
	if err != nil {
		log.Printf("Error fetching reset token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	if used {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Token already used"})
		return
	}

	if time.Now().After(expiresAt) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Token has expired"})
		return
	}

	// Hash new password
	hashedPassword, err := utils.HashPassword(req.Password)
	if err != nil {
		log.Printf("Error hashing password: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset password"})
		return
	}

	// Update password
	_, err = h.DB.Exec(`UPDATE users SET password_hash = $1, updated_at = NOW() WHERE id = $2`, hashedPassword, userID)
	if err != nil {
		log.Printf("Error updating password: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset password"})
		return
	}

	// Mark token as used
	_, err = h.DB.Exec(`UPDATE password_resets SET used = true WHERE token = $1`, req.Token)
	if err != nil {
		log.Printf("Error marking token as used: %v", err)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Password reset successfully"})
}
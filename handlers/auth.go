// handlers/auth.go
package handlers

import (
	"database/sql"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/LovationAdmin/budget-api/models"
	"github.com/LovationAdmin/budget-api/services"
	"github.com/LovationAdmin/budget-api/utils"
)

type AuthHandler struct {
	DB           *sql.DB
	EmailService *services.EmailService
}

func NewAuthHandler(db *sql.DB) *AuthHandler {
	return &AuthHandler{
		DB:           db,
		EmailService: services.NewEmailService(),
	}
}

// âœ… HELPER: Cleans token if the frontend accidentally sends the full URL
func cleanToken(token string) string {
	// If token contains "token=", split it and take the last part
	if strings.Contains(token, "token=") {
		parts := strings.Split(token, "token=")
		if len(parts) > 1 {
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	return strings.TrimSpace(token)
}

// ============================================================================
// SIGNUP
// ============================================================================

type SignupRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
	Name     string `json:"name" binding:"required"`
}

func (h *AuthHandler) Signup(c *gin.Context) {
	var req SignupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	utils.SafeInfo("Signup attempt")

	var existingID string
	err := h.DB.QueryRow("SELECT id FROM users WHERE email = $1", req.Email).Scan(&existingID)
	if err == nil {
		utils.LogAuthAction("Signup", req.Email, false)
		c.JSON(http.StatusConflict, gin.H{"error": "Email already registered"})
		return
	}

	hashedPassword, err := utils.HashPassword(req.Password)
	if err != nil {
		utils.SafeError("Failed to hash password: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create account"})
		return
	}

	userID := uuid.New().String()
	_, err = h.DB.Exec(`
		INSERT INTO users (id, email, password_hash, name, email_verified, created_at, updated_at)
		VALUES ($1, $2, $3, $4, false, NOW(), NOW())
	`, userID, req.Email, hashedPassword, req.Name)

	if err != nil {
		utils.SafeError("Failed to insert user: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create account"})
		return
	}

	verificationToken := uuid.New().String()
	expiresAt := time.Now().Add(48 * time.Hour)

	_, err = h.DB.Exec(`
		INSERT INTO email_verification_tokens (user_id, token, expires_at, created_at)
		VALUES ($1, $2, $3, NOW())
	`, userID, verificationToken, expiresAt)

	if err != nil {
		utils.SafeWarn("Failed to create verification token: %v", err)
	}

	go func() {
		if err := h.EmailService.SendVerificationEmail(req.Email, req.Name, verificationToken); err != nil {
			utils.SafeWarn("Failed to send verification email: %v", err)
		}
	}()

	utils.LogAuthAction("Signup", req.Email, true)

	c.JSON(http.StatusCreated, gin.H{
		"message": "Account created successfully. Please check your email to verify your account.",
		"user": gin.H{
			"id":    userID,
			"email": req.Email,
			"name":  req.Name,
		},
	})
}

// ============================================================================
// LOGIN
// ============================================================================

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
	TOTPCode string `json:"totp_code,omitempty"`
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	utils.SafeInfo("Login attempt")

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
		utils.LogAuthAction("Login", req.Email, false)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
		return
	}

	if err != nil {
		utils.SafeError("Database error during login: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Login failed"})
		return
	}

	if !utils.CheckPassword(req.Password, passwordHash) {
		utils.LogAuthAction("Login", req.Email, false)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
		return
	}

	if !user.EmailVerified {
		utils.LogAuthAction("Login-Unverified", req.Email, false)
		c.JSON(http.StatusForbidden, gin.H{
			"error":              "Email not verified",
			"email_not_verified": true,
		})
		return
	}

	if user.TOTPEnabled && totpSecret.Valid {
		if req.TOTPCode == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":        "2FA code required",
				"requires_2fa": true,
			})
			return
		}

		valid, err := utils.VerifyTOTP(totpSecret.String, req.TOTPCode)
		if err != nil || !valid {
			utils.LogAuthAction("Login-2FA", req.Email, false)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid 2FA code"})
			return
		}
	}

	token, err := utils.GenerateAccessToken(user.ID, user.Email)
	if err != nil {
		utils.SafeError("Failed to generate token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Login failed"})
		return
	}

	utils.LogAuthAction("Login", req.Email, true)

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": gin.H{
			"id":           user.ID,
			"email":        user.Email,
			"name":         user.Name,
			"avatar":       user.Avatar,
			"totp_enabled": user.TOTPEnabled,
		},
	})
}

// ============================================================================
// VERIFY EMAIL
// ============================================================================

func (h *AuthHandler) VerifyEmail(c *gin.Context) {
	rawToken := c.Query("token")
	if rawToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Token de vÃ©rification manquant"})
		return
	}

	// âœ… FIX: Nettoyer le token (si le frontend envoie l'URL complÃ¨te)
	token := cleanToken(rawToken)

	utils.SafeInfo("Email verification attempt")

	var userID string
	var expiresAt time.Time

	err := h.DB.QueryRow(`
        SELECT user_id, expires_at FROM email_verification_tokens
        WHERE token = $1
    `, token).Scan(&userID, &expiresAt)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Lien de vÃ©rification invalide ou dÃ©jÃ  utilisÃ©"})
		return
	}

	if err != nil {
		utils.SafeError("Database error verifying email: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erreur lors de la vÃ©rification"})
		return
	}

	if time.Now().After(expiresAt) {
		utils.SafeWarn("Expired verification token used")
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Ce lien de vÃ©rification a expirÃ©. Demandez un nouveau lien.",
			"expired": true,
		})
		return
	}

	var alreadyVerified bool
	err = h.DB.QueryRow(`SELECT email_verified FROM users WHERE id = $1`, userID).Scan(&alreadyVerified)

	if err != nil {
		utils.SafeError("Failed to check email_verified status: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erreur lors de la vÃ©rification"})
		return
	}

	if alreadyVerified {
		h.DB.Exec("DELETE FROM email_verification_tokens WHERE token = $1", token)
		c.JSON(http.StatusOK, gin.H{
			"message":          "Email dÃ©jÃ  vÃ©rifiÃ©. Vous pouvez vous connecter.",
			"already_verified": true,
		})
		return
	}

	_, err = h.DB.Exec(`UPDATE users SET email_verified = true, updated_at = NOW() WHERE id = $1`, userID)
	if err != nil {
		utils.SafeError("Failed to update email_verified: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Impossible de vÃ©rifier l'email"})
		return
	}

	h.DB.Exec("DELETE FROM email_verification_tokens WHERE token = $1", token)

	utils.SafeInfo("Email verified successfully for user: %s", userID)

	c.JSON(http.StatusOK, gin.H{
		"message": "Email vÃ©rifiÃ© avec succÃ¨s !",
		"success": true,
	})
}

// ============================================================================
// RESEND VERIFICATION EMAIL
// ============================================================================

type ResendVerificationRequest struct {
	Email string `json:"email" binding:"required,email"`
}

func (h *AuthHandler) ResendVerificationEmail(c *gin.Context) {
	var req ResendVerificationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	utils.SafeInfo("Resend verification email request for: %s", req.Email)

	var userID, name string
	var emailVerified bool

	err := h.DB.QueryRow(`
        SELECT id, name, email_verified FROM users WHERE email = $1
    `, req.Email).Scan(&userID, &name, &emailVerified)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusOK, gin.H{"message": "Si un compte existe, un email de vÃ©rification a Ã©tÃ© envoyÃ©."})
		return
	}

	if emailVerified {
		c.JSON(http.StatusOK, gin.H{
			"message":          "Email dÃ©jÃ  vÃ©rifiÃ©. Vous pouvez vous connecter.",
			"already_verified": true,
		})
		return
	}

	var lastSentAt sql.NullTime
	err = h.DB.QueryRow(`
        SELECT created_at FROM email_verification_tokens 
        WHERE user_id = $1 ORDER BY created_at DESC LIMIT 1
    `, userID).Scan(&lastSentAt)

	if lastSentAt.Valid && time.Since(lastSentAt.Time) < 2*time.Minute {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Veuillez attendre 2 minutes avant de renvoyer un email."})
		return
	}

	h.DB.Exec("DELETE FROM email_verification_tokens WHERE user_id = $1", userID)

	verificationToken := uuid.New().String()
	expiresAt := time.Now().Add(48 * time.Hour)

	_, err = h.DB.Exec(`
        INSERT INTO email_verification_tokens (user_id, token, expires_at, created_at)
        VALUES ($1, $2, $3, NOW())
    `, userID, verificationToken, expiresAt)

	if err != nil {
		utils.SafeError("Failed to create verification token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Impossible de gÃ©nÃ©rer un nouveau lien"})
		return
	}

	go func() {
		if err := h.EmailService.SendVerificationEmail(req.Email, name, verificationToken); err != nil {
			utils.SafeWarn("Failed to send verification email: %v", err)
		}
	}()

	utils.SafeInfo("Verification email resent successfully")

	c.JSON(http.StatusOK, gin.H{
		"message": "Email de vÃ©rification envoyÃ© avec succÃ¨s.",
		"success": true,
	})
}

// ============================================================================
// FORGOT PASSWORD
// ============================================================================

type ForgotPasswordRequest struct {
	Email string `json:"email" binding:"required,email"`
}

func (h *AuthHandler) ForgotPassword(c *gin.Context) {
	var req ForgotPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	utils.SafeInfo("Password reset request")

	var userID, name string
	err := h.DB.QueryRow(`SELECT id, name FROM users WHERE email = $1`, req.Email).Scan(&userID, &name)

	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "If an account exists with this email, a password reset link has been sent."})
		return
	}

	h.DB.Exec("DELETE FROM password_reset_tokens WHERE user_id = $1", userID)

	resetToken := uuid.New().String()
	expiresAt := time.Now().Add(1 * time.Hour)

	_, err = h.DB.Exec(`
		INSERT INTO password_reset_tokens (user_id, token, expires_at, created_at)
		VALUES ($1, $2, $3, NOW())
	`, userID, resetToken, expiresAt)

	if err != nil {
		utils.SafeError("Failed to create reset token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process request"})
		return
	}

	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "https://budgetfamille.com"
	}
	resetLink := frontendURL + "/reset-password?token=" + resetToken

	go func() {
		if err := h.EmailService.SendPasswordResetEmail(req.Email, name, resetLink); err != nil {
			utils.SafeWarn("Failed to send password reset email: %v", err)
		}
	}()

	c.JSON(http.StatusOK, gin.H{"message": "If an account exists with this email, a password reset link has been sent."})
}

// ============================================================================
// RESET PASSWORD
// ============================================================================

type ResetPasswordRequest struct {
	Token       string `json:"token" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=8"`
}

func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var req ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// âœ… FIX: Nettoyer le token ici aussi
	cleanReqToken := cleanToken(req.Token)

	utils.SafeInfo("Password reset execution")

	var userID string
	var expiresAt time.Time

	err := h.DB.QueryRow(`
		SELECT user_id, expires_at FROM password_reset_tokens
		WHERE token = $1
	`, cleanReqToken).Scan(&userID, &expiresAt)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid or expired reset token"})
		return
	}

	if err != nil {
		utils.SafeError("Database error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process request"})
		return
	}

	if time.Now().After(expiresAt) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Reset token has expired"})
		return
	}

	hashedPassword, err := utils.HashPassword(req.NewPassword)
	if err != nil {
		utils.SafeError("Failed to hash password: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset password"})
		return
	}

	_, err = h.DB.Exec(`UPDATE users SET password_hash = $1, updated_at = NOW() WHERE id = $2`, hashedPassword, userID)

	if err != nil {
		utils.SafeError("Failed to update password: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset password"})
		return
	}

	h.DB.Exec("DELETE FROM password_reset_tokens WHERE token = $1", cleanReqToken)

	utils.SafeInfo("Password reset completed successfully")

	c.JSON(http.StatusOK, gin.H{"message": "Password reset successfully"})
}
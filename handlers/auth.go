// handlers/auth.go
// ✅ VERSION FINALE CORRIGÉE - Compatible avec votre codebase
// ⚠️ SANS country/postal_code (comme demandé pour la migration)

package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	
	"github.com/LovationAdmin/budget-api/utils"
)

// ============================================================================
// STRUCTURES
// ============================================================================

type AuthHandler struct {
	DB *sql.DB
}

type SignupRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
	Name     string `json:"name" binding:"required"`
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
	TOTPCode string `json:"totp_code"`
}

type ResendVerificationRequest struct {
	Email string `json:"email" binding:"required,email"`
}

type ForgotPasswordRequest struct {
	Email string `json:"email" binding:"required,email"`
}

type ResetPasswordRequest struct {
	Token       string `json:"token" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=8"`
}

// ============================================================================
// SIGNUP - ✅ SANS country/postal_code
// ============================================================================

func (h *AuthHandler) Signup(c *gin.Context) {
	var req SignupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check if user exists
	var exists bool
	err := h.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)", req.Email).Scan(&exists)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	if exists {
		c.JSON(http.StatusConflict, gin.H{"error": "Cet email est déjà utilisé"})
		return
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	// ✅ Create user SANS country/postal_code
	userID := uuid.New().String()
	_, err = h.DB.Exec(`
		INSERT INTO users (id, email, password_hash, name, created_at, updated_at, email_verified)
		VALUES ($1, $2, $3, $4, $5, $6, FALSE)
	`, userID, req.Email, string(hashedPassword), req.Name, time.Now(), time.Now())

	if err != nil {
		log.Printf("❌ Error creating user: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erreur création utilisateur"})
		return
	}

	log.Printf("✅ User created: %s", req.Email)

	// Create Verification Token
	verificationToken := uuid.New().String()
	expiresAt := time.Now().Add(24 * time.Hour)

	_, err = h.DB.Exec(`
        INSERT INTO email_verifications (user_id, token, expires_at)
        VALUES ($1, $2, $3)
    `, userID, verificationToken, expiresAt)

	if err != nil {
		log.Printf("Erreur insert verification: %v", err)
	}

	// Send Email
	go utils.SendVerificationEmail(req.Email, req.Name, verificationToken)

	c.JSON(http.StatusCreated, gin.H{
		"message":              "Compte créé. Veuillez vérifier vos emails pour l'activer.",
		"require_verification": true,
	})
}

// ============================================================================
// LOGIN
// ============================================================================

func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get user
	var userID, passwordHash, name string
	var totpEnabled, emailVerified bool
	var totpSecret sql.NullString

	err := h.DB.QueryRow(`
		SELECT id, password_hash, name, totp_enabled, totp_secret, email_verified
		FROM users WHERE email = $1
	`, req.Email).Scan(&userID, &passwordHash, &name, &totpEnabled, &totpSecret, &emailVerified)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Identifiants invalides"})
		return
	}

	if err != nil {
		log.Printf("❌ Database error during login: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Identifiants invalides"})
		return
	}

	// Check email verification
	if !emailVerified {
		c.JSON(http.StatusForbidden, gin.H{
			"error":        "Email non vérifié. Veuillez vérifier votre boîte de réception.",
			"not_verified": true,
		})
		return
	}

	// ✅ Check TOTP if enabled (CORRECT: utils.VerifyTOTP returns (bool, error))
	if totpEnabled {
		if req.TOTPCode == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "2FA code required", "require_totp": true})
			return
		}
		valid, _ := utils.VerifyTOTP(totpSecret.String, req.TOTPCode)
		if !valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Code 2FA invalide"})
			return
		}
	}

	// ✅ Generate token (using local generateJWT function)
	token, err := generateJWT(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": gin.H{
			"id":    userID,
			"email": req.Email,
			"name":  name,
		},
	})

	log.Printf("✅ User logged in: %s", req.Email)
}

// ============================================================================
// EMAIL VERIFICATION
// ============================================================================

func (h *AuthHandler) VerifyEmail(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Token manquant"})
		return
	}

	var userID string
	var expiresAt time.Time

	err := h.DB.QueryRow(`
        SELECT user_id, expires_at FROM email_verifications WHERE token = $1
    `, token).Scan(&userID, &expiresAt)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Lien invalide ou expiré"})
		return
	}

	if time.Now().After(expiresAt) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Le lien a expiré."})
		return
	}

	_, err = h.DB.Exec("UPDATE users SET email_verified = TRUE WHERE id = $1", userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erreur serveur"})
		return
	}

	h.DB.Exec("DELETE FROM email_verifications WHERE token = $1", token)

	log.Printf("✅ Email verified for user %s", userID)

	c.JSON(http.StatusOK, gin.H{"message": "Email vérifié avec succès ! Vous pouvez vous connecter."})
}

func (h *AuthHandler) ResendVerification(c *gin.Context) {
	var req ResendVerificationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var userID string
	var isVerified bool
	var name string

	err := h.DB.QueryRow("SELECT id, name, email_verified FROM users WHERE email = $1", req.Email).Scan(&userID, &name, &isVerified)
	
	if err == sql.ErrNoRows {
		c.JSON(http.StatusOK, gin.H{"message": "Si ce compte existe, un email a été envoyé."})
		return
	}

	if isVerified {
		c.JSON(http.StatusConflict, gin.H{"error": "Ce compte est déjà vérifié. Connectez-vous."})
		return
	}

	_, err = h.DB.Exec("DELETE FROM email_verifications WHERE user_id = $1", userID)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Erreur système"})
        return
    }

	verificationToken := uuid.New().String()
	expiresAt := time.Now().Add(24 * time.Hour)

	_, err = h.DB.Exec(`
		INSERT INTO email_verifications (user_id, token, expires_at)
		VALUES ($1, $2, $3)
	`, userID, verificationToken, expiresAt)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Impossible de générer le token"})
		return
	}

	go utils.SendVerificationEmail(req.Email, name, verificationToken)

	c.JSON(http.StatusOK, gin.H{"message": "Email de vérification envoyé !"})
}

// ============================================================================
// PASSWORD RESET
// ============================================================================

func (h *AuthHandler) ForgotPassword(c *gin.Context) {
	var req ForgotPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var userID, name string
	err := h.DB.QueryRow("SELECT id, name FROM users WHERE email = $1", req.Email).Scan(&userID, &name)
	
	if err == sql.ErrNoRows {
		c.JSON(http.StatusOK, gin.H{
			"message": "Si ce compte existe, un email de réinitialisation a été envoyé.",
		})
		return
	}

	if err != nil {
		log.Printf("❌ Error checking user existence: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erreur système"})
		return
	}

	_, err = h.DB.Exec("DELETE FROM password_resets WHERE user_id = $1 AND used = FALSE", userID)
	if err != nil {
		log.Printf("❌ Error cleaning old tokens: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erreur système"})
		return
	}

	resetToken := uuid.New().String()
	expiresAt := time.Now().Add(1 * time.Hour)

	_, err = h.DB.Exec(`
		INSERT INTO password_resets (user_id, token, expires_at, used)
		VALUES ($1, $2, $3, FALSE)
	`, userID, resetToken, expiresAt)

	if err != nil {
		log.Printf("❌ Error creating reset token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Impossible de générer le token"})
		return
	}

	go utils.SendPasswordResetEmail(req.Email, name, resetToken)

	log.Printf("✅ Password reset token created for user %s (%s)", userID, req.Email)

	c.JSON(http.StatusOK, gin.H{
		"message": "Si ce compte existe, un email de réinitialisation a été envoyé.",
	})
}

func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var req ResetPasswordRequest
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "Lien invalide ou expiré"})
		return
	}

	if err != nil {
		log.Printf("❌ Error checking reset token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erreur système"})
		return
	}

	if used {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ce lien a déjà été utilisé"})
		return
	}

	if time.Now().After(expiresAt) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Le lien a expiré. Veuillez faire une nouvelle demande."})
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("❌ Error hashing password: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erreur lors du traitement"})
		return
	}

	_, err = h.DB.Exec(`
		UPDATE users 
		SET password_hash = $1, updated_at = NOW()
		WHERE id = $2
	`, string(hashedPassword), userID)

	if err != nil {
		log.Printf("❌ Error updating password: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erreur lors de la mise à jour"})
		return
	}

	_, err = h.DB.Exec(`
		UPDATE password_resets 
		SET used = TRUE 
		WHERE token = $1
	`, req.Token)

	if err != nil {
		log.Printf("⚠️ Error marking token as used: %v", err)
	}

	log.Printf("✅ Password reset successful for user %s", userID)

	c.JSON(http.StatusOK, gin.H{
		"message": "Mot de passe réinitialisé avec succès. Vous pouvez maintenant vous connecter.",
	})
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

func generateJWT(userID string) (string, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		if os.Getenv("ENVIRONMENT") == "production" || os.Getenv("GIN_MODE") == "release" {
			return "", fmt.Errorf("JWT_SECRET is required in production")
		}
		secret = "dev-only-insecure-secret"
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID,
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
	})
	return token.SignedString([]byte(secret))
}
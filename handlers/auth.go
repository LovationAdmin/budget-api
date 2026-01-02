// handlers/auth.go
// ✅ VERSION OPTIMISÉE - Pour schéma avec country/postal_code
// ✅ ZÉRO RÉGRESSION - Colonnes garanties par migrations

package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/LovationAdmin/budget-api/utils"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// ============================================================================
// STRUCTURES
// ============================================================================

type AuthHandler struct {
	DB *sql.DB
}

// 🆕 UPDATED - Added optional Country and PostalCode
type SignupRequest struct {
	Email      string `json:"email" binding:"required,email"`
	Password   string `json:"password" binding:"required,min=8"`
	Name       string `json:"name" binding:"required"`
	Country    string `json:"country"`     // ✅ NEW (optional, defaults to FR)
	PostalCode string `json:"postal_code"` // ✅ NEW (optional)
}

// ✅ PRESERVED
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
	TOTPCode string `json:"totp_code"`
}

// ✅ PRESERVED
type ResendVerificationRequest struct {
	Email string `json:"email" binding:"required,email"`
}

// ✅ PRESERVED
type ForgotPasswordRequest struct {
	Email string `json:"email" binding:"required,email"`
}

// ✅ PRESERVED
type ResetPasswordRequest struct {
	Token       string `json:"token" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=8"`
}

// ============================================================================
// SIGNUP
// ============================================================================

func (h *AuthHandler) Signup(c *gin.Context) {
	var req SignupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 🆕 Handle country (default to FR if empty)
	country := strings.ToUpper(req.Country)
	if country == "" {
		country = "FR"
	}

	// 🆕 Validate country
	validCountries := map[string]bool{
		"FR": true, "BE": true, "DE": true, "ES": true, "IT": true,
		"PT": true, "NL": true, "LU": true, "AT": true, "IE": true,
	}

	if !validCountries[country] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Country not supported. Supported: FR, BE, DE, ES, IT, PT, NL, LU, AT, IE",
		})
		return
	}

	// ✅ EXISTING - Check if user exists
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

	// ✅ EXISTING - Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	// 🆕 UPDATED - Create user WITH country and postal_code
	userID := uuid.New().String()
	_, err = h.DB.Exec(`
		INSERT INTO users (id, email, password_hash, name, country, postal_code, created_at, updated_at, email_verified)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, FALSE)
	`, userID, req.Email, string(hashedPassword), req.Name, country, req.PostalCode, time.Now(), time.Now())

	if err != nil {
		log.Printf("❌ Error creating user: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erreur création utilisateur"})
		return
	}

	log.Printf("✅ User created: %s (Country: %s, Postal: %s)", req.Email, country, req.PostalCode)

	// ✅ EXISTING - Create Verification Token
	verificationToken := uuid.New().String()
	expiresAt := time.Now().Add(24 * time.Hour)

	_, err = h.DB.Exec(`
        INSERT INTO email_verifications (user_id, token, expires_at)
        VALUES ($1, $2, $3)
    `, userID, verificationToken, expiresAt)

	if err != nil {
		log.Printf("Erreur insert verification: %v", err)
	}

	// ✅ EXISTING - Send Email
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

	// 🆕 UPDATED - Get user WITH country and postal_code
	var userID, passwordHash, name string
	var country, postalCode sql.NullString
	var totpEnabled, emailVerified bool
	var totpSecret sql.NullString

	err := h.DB.QueryRow(`
		SELECT id, password_hash, name, totp_enabled, totp_secret, email_verified,
		       COALESCE(country, 'FR'), COALESCE(postal_code, '')
		FROM users WHERE email = $1
	`, req.Email).Scan(&userID, &passwordHash, &name, &totpEnabled, &totpSecret, &emailVerified, &country, &postalCode)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Identifiants invalides"})
		return
	}

	if err != nil {
		log.Printf("❌ Database error during login: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	// ✅ EXISTING - Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Identifiants invalides"})
		return
	}

	// ✅ EXISTING - Check email verification
	if !emailVerified {
		c.JSON(http.StatusForbidden, gin.H{
			"error":        "Email non vérifié. Veuillez vérifier votre boîte de réception.",
			"not_verified": true,
		})
		return
	}

	// ✅ EXISTING - Check TOTP if enabled
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

	// ✅ EXISTING - Generate token
	token, err := generateJWT(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	// 🆕 UPDATED - Include country and postal_code in response
	userResponse := gin.H{
		"id":    userID,
		"email": req.Email,
		"name":  name,
	}
	
	// Add location data if available
	if country.Valid && country.String != "" {
		userResponse["country"] = country.String
	}
	if postalCode.Valid && postalCode.String != "" {
		userResponse["postal_code"] = postalCode.String
	}

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user":  userResponse,
	})

	log.Printf("✅ User logged in: %s", req.Email)
}

// ============================================================================
// EMAIL VERIFICATION - PRESERVED 100%
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

// ============================================================================
// RESEND VERIFICATION - PRESERVED 100%
// ============================================================================

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
// FORGOT PASSWORD - PRESERVED 100%
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

// ============================================================================
// RESET PASSWORD - PRESERVED 100%
// ============================================================================

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
// HELPER FUNCTIONS - PRESERVED 100%
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
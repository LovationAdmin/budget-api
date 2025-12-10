package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"time"

	"budget-api/utils"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// --- STRUCTURES DÉFINIES ICI (C'était ce qui manquait) ---

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

// --- HANDLERS ---

// Signup crée un utilisateur et envoie un email de vérification
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

	// Create user (email_verified defaults to FALSE)
	userID := uuid.New().String()
	_, err = h.DB.Exec(`
		INSERT INTO users (id, email, password_hash, name, created_at, updated_at, email_verified)
		VALUES ($1, $2, $3, $4, $5, $6, FALSE)
	`, userID, req.Email, string(hashedPassword), req.Name, time.Now(), time.Now())

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erreur création utilisateur"})
		return
	}

	// Create Verification Token
	verificationToken := uuid.New().String()
	expiresAt := time.Now().Add(24 * time.Hour)

	_, err = h.DB.Exec(`
        INSERT INTO email_verifications (user_id, token, expires_at)
        VALUES ($1, $2, $3)
    `, userID, verificationToken, expiresAt)

	if err != nil {
		fmt.Println("Erreur insert verification:", err)
	}

	// Send Email (in goroutine to be faster)
	go utils.SendVerificationEmail(req.Email, req.Name, verificationToken)

	c.JSON(http.StatusCreated, gin.H{
		"message":              "Compte créé. Veuillez vérifier vos emails pour l'activer.",
		"require_verification": true,
	})
}

// Login connecte l'utilisateur et vérifie si l'email est validé
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

	// Note: On récupère aussi email_verified
	err := h.DB.QueryRow(`
		SELECT id, password_hash, name, totp_enabled, totp_secret, email_verified
		FROM users WHERE email = $1
	`, req.Email).Scan(&userID, &passwordHash, &name, &totpEnabled, &totpSecret, &emailVerified)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Identifiants invalides"})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Identifiants invalides"})
		return
	}

	// --- CHECK VERIFICATION ---
	if !emailVerified {
		c.JSON(http.StatusForbidden, gin.H{
			"error":        "Email non vérifié. Veuillez vérifier votre boîte de réception.",
			"not_verified": true,
		})
		return
	}

	// Check TOTP if enabled
	if totpEnabled {
		if req.TOTPCode == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "2FA code required", "require_totp": true})
			return
		}
		// Verify TOTP code here (logic in user handler or util)
		valid, _ := utils.VerifyTOTP(totpSecret.String, req.TOTPCode)
		if !valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Code 2FA invalide"})
			return
		}
	}

	// Generate token
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
}

// VerifyEmail valide le token reçu par email
func (h *AuthHandler) VerifyEmail(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Token manquant"})
		return
	}

	var userID string
	var expiresAt time.Time

	// Find Token
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

	// Activate User
	_, err = h.DB.Exec("UPDATE users SET email_verified = TRUE WHERE id = $1", userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erreur serveur"})
		return
	}

	// Delete Token
	h.DB.Exec("DELETE FROM email_verifications WHERE token = $1", token)

	c.JSON(http.StatusOK, gin.H{"message": "Email vérifié avec succès ! Vous pouvez vous connecter."})
}

// --- HELPER FUNCTIONS ---

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
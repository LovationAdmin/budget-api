package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

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
		c.JSON(http.StatusConflict, gin.H{"error": "User already exists"})
		return
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	// Create user
	userID := uuid.New().String()
	_, err = h.DB.Exec(`
		INSERT INTO users (id, email, password_hash, name, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, userID, req.Email, string(hashedPassword), req.Name, time.Now(), time.Now())

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	// Generate tokens
	token, err := generateJWT(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"token": token,
		"user": gin.H{
			"id":    userID,
			"email": req.Email,
			"name":  req.Name,
		},
	})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get user
	var userID, passwordHash, name string
	var totpEnabled bool
	var totpSecret sql.NullString

	err := h.DB.QueryRow(`
		SELECT id, password_hash, name, totp_enabled, totp_secret
		FROM users WHERE email = $1
	`, req.Email).Scan(&userID, &passwordHash, &name, &totpEnabled, &totpSecret)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Check TOTP if enabled
	if totpEnabled {
		if req.TOTPCode == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "2FA code required", "require_totp": true})
			return
		}
		// Verify TOTP code here (implement TOTP verification)
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

// handlers/auth.go (dans la fonction generateJWT)
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

func generateTOTPSecret() string {
	secret := make([]byte, 20)
	rand.Read(secret)
	return base32.StdEncoding.EncodeToString(secret)
}
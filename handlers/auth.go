package handlers

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"budget-api/models"
	"budget-api/utils"
)

type AuthHandler struct {
	DB *sql.DB
}

func (h *AuthHandler) Signup(c *gin.Context) {
	var req models.SignupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var exists bool
	err := h.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)", req.Email).Scan(&exists)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}
	if exists {
		c.JSON(http.StatusConflict, gin.H{"error": "Email already registered"})
		return
	}

	passwordHash, err := utils.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	var userID string
	err = h.DB.QueryRow(`
		INSERT INTO users (email, password_hash, name)
		VALUES ($1, $2, $3)
		RETURNING id
	`, req.Email, passwordHash, req.Name).Scan(&userID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	accessToken, err := utils.GenerateAccessToken(userID, req.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	refreshToken, err := utils.GenerateRefreshToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate refresh token"})
		return
	}

	_, err = h.DB.Exec(`
		INSERT INTO sessions (user_id, refresh_token, expires_at)
		VALUES ($1, $2, $3)
	`, userID, refreshToken, time.Now().Add(7*24*time.Hour))

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create session"})
		return
	}

	user := models.User{
		ID:            userID,
		Email:         req.Email,
		Name:          req.Name,
		TOTPEnabled:   false,
		EmailVerified: false,
		CreatedAt:     time.Now(),
	}

	c.JSON(http.StatusCreated, models.AuthResponse{
		User:         user,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var user models.User
	var passwordHash string
	var totpSecret sql.NullString

	err := h.DB.QueryRow(`
		SELECT id, email, password_hash, name, totp_secret, totp_enabled, email_verified, created_at, updated_at
		FROM users
		WHERE email = $1
	`, req.Email).Scan(&user.ID, &user.Email, &passwordHash, &user.Name, &totpSecret, &user.TOTPEnabled, &user.EmailVerified, &user.CreatedAt, &user.UpdatedAt)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	if !utils.CheckPassword(req.Password, passwordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	if user.TOTPEnabled {
		if req.TOTPCode == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "2FA code required", "requires_2fa": true})
			return
		}

		if totpSecret.Valid {
			valid, err := utils.VerifyTOTP(totpSecret.String, req.TOTPCode)
			if err != nil || !valid {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid 2FA code"})
				return
			}
		}
	}

	accessToken, err := utils.GenerateAccessToken(user.ID, user.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	refreshToken, err := utils.GenerateRefreshToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate refresh token"})
		return
	}

	_, err = h.DB.Exec(`
		INSERT INTO sessions (user_id, refresh_token, expires_at)
		VALUES ($1, $2, $3)
	`, user.ID, refreshToken, time.Now().Add(7*24*time.Hour))

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create session"})
		return
	}

	c.JSON(http.StatusOK, models.AuthResponse{
		User:         user,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	})
}
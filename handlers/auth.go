// handlers/auth.go
// ============================================================================
// AUTHENTIFICATION HANDLER - Login, Signup, Password Reset, Email Verification
// ============================================================================
// VERSION CORRIGÉE : Logging sécurisé sans données personnelles
// ============================================================================

package handlers

import (
	"database/sql"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/LovationAdmin/budget-api/models"
	"github.com/LovationAdmin/budget-api/services"
	"github.com/LovationAdmin/budget-api/utils"
)

// ============================================================================
// HANDLER STRUCT
// ============================================================================

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

	// ✅ LOGGING SÉCURISÉ - Pas d'email
	utils.SafeInfo("Signup attempt")

	// Vérifier si l'email existe déjà
	var existingID string
	err := h.DB.QueryRow("SELECT id FROM users WHERE email = $1", req.Email).Scan(&existingID)
	if err == nil {
		utils.LogAuthAction("Signup", req.Email, false)
		c.JSON(http.StatusConflict, gin.H{"error": "Email already registered"})
		return
	}

	// Hasher le mot de passe
	hashedPassword, err := utils.HashPassword(req.Password)
	if err != nil {
		utils.SafeError("Failed to hash password: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create account"})
		return
	}

	// Créer l'utilisateur
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

	// Créer le token de vérification email
	verificationToken := uuid.New().String()
	expiresAt := time.Now().Add(48 * time.Hour)

	_, err = h.DB.Exec(`
		INSERT INTO email_verification_tokens (user_id, token, expires_at, created_at)
		VALUES ($1, $2, $3, NOW())
	`, userID, verificationToken, expiresAt)

	if err != nil {
		utils.SafeWarn("Failed to create verification token: %v", err)
	}

	// Envoyer l'email de vérification
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "https://budgetfamille.com"
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

	// ✅ LOGGING SÉCURISÉ
	utils.SafeInfo("Login attempt")

	// Récupérer l'utilisateur
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

	// Vérifier le mot de passe
	if !utils.CheckPassword(req.Password, passwordHash) {
		utils.LogAuthAction("Login", req.Email, false)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
		return
	}

	// Vérifier si l'email est vérifié
	if !user.EmailVerified {
		utils.LogAuthAction("Login-Unverified", req.Email, false)
		c.JSON(http.StatusForbidden, gin.H{
			"error":           "Email not verified",
			"email_not_verified": true,
		})
		return
	}

	// Vérifier 2FA si activé
	if user.TOTPEnabled && totpSecret.Valid {
		if req.TOTPCode == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":         "2FA code required",
				"requires_2fa":  true,
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

	// Générer le token JWT
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
    token := c.Query("token")
    if token == "" {
        c.JSON(http.StatusBadRequest, gin.H{
            "error": "Token de vérification manquant",
        })
        return
    }

    utils.SafeInfo("Email verification attempt")

    // Vérifier le token
    var userID string
    var expiresAt time.Time

    err := h.DB.QueryRow(`
        SELECT user_id, expires_at FROM email_verification_tokens
        WHERE token = $1
    `, token).Scan(&userID, &expiresAt)

    if err == sql.ErrNoRows {
        // ✅ Message plus clair
        c.JSON(http.StatusBadRequest, gin.H{
            "error": "Lien de vérification invalide ou déjà utilisé",
        })
        return
    }

    if err != nil {
        utils.SafeError("Database error verifying email: %v", err)
        c.JSON(http.StatusInternalServerError, gin.H{
            "error": "Erreur lors de la vérification",
        })
        return
    }

    // ✅ FIX 3: Vérifier l'expiration AVANT de modifier l'utilisateur
    if time.Now().After(expiresAt) {
        utils.SafeWarn("Expired verification token used")
        c.JSON(http.StatusBadRequest, gin.H{
            "error": "Ce lien de vérification a expiré. Demandez un nouveau lien.",
            "expired": true, // ✅ Flag pour le frontend
        })
        return
    }

    // ✅ FIX 4: Vérifier si l'email est déjà vérifié
    var alreadyVerified bool
    err = h.DB.QueryRow(`
        SELECT email_verified FROM users WHERE id = $1
    `, userID).Scan(&alreadyVerified)

    if err != nil {
        utils.SafeError("Failed to check email_verified status: %v", err)
        c.JSON(http.StatusInternalServerError, gin.H{
            "error": "Erreur lors de la vérification",
        })
        return
    }

    if alreadyVerified {
        // ✅ Si déjà vérifié, supprimer le token et retourner succès
        h.DB.Exec("DELETE FROM email_verification_tokens WHERE token = $1", token)
        c.JSON(http.StatusOK, gin.H{
            "message": "Email déjà vérifié. Vous pouvez vous connecter.",
            "already_verified": true,
        })
        return
    }

    // Marquer l'email comme vérifié
    _, err = h.DB.Exec(`
        UPDATE users SET email_verified = true, updated_at = NOW()
        WHERE id = $1
    `, userID)

    if err != nil {
        utils.SafeError("Failed to update email_verified: %v", err)
        c.JSON(http.StatusInternalServerError, gin.H{
            "error": "Impossible de vérifier l'email",
        })
        return
    }

    // Supprimer le token utilisé
    h.DB.Exec("DELETE FROM email_verification_tokens WHERE token = $1", token)

    utils.SafeInfo("Email verified successfully for user: %s", userID)

    c.JSON(http.StatusOK, gin.H{
        "message": "Email vérifié avec succès !",
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

    // Récupérer l'utilisateur
    var userID, name string
    var emailVerified bool

    err := h.DB.QueryRow(`
        SELECT id, name, email_verified FROM users WHERE email = $1
    `, req.Email).Scan(&userID, &name, &emailVerified)

    if err == sql.ErrNoRows {
        // ✅ Ne pas révéler si l'email existe
        c.JSON(http.StatusOK, gin.H{
            "message": "Si un compte existe, un email de vérification a été envoyé.",
        })
        return
    }

    if err != nil {
        utils.SafeError("Database error: %v", err)
        c.JSON(http.StatusInternalServerError, gin.H{
            "error": "Erreur lors du traitement",
        })
        return
    }

    // Si déjà vérifié
    if emailVerified {
        c.JSON(http.StatusOK, gin.H{
            "message": "Email déjà vérifié. Vous pouvez vous connecter.",
            "already_verified": true,
        })
        return
    }

    // ✅ FIX 6: Vérifier la limite de renvoi (anti-spam)
    var lastSentAt sql.NullTime
    err = h.DB.QueryRow(`
        SELECT created_at FROM email_verification_tokens 
        WHERE user_id = $1 
        ORDER BY created_at DESC 
        LIMIT 1
    `, userID).Scan(&lastSentAt)

    if lastSentAt.Valid && time.Since(lastSentAt.Time) < 2*time.Minute {
        c.JSON(http.StatusTooManyRequests, gin.H{
            "error": "Veuillez attendre 2 minutes avant de renvoyer un email.",
        })
        return
    }

    // Supprimer les anciens tokens
    h.DB.Exec("DELETE FROM email_verification_tokens WHERE user_id = $1", userID)

    // Créer un nouveau token (48h)
    verificationToken := uuid.New().String()
    expiresAt := time.Now().Add(48 * time.Hour)

    _, err = h.DB.Exec(`
        INSERT INTO email_verification_tokens (user_id, token, expires_at, created_at)
        VALUES ($1, $2, $3, NOW())
    `, userID, verificationToken, expiresAt)

    if err != nil {
        utils.SafeError("Failed to create verification token: %v", err)
        c.JSON(http.StatusInternalServerError, gin.H{
            "error": "Impossible de générer un nouveau lien",
        })
        return
    }

    // Envoyer l'email
    frontendURL := os.Getenv("FRONTEND_URL")
    if frontendURL == "" {
        frontendURL = "https://budgetfamille.com"
    }

    go func() {
        if err := h.EmailService.SendVerificationEmail(req.Email, req.Name, verificationToken); err != nil {
            utils.SafeWarn("Failed to send verification email: %v", err)
        }
    }()

    utils.SafeInfo("Verification email resent successfully")

    c.JSON(http.StatusOK, gin.H{
        "message": "Email de vérification envoyé avec succès.",
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

	// Récupérer l'utilisateur
	var userID, name string

	err := h.DB.QueryRow(`
		SELECT id, name FROM users WHERE email = $1
	`, req.Email).Scan(&userID, &name)

	// Toujours retourner succès pour ne pas révéler si l'email existe
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "If an account exists with this email, a password reset link has been sent."})
		return
	}

	// Supprimer les anciens tokens de reset
	h.DB.Exec("DELETE FROM password_reset_tokens WHERE user_id = $1", userID)

	// Créer un nouveau token
	resetToken := uuid.New().String()
	expiresAt := time.Now().Add(1 * time.Hour) // Expire dans 1 heure

	_, err = h.DB.Exec(`
		INSERT INTO password_reset_tokens (user_id, token, expires_at, created_at)
		VALUES ($1, $2, $3, NOW())
	`, userID, resetToken, expiresAt)

	if err != nil {
		utils.SafeError("Failed to create reset token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process request"})
		return
	}

	// Envoyer l'email
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

	utils.SafeInfo("Password reset execution")

	// Vérifier le token
	var userID string
	var expiresAt time.Time

	err := h.DB.QueryRow(`
		SELECT user_id, expires_at FROM password_reset_tokens
		WHERE token = $1
	`, req.Token).Scan(&userID, &expiresAt)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid or expired reset token"})
		return
	}

	if err != nil {
		utils.SafeError("Database error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process request"})
		return
	}

	// Vérifier l'expiration
	if time.Now().After(expiresAt) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Reset token has expired"})
		return
	}

	// Hasher le nouveau mot de passe
	hashedPassword, err := utils.HashPassword(req.NewPassword)
	if err != nil {
		utils.SafeError("Failed to hash password: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset password"})
		return
	}

	// Mettre à jour le mot de passe
	_, err = h.DB.Exec(`
		UPDATE users SET password_hash = $1, updated_at = NOW()
		WHERE id = $2
	`, hashedPassword, userID)

	if err != nil {
		utils.SafeError("Failed to update password: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset password"})
		return
	}

	// Supprimer le token utilisé
	h.DB.Exec("DELETE FROM password_reset_tokens WHERE token = $1", req.Token)

	utils.SafeInfo("Password reset completed successfully")

	c.JSON(http.StatusOK, gin.H{"message": "Password reset successfully"})
}
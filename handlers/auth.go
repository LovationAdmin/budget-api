package handlers

import (
	"database/sql"
	"net/http"
	"time"
    "os"
    "fmt" // Ajoutez fmt s'il manque

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
    "budget-api/utils" // Import utils pour l'email
)

// ... (Structs AuthHandler, SignupRequest, LoginRequest restent identiques)

// 1. SIGNUP MODIFIÉ
func (h *AuthHandler) Signup(c *gin.Context) {
	var req SignupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

    // Check user exists (identique...)
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

    // Hash password (identique...)
	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)

	// Create user (identique...)
    // NOTE: email_verified est FALSE par défaut dans la DB (ou NULL), donc c'est bon.
	userID := uuid.New().String()
	_, err = h.DB.Exec(`
		INSERT INTO users (id, email, password_hash, name, created_at, updated_at, email_verified)
		VALUES ($1, $2, $3, $4, $5, $6, FALSE)
	`, userID, req.Email, string(hashedPassword), req.Name, time.Now(), time.Now())

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erreur création utilisateur"})
		return
	}

    // --- CHANGEMENT ICI : Génération Token de Vérification ---
    verificationToken := uuid.New().String()
    expiresAt := time.Now().Add(24 * time.Hour)

    _, err = h.DB.Exec(`
        INSERT INTO email_verifications (user_id, token, expires_at)
        VALUES ($1, $2, $3)
    `, userID, verificationToken, expiresAt)

    if err != nil {
        // On log l'erreur mais on ne fail pas la requête HTTP critique, l'user pourra demander un renvoi
        fmt.Println("Erreur insert verification:", err)
    }

    // Envoi Email
    go utils.SendVerificationEmail(req.Email, req.Name, verificationToken)

    // --- FIN CHANGEMENT : On ne renvoie PAS de JWT ---
	c.JSON(http.StatusCreated, gin.H{
		"message": "Compte créé. Veuillez vérifier vos emails pour l'activer.",
        "require_verification": true,
	})
}

// 2. LOGIN MODIFIÉ
func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

    // On récupère aussi email_verified
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

    // Verify Password
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Identifiants invalides"})
		return
	}

    // --- CHECK VERIFICATION ---
    if !emailVerified {
        c.JSON(http.StatusForbidden, gin.H{
            "error": "Email non vérifié. Veuillez vérifier votre boîte de réception.",
            "not_verified": true,
        })
        return
    }
    // --------------------------

    // ... (Reste de la logique TOTP et JWT identique) ...
    // ...
    // Generate tokens
	token, _ := generateJWT(userID) // Assurez-vous que generateJWT est dispo dans le package ou copié ici
	
    c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": gin.H{"id": userID, "email": req.Email, "name": name},
	})
}

// 3. NOUVEAU HANDLER : VERIFY EMAIL
func (h *AuthHandler) VerifyEmail(c *gin.Context) {
    token := c.Query("token")
    if token == "" {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Token manquant"})
        return
    }

    var userID string
    var expiresAt time.Time

    // Trouver le token
    err := h.DB.QueryRow(`
        SELECT user_id, expires_at FROM email_verifications WHERE token = $1
    `, token).Scan(&userID, &expiresAt)

    if err == sql.ErrNoRows {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Lien invalide ou expiré"})
        return
    }

    if time.Now().After(expiresAt) {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Le lien a expiré. Connectez-vous pour en recevoir un nouveau."})
        return
    }

    // Valider l'utilisateur
    _, err = h.DB.Exec("UPDATE users SET email_verified = TRUE WHERE id = $1", userID)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Erreur serveur"})
        return
    }

    // Nettoyer le token
    h.DB.Exec("DELETE FROM email_verifications WHERE token = $1", token)

    c.JSON(http.StatusOK, gin.H{"message": "Email vérifié avec succès ! Vous pouvez vous connecter."})
}
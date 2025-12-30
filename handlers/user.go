// handlers/user.go
// VERSION COMPLÈTE AVEC EXPORT RGPD

package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
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
		       COALESCE(country, 'FR'),
		       COALESCE(postal_code, ''),
		       totp_enabled, email_verified, 
		       created_at, updated_at
		FROM users
		WHERE id = $1
	`, userID).Scan(
		&user.ID,
		&user.Email,
		&user.Name,
		&user.Avatar,
		&user.Country,
		&user.PostalCode,
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

	c.JSON(http.StatusOK, gin.H{
		"message": "Profile updated successfully",
		"user": gin.H{
			"name":   req.Name,
			"avatar": req.Avatar,
		},
	})
}

// ============================================================================
// LOCATION MANAGEMENT
// ============================================================================

type UpdateLocationRequest struct {
	Country    string `json:"country" binding:"required,len=2"`
	PostalCode string `json:"postal_code"`
}

func (h *UserHandler) UpdateLocation(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req UpdateLocationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid country code (must be 2 characters)"})
		return
	}

	validCountries := map[string]bool{
		"FR": true, "BE": true, "DE": true, "ES": true, "IT": true,
		"PT": true, "NL": true, "LU": true, "AT": true, "IE": true,
	}

	countryUpper := strings.ToUpper(req.Country)
	if !validCountries[countryUpper] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Country not supported. Supported countries: FR, BE, DE, ES, IT, PT, NL, LU, AT, IE",
		})
		return
	}

	_, err := h.DB.Exec(`
		UPDATE users
		SET country = $1, postal_code = $2, updated_at = NOW()
		WHERE id = $3
	`, countryUpper, req.PostalCode, userID)

	if err != nil {
		log.Printf("Failed to update location for user %s: %v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update location"})
		return
	}

	log.Printf("✅ User %s location updated: %s %s", userID, countryUpper, req.PostalCode)

	c.JSON(http.StatusOK, gin.H{
		"message":     "Location updated successfully",
		"country":     countryUpper,
		"postal_code": req.PostalCode,
	})
}

func (h *UserHandler) GetLocation(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var country, postalCode string
	err := h.DB.QueryRow(`
		SELECT COALESCE(country, 'FR'), COALESCE(postal_code, '')
		FROM users
		WHERE id = $1
	`, userID).Scan(&country, &postalCode)

	if err != nil {
		log.Printf("Error fetching location: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch location"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"country":     country,
		"postal_code": postalCode,
	})
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

	var currentHash string
	err := h.DB.QueryRow(`
		SELECT password_hash FROM users WHERE id = $1
	`, userID).Scan(&currentHash)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify password"})
		return
	}

	if !utils.CheckPassword(req.CurrentPassword, currentHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Current password is incorrect"})
		return
	}

	newHash, err := utils.HashPassword(req.NewPassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash new password"})
		return
	}

	_, err = h.DB.Exec(`
		UPDATE users
		SET password_hash = $1, updated_at = NOW()
		WHERE id = $2
	`, newHash, userID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update password"})
		return
	}

	log.Printf("✅ User %s password changed successfully", userID)

	c.JSON(http.StatusOK, gin.H{"message": "Password changed successfully"})
}

// ============================================================================
// 2FA MANAGEMENT
// ============================================================================

func (h *UserHandler) SetupTOTP(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var email string
	err := h.DB.QueryRow(`SELECT email FROM users WHERE id = $1`, userID).Scan(&email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get user"})
		return
	}

	secret, qrCode, err := utils.GenerateTOTPSecret(email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate TOTP"})
		return
	}

	_, err = h.DB.Exec(`
		UPDATE users
		SET totp_secret = $1, updated_at = NOW()
		WHERE id = $2
	`, secret, userID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store TOTP secret"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"secret":  secret,
		"qr_code": qrCode,
	})
}

type VerifyTOTPRequest struct {
	Code string `json:"code" binding:"required"`
}

func (h *UserHandler) VerifyTOTP(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req VerifyTOTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var secret sql.NullString
	err := h.DB.QueryRow(`
		SELECT totp_secret FROM users WHERE id = $1
	`, userID).Scan(&secret)

	if err != nil || !secret.Valid {
		c.JSON(http.StatusBadRequest, gin.H{"error": "TOTP not set up"})
		return
	}

	valid, err := utils.VerifyTOTP(secret.String, req.Code)
	if err != nil || !valid {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid TOTP code"})
		return
	}

	_, err = h.DB.Exec(`
		UPDATE users
		SET totp_enabled = TRUE, updated_at = NOW()
		WHERE id = $1
	`, userID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to enable 2FA"})
		return
	}

	log.Printf("✅ 2FA enabled for user %s", userID)

	c.JSON(http.StatusOK, gin.H{
		"message": "2FA enabled successfully",
		"enabled": true,
	})
}

type DisableTOTPRequest struct {
	Password string `json:"password" binding:"required"`
	Code     string `json:"code" binding:"required"`
}

func (h *UserHandler) DisableTOTP(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req DisableTOTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var passwordHash string
	var secret sql.NullString
	err := h.DB.QueryRow(`
		SELECT password_hash, totp_secret FROM users WHERE id = $1
	`, userID).Scan(&passwordHash, &secret)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify credentials"})
		return
	}

	if !utils.CheckPassword(req.Password, passwordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid password"})
		return
	}

	if secret.Valid {
		valid, err := utils.VerifyTOTP(secret.String, req.Code)
		if err != nil || !valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid 2FA code"})
			return
		}
	}

	_, err = h.DB.Exec(`
		UPDATE users
		SET totp_enabled = FALSE, totp_secret = NULL, updated_at = NOW()
		WHERE id = $1
	`, userID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to disable 2FA"})
		return
	}

	log.Printf("✅ 2FA disabled for user %s", userID)

	c.JSON(http.StatusOK, gin.H{
		"message": "2FA disabled successfully",
		"enabled": false,
	})
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

	var passwordHash string
	err := h.DB.QueryRow(`
		SELECT password_hash FROM users WHERE id = $1
	`, userID).Scan(&passwordHash)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify password"})
		return
	}

	if !utils.CheckPassword(req.Password, passwordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid password"})
		return
	}

	_, err = h.DB.Exec(`DELETE FROM users WHERE id = $1`, userID)

	if err != nil {
		log.Printf("Error deleting account: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete account"})
		return
	}

	log.Printf("✅ User %s account deleted", userID)

	c.JSON(http.StatusOK, gin.H{"message": "Account deleted successfully"})
}

// ============================================================================
// GDPR DATA EXPORT
// ============================================================================

func (h *UserHandler) ExportUserData(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	log.Printf("📊 [GDPR Export] User %s requested data export", userID)

	// 1. Get user profile
	var user models.User
	err := h.DB.QueryRow(`
		SELECT id, email, name, 
		       COALESCE(avatar, ''), 
		       COALESCE(country, 'FR'),
		       COALESCE(postal_code, ''),
		       totp_enabled, email_verified, 
		       created_at, updated_at
		FROM users
		WHERE id = $1
	`, userID).Scan(
		&user.ID,
		&user.Email,
		&user.Name,
		&user.Avatar,
		&user.Country,
		&user.PostalCode,
		&user.TOTPEnabled,
		&user.EmailVerified,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		log.Printf("❌ [GDPR Export] Failed to fetch user profile: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch user data"})
		return
	}

	// 2. Get user's budgets (only the ones they OWN)
	budgetRows, err := h.DB.Query(`
		SELECT 
			b.id,
			b.name,
			b.year,
			b.created_at,
			b.updated_at,
			bd.data
		FROM budgets b
		LEFT JOIN budget_data bd ON b.id = bd.budget_id
		WHERE b.owner_id = $1
		ORDER BY b.created_at DESC
	`, userID)

	if err != nil {
		log.Printf("❌ [GDPR Export] Failed to fetch budgets: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch budget data"})
		return
	}
	defer budgetRows.Close()

	type BudgetExport struct {
		ID        string                 `json:"id"`
		Name      string                 `json:"name"`
		Year      int                    `json:"year"`
		CreatedAt string                 `json:"created_at"`
		UpdatedAt string                 `json:"updated_at"`
		Data      map[string]interface{} `json:"data,omitempty"`
	}

	var budgets []BudgetExport
	for budgetRows.Next() {
		var budget BudgetExport
		var rawData []byte
		var createdAt, updatedAt interface{}

		err := budgetRows.Scan(
			&budget.ID,
			&budget.Name,
			&budget.Year,
			&createdAt,
			&updatedAt,
			&rawData,
		)

		if err != nil {
			log.Printf("⚠️ [GDPR Export] Error scanning budget: %v", err)
			continue
		}

		if t, ok := createdAt.([]uint8); ok {
			budget.CreatedAt = string(t)
		}
		if t, ok := updatedAt.([]uint8); ok {
			budget.UpdatedAt = string(t)
		}

		// Decrypt and parse budget data if present
		if len(rawData) > 0 {
			var wrapper struct {
				Encrypted string `json:"encrypted"`
			}

			if err := json.Unmarshal(rawData, &wrapper); err == nil && wrapper.Encrypted != "" {
				decryptedBytes, err := utils.Decrypt(wrapper.Encrypted)
				if err != nil {
					log.Printf("⚠️ [GDPR Export] Failed to decrypt budget %s: %v", budget.ID, err)
					budget.Data = map[string]interface{}{
						"error": "Could not decrypt budget data",
					}
				} else {
					if err := json.Unmarshal(decryptedBytes, &budget.Data); err != nil {
						log.Printf("⚠️ [GDPR Export] Failed to unmarshal decrypted data: %v", err)
						budget.Data = map[string]interface{}{
							"error": "Could not parse decrypted data",
						}
					}
				}
			} else {
				if err := json.Unmarshal(rawData, &budget.Data); err != nil {
					log.Printf("⚠️ [GDPR Export] Failed to unmarshal budget data: %v", err)
					budget.Data = map[string]interface{}{
						"error": "Could not parse budget data",
					}
				}
			}
		}

		budgets = append(budgets, budget)
	}

	// 3. Get budgets where user is a member (shared budgets)
	sharedBudgetRows, err := h.DB.Query(`
		SELECT 
			b.id,
			b.name,
			b.year,
			bm.role,
			bm.created_at
		FROM budget_members bm
		JOIN budgets b ON bm.budget_id = b.id
		WHERE bm.user_id = $1 AND b.owner_id != $1
		ORDER BY bm.created_at DESC
	`, userID)

	if err != nil {
		log.Printf("⚠️ [GDPR Export] Failed to fetch shared budgets: %v", err)
	}

	type SharedBudgetExport struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Year     int    `json:"year"`
		Role     string `json:"role"`
		JoinedAt string `json:"joined_at"`
	}

	var sharedBudgets []SharedBudgetExport
	if sharedBudgetRows != nil {
		defer sharedBudgetRows.Close()
		for sharedBudgetRows.Next() {
			var sb SharedBudgetExport
			var joinedAt interface{}

			err := sharedBudgetRows.Scan(
				&sb.ID,
				&sb.Name,
				&sb.Year,
				&sb.Role,
				&joinedAt,
			)

			if err != nil {
				log.Printf("⚠️ [GDPR Export] Error scanning shared budget: %v", err)
				continue
			}

			if t, ok := joinedAt.([]uint8); ok {
				sb.JoinedAt = string(t)
			}

			sharedBudgets = append(sharedBudgets, sb)
		}
	}

	// 4. Get pending invitations
	invitationRows, err := h.DB.Query(`
		SELECT 
			i.id,
			i.budget_id,
			b.name as budget_name,
			i.status,
			i.created_at
		FROM invitations i
		JOIN budgets b ON i.budget_id = b.id
		WHERE i.email = (SELECT email FROM users WHERE id = $1)
		ORDER BY i.created_at DESC
	`, userID)

	if err != nil {
		log.Printf("⚠️ [GDPR Export] Failed to fetch invitations: %v", err)
	}

	type InvitationExport struct {
		ID         string `json:"id"`
		BudgetID   string `json:"budget_id"`
		BudgetName string `json:"budget_name"`
		Status     string `json:"status"`
		CreatedAt  string `json:"created_at"`
	}

	var invitations []InvitationExport
	if invitationRows != nil {
		defer invitationRows.Close()
		for invitationRows.Next() {
			var inv InvitationExport
			var createdAt interface{}

			err := invitationRows.Scan(
				&inv.ID,
				&inv.BudgetID,
				&inv.BudgetName,
				&inv.Status,
				&createdAt,
			)

			if err != nil {
				log.Printf("⚠️ [GDPR Export] Error scanning invitation: %v", err)
				continue
			}

			if t, ok := createdAt.([]uint8); ok {
				inv.CreatedAt = string(t)
			}

			invitations = append(invitations, inv)
		}
	}

	// 5. Build final export
	exportData := gin.H{
		"export_info": gin.H{
			"generated_at": time.Now().Format(time.RFC3339),
			"user_id":      userID,
			"format":       "JSON",
			"compliance":   "GDPR Article 20 - Right to Data Portability",
		},
		"user_profile": gin.H{
			"id":             user.ID,
			"email":          user.Email,
			"name":           user.Name,
			"avatar":         user.Avatar,
			"country":        user.Country,
			"postal_code":    user.PostalCode,
			"totp_enabled":   user.TOTPEnabled,
			"email_verified": user.EmailVerified,
			"created_at":     user.CreatedAt.Format(time.RFC3339),
			"updated_at":     user.UpdatedAt.Format(time.RFC3339),
		},
		"owned_budgets":  budgets,
		"shared_budgets": sharedBudgets,
		"invitations":    invitations,
		"note":           "This export contains only YOUR personal data. Data from other users in shared budgets is excluded for privacy reasons.",
	}

	log.Printf("✅ [GDPR Export] Successfully generated export for user %s", userID)

	c.JSON(http.StatusOK, exportData)
}
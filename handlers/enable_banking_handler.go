package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"
	"os"

	"budget-api/middleware"
	"budget-api/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type EnableBankingHandler struct {
	DB                    *sql.DB
	Service               *services.BankingService
	EnableBankingService  *services.EnableBankingService
}

func NewEnableBankingHandler(db *sql.DB) *EnableBankingHandler {
	return &EnableBankingHandler{
		DB:                   db,
		Service:              services.NewBankingService(db),
		EnableBankingService: services.NewEnableBankingService(),
	}
}

// ========== 1. GET BANKS (ASPSPs) ==========

// GET /api/v1/banking/enablebanking/banks?country=FR
func (h *EnableBankingHandler) GetBanks(c *gin.Context) {
	country := c.DefaultQuery("country", "FR")
	
	aspsps, err := h.EnableBankingService.GetASPSPs(c.Request.Context(), country)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to fetch banks",
			"details": err.Error(),
		})
		return
	}

	// Format pour le frontend
	var banks []map[string]interface{}
	for _, aspsp := range aspsps {
		bank := map[string]interface{}{
			"id":      aspsp.Name, // Utiliser le nom comme ID
			"name":    aspsp.Name,
			"country": aspsp.Country,
			"logo":    aspsp.Logo,
			"beta":    aspsp.Beta,
		}
		
		// Ajouter BIC s'il existe
		if aspsp.BIC != "" {
			bank["bic"] = aspsp.BIC
		}
		
		// Ajouter info sandbox si disponible
		if aspsp.Sandbox != nil {
			bank["sandbox"] = true
			bank["sandbox_users"] = aspsp.Sandbox.Users
		} else {
			bank["sandbox"] = false
		}
		
		banks = append(banks, bank)
	}

	c.JSON(http.StatusOK, gin.H{"banks": banks})
}

// ========== 2. CREATE CONNECTION (Auth Request) ==========

// POST /api/v1/banking/enablebanking/connect
// Body: { "aspsp_id": "ASPSP_NAME" }
func (h *EnableBankingHandler) CreateConnection(c *gin.Context) {
	var req struct {
		ASPSPID string `json:"aspsp_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "aspsp_id is required"})
		return
	}

	// Générer un state unique pour cette demande
	state := uuid.New().String()

	// Calculer la date de validité (90 jours dans le futur)
	validUntil := time.Now().AddDate(0, 0, 90).Format(time.RFC3339)

	// Déterminer l'URL de callback (production vs développement)
	callbackURL := os.Getenv("FRONTEND_URL")
	if callbackURL == "" {
		callbackURL = "https://www.budgetfamille.com" // URL de production par défaut
	}
	callbackURL += "/beta2/callback"

	// Créer la demande d'autorisation selon le format Enable Banking
	authReq := services.AuthRequest{
		Access: services.Access{
			ValidUntil: validUntil,
		},
		ASPSP: services.ASPSPIdentifier{
			Name:    req.ASPSPID, // Le frontend envoie le nom de la banque
			Country: "FR",        // TODO: rendre dynamique si support multi-pays
		},
		State:       state,
		RedirectURL: callbackURL,
		PSUType:     "personal", // TODO: rendre dynamique
	}

	authResp, err := h.EnableBankingService.CreateAuthRequest(c.Request.Context(), authReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to create connection",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"redirect_url": authResp.AuthURL,
		"state":        authResp.State,
	})
}

// ========== 3. CALLBACK (After Bank Authorization) ==========

// GET /api/v1/banking/enablebanking/callback?code=xxx&state=xxx
func (h *EnableBankingHandler) HandleCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")

	if code == "" || state == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing code or state"})
		return
	}

	// TODO: Valider le state

	// Créer la session
	sessionResp, err := h.EnableBankingService.CreateSession(c.Request.Context(), code, state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create session",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"session_id": sessionResp.SessionID,
		"accounts":   sessionResp.Accounts,
	})
}

// ========== 4. SYNC ACCOUNTS (Dans le Budget) ==========

// POST /api/v1/budgets/:id/banking/enablebanking/sync
// Body: { "session_id": "xxx" }
func (h *EnableBankingHandler) SyncAccounts(c *gin.Context) {
	userID := middleware.GetUserID(c)
	budgetID := c.Param("id")

	var req struct {
		SessionID string `json:"session_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id is required"})
		return
	}

	// 1. Récupérer les comptes depuis Enable Banking
	accounts, err := h.EnableBankingService.GetAccounts(c.Request.Context(), req.SessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to fetch accounts",
			"details": err.Error(),
		})
		return
	}

	if len(accounts) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"message": "No accounts found",
			"accounts_synced": 0,
		})
		return
	}

	accountsSynced := 0

	// 2. Pour chaque compte, créer/mettre à jour dans la DB
	for _, acc := range accounts {
		// Extraire l'IBAN ou autre identifiant
		accountIdentifier := acc.AccountID.IBAN
		if accountIdentifier == "" && acc.AccountID.Other != nil {
			accountIdentifier = acc.AccountID.Other.Identification
		}
		
		// Utiliser l'UID comme external_account_id (c'est ce qu'on utilise pour les API calls)
		externalAccountID := acc.UID
		if externalAccountID == "" {
			// Fallback sur l'IBAN si UID pas disponible
			externalAccountID = accountIdentifier
		}
		
		log.Printf("💳 Processing account: %s (ID: %s)", acc.Name, externalAccountID)
		
		// A. Créer/récupérer la connexion
		connID, err := h.Service.SaveConnectionWithTokens(
			c.Request.Context(),
			userID,
			budgetID,
			externalAccountID,           // provider_id
			"Enable Banking",             // institution_name
			req.SessionID,                // provider_connection_id
			"enablebanking-managed",      // access_token
			"",                           // refresh_token
			time.Now().AddDate(0, 3, 0), // expires_at
		)

		if err != nil {
			log.Printf("❌ Error creating connection for account %s: %v", acc.Name, err)
			continue
		}

		// B. Récupérer le solde depuis Enable Banking
		balance := 0.0
		balances, err := h.EnableBankingService.GetBalances(
			c.Request.Context(),
			req.SessionID,
			externalAccountID,
		)
		
		if err != nil {
			log.Printf("⚠️  Could not fetch balance for %s: %v", acc.Name, err)
			// Continue quand même sans balance
		} else if len(balances) > 0 {
			// Prendre le premier solde disponible (généralement "Booked balance")
			amountStr := balances[0].BalanceAmount.Amount
			if parsed, err := strconv.ParseFloat(amountStr, 64); err == nil {
				balance = parsed
				log.Printf("💰 Balance for %s: %.2f %s", acc.Name, balance, balances[0].BalanceAmount.Currency)
			} else {
				log.Printf("⚠️  Could not parse balance amount '%s': %v", amountStr, err)
			}
		}

		// C. Sauvegarder le compte
		mask := accountIdentifier
		if len(mask) > 4 {
			mask = mask[len(mask)-4:]
		}

		err = h.Service.SaveAccount(
			c.Request.Context(),
			connID,
			externalAccountID,
			acc.Name,
			mask,
			acc.Currency,
			balance,
		)

		if err != nil {
			log.Printf("❌ Error saving account %s: %v", acc.Name, err)
		} else {
			log.Printf("✅ Account saved: %s (%.2f %s)", acc.Name, balance, acc.Currency)
			accountsSynced++
		}
	}

	log.Printf("🎉 Sync complete: %d/%d accounts synced", accountsSynced, len(accounts))

	c.JSON(http.StatusOK, gin.H{
		"message": "Accounts synchronized successfully",
		"accounts_synced": accountsSynced,
	})
}

// ========== 5. REFRESH BALANCES ==========

// POST /api/v1/banking/enablebanking/refresh
// Body: { "connection_id": "xxx" }
func (h *EnableBankingHandler) RefreshBalances(c *gin.Context) {
	var req struct {
		ConnectionID string `json:"connection_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "connection_id is required"})
		return
	}

	// 1. Récupérer le session_id depuis la DB
	var sessionID string
	err := h.DB.QueryRow(`
		SELECT provider_connection_id 
		FROM banking_connections 
		WHERE id = $1
	`, req.ConnectionID).Scan(&sessionID)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Connection not found"})
		return
	}

	// 2. Récupérer les comptes de cette connexion
	rows, err := h.DB.Query(`
		SELECT id, external_account_id 
		FROM banking_accounts 
		WHERE connection_id = $1
	`, req.ConnectionID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch accounts"})
		return
	}
	defer rows.Close()

	updatedCount := 0

	// 3. Pour chaque compte, rafraîchir le solde
	for rows.Next() {
		var accountID, externalID string
		if err := rows.Scan(&accountID, &externalID); err != nil {
			continue
		}

		// Récupérer les soldes depuis Enable Banking
		balances, err := h.EnableBankingService.GetBalances(
			c.Request.Context(), 
			sessionID, 
			externalID,
		)

		if err != nil {
			fmt.Printf("Error fetching balance for account %s: %v\n", externalID, err)
			continue
		}

		// Mettre à jour le solde dans la DB (on prend le premier solde)
		if len(balances) > 0 {
			_, err := h.DB.Exec(`
				UPDATE banking_accounts 
				SET balance = $1, last_synced = NOW() 
				WHERE id = $2
			`, balances[0].BalanceAmount, accountID)

			if err == nil {
				updatedCount++
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Balances refreshed",
		"accounts_updated": updatedCount,
	})
}

// ========== 6. GET TRANSACTIONS ==========

// GET /api/v1/banking/enablebanking/transactions?account_id=xxx&date_from=2024-01-01&date_to=2024-12-31
func (h *EnableBankingHandler) GetTransactions(c *gin.Context) {
	accountID := c.Query("account_id")
	dateFrom := c.Query("date_from")
	dateTo := c.Query("date_to")

	if accountID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "account_id is required"})
		return
	}

	// 1. Récupérer le session_id et external_account_id
	var sessionID, externalAccountID string
	err := h.DB.QueryRow(`
		SELECT bc.provider_connection_id, ba.external_account_id
		FROM banking_accounts ba
		JOIN banking_connections bc ON ba.connection_id = bc.id
		WHERE ba.id = $1
	`, accountID).Scan(&sessionID, &externalAccountID)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Account not found"})
		return
	}

	// 2. Récupérer les transactions depuis Enable Banking
	transactions, err := h.EnableBankingService.GetTransactions(
		c.Request.Context(),
		sessionID,
		externalAccountID,
		dateFrom,
		dateTo,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to fetch transactions",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"transactions": transactions})
}

// ========== 7. DELETE CONNECTION ==========

// DELETE /api/v1/banking/enablebanking/connections/:id
func (h *EnableBankingHandler) DeleteConnection(c *gin.Context) {
	connectionID := c.Param("id")

	// 1. Récupérer le session_id
	var sessionID string
	err := h.DB.QueryRow(`
		SELECT provider_connection_id 
		FROM banking_connections 
		WHERE id = $1
	`, connectionID).Scan(&sessionID)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Connection not found"})
		return
	}

	// 2. Supprimer la session sur Enable Banking
	if err := h.EnableBankingService.DeleteSession(c.Request.Context(), sessionID); err != nil {
		// Log l'erreur mais on continue quand même
		fmt.Printf("Warning: Failed to delete Enable Banking session: %v\n", err)
	}

	// 3. Supprimer de notre DB
	_, err = h.DB.Exec(`DELETE FROM banking_connections WHERE id = $1`, connectionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete connection"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Connection deleted successfully"})
}
package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

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
			"id":      aspsp.Name,
			"name":    aspsp.Name,
			"country": aspsp.Country,
			"logo":    aspsp.Logo,
			"beta":    aspsp.Beta,
		}
		
		if aspsp.BIC != "" {
			bank["bic"] = aspsp.BIC
		}
		
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

	state := uuid.New().String()
	validUntil := time.Now().AddDate(0, 0, 90).Format(time.RFC3339)

	callbackURL := os.Getenv("FRONTEND_URL")
	if callbackURL == "" {
		callbackURL = "https://www.budgetfamille.com"
	}
	callbackURL += "/beta2/callback"

	authReq := services.AuthRequest{
		Access: services.Access{
			ValidUntil: validUntil,
		},
		ASPSP: services.ASPSPIdentifier{
			Name:    req.ASPSPID,
			Country: "FR",
		},
		State:       state,
		RedirectURL: callbackURL,
		PSUType:     "personal",
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

	sessionResp, err := h.EnableBankingService.CreateSession(c.Request.Context(), code, state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create session",
			"details": err.Error(),
		})
		return
	}

	// Formatter les comptes pour le frontend
	var accounts []map[string]interface{}
	for _, acc := range sessionResp.Accounts {
		iban := acc.AccountID.IBAN
		if iban == "" && acc.AccountID.Other != nil {
			iban = acc.AccountID.Other.Identification
		}
		
		accounts = append(accounts, map[string]interface{}{
			"uid":      acc.UID,
			"name":     acc.Name,
			"iban":     iban,
			"currency": acc.Currency,
			"type":     acc.CashAccountType,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"session_id": sessionResp.SessionID,
		"accounts":   accounts,
	})
}

// ========== 4. SYNC ACCOUNTS (Dans le Budget) ==========

// POST /api/v1/budgets/:id/banking/enablebanking/sync
// Body: { "session_id": "xxx", "accounts": [...] }
func (h *EnableBankingHandler) SyncAccounts(c *gin.Context) {
	userID := middleware.GetUserID(c)
	budgetID := c.Param("id")

	var req struct {
		SessionID string `json:"session_id"`
		Accounts  []struct {
			UID      string `json:"uid"`
			Name     string `json:"name"`
			IBAN     string `json:"iban"`
			Currency string `json:"currency"`
			Type     string `json:"type"`
		} `json:"accounts"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("❌ Sync Error - Invalid JSON: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request format",
			"details": err.Error(),
		})
		return
	}

	// Validation des champs requis
	if req.SessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id is required"})
		return
	}

	if len(req.Accounts) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"message": "No accounts to sync",
			"accounts_synced": 0,
		})
		return
	}

	log.Printf("🔄 Starting sync for %d accounts in budget %s", len(req.Accounts), budgetID)

	accountsSynced := 0

	for _, acc := range req.Accounts {
		log.Printf("💳 Processing account: %s (UID: %s)", acc.Name, acc.UID)
		
		// A. Créer/récupérer la connexion
		connID, err := h.Service.SaveConnectionWithTokens(
			c.Request.Context(),
			userID,
			budgetID,
			acc.UID,
			"Enable Banking",
			req.SessionID,
			"enablebanking-managed",
			"",
			time.Now().AddDate(0, 3, 0),
		)

		if err != nil {
			log.Printf("❌ Error creating connection for account %s: %v", acc.Name, err)
			continue
		}

		// B. Récupérer le solde
		balance := 0.0
		balances, err := h.EnableBankingService.GetBalances(
			c.Request.Context(),
			req.SessionID,
			acc.UID,
		)
		
		if err != nil {
			log.Printf("⚠️  Could not fetch balance for %s: %v", acc.Name, err)
		} else if len(balances) > 0 {
			amountStr := balances[0].BalanceAmount.Amount
			if parsed, err := strconv.ParseFloat(amountStr, 64); err == nil {
				balance = parsed
				log.Printf("💰 Balance for %s: %.2f %s", acc.Name, balance, balances[0].BalanceAmount.Currency)
			}
		}

		// C. Sauvegarder le compte
		mask := acc.IBAN
		if len(mask) > 4 {
			mask = mask[len(mask)-4:]
		}

		err = h.Service.SaveAccount(
			c.Request.Context(),
			connID,
			acc.UID,
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

	log.Printf("🎉 Sync complete: %d/%d accounts synced", accountsSynced, len(req.Accounts))

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

	for rows.Next() {
		var accountID, externalID string
		if err := rows.Scan(&accountID, &externalID); err != nil {
			continue
		}

		balances, err := h.EnableBankingService.GetBalances(
			c.Request.Context(), 
			sessionID, 
			externalID,
		)

		if err != nil {
			fmt.Printf("Error fetching balance for account %s: %v\n", externalID, err)
			continue
		}

		if len(balances) > 0 {
			amountStr := balances[0].BalanceAmount.Amount
			if balance, err := strconv.ParseFloat(amountStr, 64); err == nil {
				_, err := h.DB.Exec(`
					UPDATE banking_accounts 
					SET balance = $1, last_synced = NOW() 
					WHERE id = $2
				`, balance, accountID)

				if err == nil {
					updatedCount++
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Balances refreshed",
		"accounts_updated": updatedCount,
	})
}

// ========== 6. GET TRANSACTIONS (POUR LE MAPPING) ==========

// GET /api/v1/banking/enablebanking/transactions
func (h *EnableBankingHandler) GetTransactions(c *gin.Context) {
	userID := middleware.GetUserID(c)
	
	// Récupérer toutes les connexions Enable Banking de l'utilisateur
	rows, err := h.DB.Query(`
		SELECT bc.provider_connection_id, ba.external_account_id, ba.id
		FROM banking_accounts ba
		JOIN banking_connections bc ON ba.connection_id = bc.id
		WHERE bc.user_id = $1 AND bc.provider = 'enablebanking'
	`, userID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch accounts"})
		return
	}
	defer rows.Close()

	type TransactionDisplay struct {
		ID          string  `json:"id"`
		AccountID   string  `json:"account_id"`
		Amount      float64 `json:"amount"`
		Currency    string  `json:"currency_code"`
		Description string  `json:"clean_description"`
		Date        string  `json:"date"`
	}

	var allTransactions []TransactionDisplay
	transactionID := 1

	// Calculer la date d'il y a 90 jours
	dateFrom := time.Now().AddDate(0, 0, -90).Format("2006-01-02")
	dateTo := time.Now().Format("2006-01-02")

	for rows.Next() {
		var sessionID, accountUID, accountID string
		if err := rows.Scan(&sessionID, &accountUID, &accountID); err != nil {
			continue
		}

		// Récupérer les transactions pour ce compte
		transactions, err := h.EnableBankingService.GetTransactions(
			c.Request.Context(),
			sessionID,
			accountUID,
			dateFrom,
			dateTo,
		)

		if err != nil {
			log.Printf("⚠️  Error fetching transactions for account %s: %v", accountUID, err)
			continue
		}

		// Convertir au format attendu par le frontend
		for _, tx := range transactions {
			// Parser le montant
			amount := 0.0
			if parsed, err := strconv.ParseFloat(tx.TransactionAmount.Amount, 64); err == nil {
				amount = parsed
				// Si c'est un débit, rendre négatif
				if tx.CreditDebitIndicator == "DBIT" {
					amount = -amount
				}
			}

			description := tx.RemittanceInfo
			if description == "" && tx.CreditorName != "" {
				description = tx.CreditorName
			}
			if description == "" && tx.DebtorName != "" {
				description = tx.DebtorName
			}
			if description == "" {
				description = "Transaction"
			}

			allTransactions = append(allTransactions, TransactionDisplay{
				ID:          fmt.Sprintf("eb-%d", transactionID),
				AccountID:   accountID,
				Amount:      amount,
				Currency:    tx.TransactionAmount.Currency,
				Description: description,
				Date:        tx.BookingDate,
			})
			transactionID++
		}
	}

	c.JSON(http.StatusOK, gin.H{"transactions": allTransactions})
}

// ========== 7. DELETE CONNECTION ==========

// DELETE /api/v1/banking/enablebanking/connections/:id
func (h *EnableBankingHandler) DeleteConnection(c *gin.Context) {
	connectionID := c.Param("id")

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

	if err := h.EnableBankingService.DeleteSession(c.Request.Context(), sessionID); err != nil {
		fmt.Printf("Warning: Failed to delete Enable Banking session: %v\n", err)
	}

	_, err = h.DB.Exec(`DELETE FROM banking_connections WHERE id = $1`, connectionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete connection"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Connection deleted successfully"})
}
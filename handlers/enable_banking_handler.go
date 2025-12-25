package handlers

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
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
	DB                   *sql.DB
	Service              *services.BankingService
	EnableBankingService *services.EnableBankingService
}

func NewEnableBankingHandler(db *sql.DB) *EnableBankingHandler {
	return &EnableBankingHandler{
		DB:                   db,
		Service:              services.NewBankingService(db),
		EnableBankingService: services.NewEnableBankingService(),
	}
}

// ============================================================================
// 1. GET BANKS - Liste des banques disponibles
// ============================================================================

func (h *EnableBankingHandler) GetBanks(c *gin.Context) {
	country := c.DefaultQuery("country", "FR")
	
	log.Printf("🏦 Fetching banks for country: %s", country)
	
	aspsps, err := h.EnableBankingService.GetASPSPs(c.Request.Context(), country)
	if err != nil {
		log.Printf("❌ Failed to fetch banks: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to fetch banks",
			"details": err.Error(),
		})
		return
	}

	// Transformer en format UI-friendly
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
		
		// Identifier si c'est une banque sandbox
		if aspsp.Sandbox != nil {
			bank["sandbox"] = true
			bank["sandbox_users"] = aspsp.Sandbox.Users
		} else {
			bank["sandbox"] = false
		}
		
		banks = append(banks, bank)
	}

	log.Printf("✅ Returning %d banks", len(banks))
	c.JSON(http.StatusOK, gin.H{"banks": banks})
}

// ============================================================================
// 2. CREATE CONNECTION - Initier l'autorisation bancaire
// ============================================================================

func (h *EnableBankingHandler) CreateConnection(c *gin.Context) {
	var req struct {
		ASPSPID  string `json:"aspsp_id" binding:"required"`
		BudgetID string `json:"budget_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request",
			"details": "aspsp_id and budget_id are required",
		})
		return
	}

	log.Printf("🔐 Creating connection for bank: %s (budget: %s)", req.ASPSPID, req.BudgetID)

	// Générer un state unique qui encode le budget ID
	state := fmt.Sprintf("%s|%s", req.BudgetID, uuid.New().String())
	validUntil := time.Now().AddDate(0, 0, 90).Format(time.RFC3339)

	// Construire l'URL de callback
	callbackURL := os.Getenv("FRONTEND_URL")
	if callbackURL == "" {
		callbackURL = "https://www.budgetfamille.com"
	}
	callbackURL += "/beta2/callback"

	log.Printf("📍 Callback URL: %s", callbackURL)

	// Créer la demande d'autorisation
	authReq := services.AuthRequest{
		Access: services.Access{
			ValidUntil: validUntil,
		},
		ASPSP: services.ASPSPIdentifier{
			Name:    req.ASPSPID,
			Country: "FR", // Pour l'instant, on se concentre sur la France
		},
		State:       state,
		RedirectURL: callbackURL,
		PSUType:     "personal",
	}

	authResp, err := h.EnableBankingService.CreateAuthRequest(c.Request.Context(), authReq)
	if err != nil {
		log.Printf("❌ Failed to create auth request: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to create connection",
			"details": err.Error(),
		})
		return
	}

	log.Printf("✅ Authorization URL created successfully")
	log.Printf("   URL: %s", authResp.URL)
	log.Printf("   State: %s", state)

	c.JSON(http.StatusOK, gin.H{
		"redirect_url":     authResp.URL,
		"state":            state,
		"authorization_id": authResp.AuthorizationID,
	})
}

// ============================================================================
// 3. HANDLE CALLBACK - Gérer le retour après autorisation
// ============================================================================

func (h *EnableBankingHandler) HandleCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")

	log.Printf("📞 Callback received - Code: %s..., State: %s", code[:min(10, len(code))], state)

	if code == "" || state == "" {
		log.Println("❌ Missing code or state parameter")
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Missing code or state parameter",
		})
		return
	}

	// Créer la session avec le code d'autorisation
	sessionResp, err := h.EnableBankingService.CreateSession(c.Request.Context(), code, state)
	if err != nil {
		log.Printf("❌ Failed to create session: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to create session",
			"details": err.Error(),
		})
		return
	}

	// Extraire le budget ID du state
	budgetID := state
	if len(state) > 36 {
		// State format: "budgetID|uuid"
		budgetID = state[:36]
	}

	log.Printf("✅ Session created successfully")
	log.Printf("   Session ID: %s", sessionResp.SessionID)
	log.Printf("   Budget ID: %s", budgetID)
	log.Printf("   Accounts: %d", len(sessionResp.Accounts))

	// Transformer les comptes en format UI-friendly
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
		"session_id":    sessionResp.SessionID,
		"budget_id":     budgetID,
		"accounts":      accounts,
		"bank_name":     sessionResp.ASPSP.Name,
		"bank_country":  sessionResp.ASPSP.Country,
	})
}

// ============================================================================
// 4. SYNC ACCOUNTS - Synchroniser les comptes dans le budget
// ============================================================================

func (h *EnableBankingHandler) SyncAccounts(c *gin.Context) {
	userID := middleware.GetUserID(c)
	budgetID := c.Param("id")

	log.Printf("═══════════════════════════════════════════════════")
	log.Printf("🔄 SYNC START - Budget: %s, User: %s", budgetID, userID)
	log.Printf("═══════════════════════════════════════════════════")

	// Lire le body
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		log.Printf("❌ Failed to read body: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot read request body"})
		return
	}

	log.Printf("📦 Request body: %s", string(bodyBytes))

	// Restaurer le body pour le binding
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var req struct {
		SessionID string `json:"session_id" binding:"required"`
		BankName  string `json:"bank_name"`
		Accounts  []struct {
			UID      string `json:"uid" binding:"required"`
			Name     string `json:"name" binding:"required"`
			IBAN     string `json:"iban"`
			Currency string `json:"currency" binding:"required"`
			Type     string `json:"type"`
		} `json:"accounts" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("❌ JSON binding error: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request format",
			"details": err.Error(),
		})
		return
	}

	log.Printf("✅ Parsed request:")
	log.Printf("   Session ID: %s", req.SessionID)
	log.Printf("   Bank: %s", req.BankName)
	log.Printf("   Accounts: %d", len(req.Accounts))

	if len(req.Accounts) == 0 {
		log.Println("⚠️  No accounts to sync")
		c.JSON(http.StatusOK, gin.H{
			"message":         "No accounts to sync",
			"accounts_synced": 0,
		})
		return
	}

	accountsSynced := 0
	bankName := req.BankName
	if bankName == "" {
		bankName = "Enable Banking"
	}

	for i, acc := range req.Accounts {
		log.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		log.Printf("💳 [%d/%d] Processing: %s", i+1, len(req.Accounts), acc.Name)
		log.Printf("    UID: %s", acc.UID)
		log.Printf("    IBAN: %s", acc.IBAN)
		log.Printf("    Currency: %s", acc.Currency)
		log.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

		// A. Créer/récupérer la connexion
		log.Println("   → Creating/updating connection...")
		connID, err := h.Service.SaveConnectionWithTokens(
			c.Request.Context(),
			userID,
			budgetID,
			acc.UID,
			bankName,
			req.SessionID,
			"enablebanking",
			"",
			time.Now().AddDate(0, 3, 0), // 3 mois
		)

		if err != nil {
			log.Printf("❌ Failed to create connection: %v", err)
			continue
		}

		log.Printf("✅ Connection ID: %s", connID)

		// B. Récupérer le solde
		balance := 0.0
		log.Println("   → Fetching balance...")
		
		balances, err := h.EnableBankingService.GetBalances(
			c.Request.Context(),
			req.SessionID,
			acc.UID,
		)
		
		if err != nil {
			log.Printf("⚠️  Could not fetch balance: %v", err)
		} else if len(balances) > 0 {
			// Prendre le premier solde disponible
			amountStr := balances[0].BalanceAmount.Amount
			if parsed, err := strconv.ParseFloat(amountStr, 64); err == nil {
				balance = parsed
				log.Printf("💰 Balance: %.2f %s", balance, balances[0].BalanceAmount.Currency)
			} else {
				log.Printf("⚠️  Could not parse balance: '%s'", amountStr)
			}
		}

		// C. Sauvegarder le compte
		log.Println("   → Saving account...")
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
			log.Printf("❌ Failed to save account: %v", err)
			continue
		}

		log.Printf("✅ Account synced: %s (%.2f %s)", acc.Name, balance, acc.Currency)
		accountsSynced++
	}

	log.Printf("═══════════════════════════════════════════════════")
	log.Printf("🎉 SYNC COMPLETE: %d/%d accounts synced", accountsSynced, len(req.Accounts))
	log.Printf("═══════════════════════════════════════════════════")

	c.JSON(http.StatusOK, gin.H{
		"message":         "Accounts synchronized successfully",
		"accounts_synced": accountsSynced,
		"total_accounts":  len(req.Accounts),
	})
}

// ============================================================================
// 5. GET CONNECTIONS - Liste des connexions du budget
// ============================================================================

func (h *EnableBankingHandler) GetConnections(c *gin.Context) {
	budgetID := c.Param("id")
	userID := middleware.GetUserID(c)

	log.Printf("📋 Fetching Enable Banking connections for budget %s", budgetID)

	rows, err := h.DB.Query(`
		SELECT 
			bc.id,
			bc.aspsp_name as institution_name,
			bc.session_id,
			bc.created_at,
			COUNT(ba.id) as account_count
		FROM banking_connections bc
		LEFT JOIN banking_accounts ba ON ba.connection_id = bc.id
		WHERE bc.budget_id = $1 
		  AND bc.user_id = $2
		GROUP BY bc.id, bc.aspsp_name, bc.session_id, bc.created_at
		ORDER BY bc.created_at DESC
	`, budgetID, userID)

	if err != nil {
		log.Printf("❌ Error fetching connections: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch connections"})
		return
	}
	defer rows.Close()

	type Connection struct {
		ID              string    `json:"id"`
		InstitutionName string    `json:"institution_name"`
		SessionID       string    `json:"session_id"`
		CreatedAt       time.Time `json:"created_at"`
		AccountCount    int       `json:"account_count"`
		Provider        string    `json:"provider"`
	}

	var connections []Connection

	for rows.Next() {
		var conn Connection
		conn.Provider = "enablebanking"
		if err := rows.Scan(&conn.ID, &conn.InstitutionName, &conn.SessionID, &conn.CreatedAt, &conn.AccountCount); err != nil {
			log.Printf("⚠️  Error scanning row: %v", err)
			continue
		}
		connections = append(connections, conn)
	}

	log.Printf("✅ Found %d Enable Banking connections", len(connections))

	c.JSON(http.StatusOK, gin.H{"connections": connections})
}

// ============================================================================
// 6. REFRESH BALANCES - Rafraîchir les soldes
// ============================================================================

func (h *EnableBankingHandler) RefreshBalances(c *gin.Context) {
	var req struct {
		ConnectionID string `json:"connection_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "connection_id is required"})
		return
	}

	log.Printf("🔄 Refreshing balances for connection: %s", req.ConnectionID)

	// Récupérer le session ID
	var sessionID string
	err := h.DB.QueryRow(`
		SELECT provider_connection_id 
		FROM banking_connections 
		WHERE id = $1
	`, req.ConnectionID).Scan(&sessionID)

	if err != nil {
		log.Printf("❌ Connection not found: %v", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "Connection not found"})
		return
	}

	// Récupérer tous les comptes de cette connexion
	rows, err := h.DB.Query(`
		SELECT id, external_account_id, name
		FROM banking_accounts 
		WHERE connection_id = $1
	`, req.ConnectionID)

	if err != nil {
		log.Printf("❌ Failed to fetch accounts: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch accounts"})
		return
	}
	defer rows.Close()

	updatedCount := 0
	errors := []string{}

	for rows.Next() {
		var accountID, externalID, accountName string
		if err := rows.Scan(&accountID, &externalID, &accountName); err != nil {
			continue
		}

		log.Printf("💰 Refreshing balance for: %s (UID: %s)", accountName, externalID)

		balances, err := h.EnableBankingService.GetBalances(
			c.Request.Context(),
			sessionID,
			externalID,
		)

		if err != nil {
			errMsg := fmt.Sprintf("Error fetching balance for %s: %v", accountName, err)
			log.Printf("❌ %s", errMsg)
			errors = append(errors, errMsg)
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
					log.Printf("✅ Updated balance for %s: %.2f %s", accountName, balance, balances[0].BalanceAmount.Currency)
					updatedCount++
				} else {
					errMsg := fmt.Sprintf("Failed to update balance for %s: %v", accountName, err)
					log.Printf("❌ %s", errMsg)
					errors = append(errors, errMsg)
				}
			}
		}
	}

	response := gin.H{
		"message":          "Balances refresh completed",
		"accounts_updated": updatedCount,
	}

	if len(errors) > 0 {
		response["errors"] = errors
	}

	log.Printf("✅ Balance refresh complete: %d accounts updated", updatedCount)

	c.JSON(http.StatusOK, response)
}

// ============================================================================
// 7. GET TRANSACTIONS - Récupérer les transactions
// ============================================================================

func (h *EnableBankingHandler) GetTransactions(c *gin.Context) {
	budgetID := c.Query("budget_id")
	userID := middleware.GetUserID(c)

	log.Printf("💳 Fetching transactions for budget: %s", budgetID)

	// Récupérer tous les comptes Enable Banking de l'utilisateur
	rows, err := h.DB.Query(`
		SELECT bc.provider_connection_id, ba.external_account_id, ba.id, ba.name
		FROM banking_accounts ba
		JOIN banking_connections bc ON ba.connection_id = bc.id
		WHERE bc.user_id = $1 
		  AND bc.budget_id = $2 
		  AND bc.provider = 'enablebanking'
	`, userID, budgetID)

	if err != nil {
		log.Printf("❌ Failed to fetch accounts: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch accounts"})
		return
	}
	defer rows.Close()

	type TransactionDisplay struct {
		ID          string  `json:"id"`
		AccountID   string  `json:"account_id"`
		AccountName string  `json:"account_name"`
		Amount      float64 `json:"amount"`
		Currency    string  `json:"currency_code"`
		Description string  `json:"clean_description"`
		Date        string  `json:"date"`
		Type        string  `json:"type"` // DBIT ou CRDT
	}

	var allTransactions []TransactionDisplay
	transactionID := 1

	// Récupérer les transactions des 90 derniers jours
	dateFrom := time.Now().AddDate(0, 0, -90).Format("2006-01-02")
	dateTo := time.Now().Format("2006-01-02")

	for rows.Next() {
		var sessionID, accountUID, accountID, accountName string
		if err := rows.Scan(&sessionID, &accountUID, &accountID, &accountName); err != nil {
			continue
		}

		log.Printf("   → Fetching transactions for: %s", accountName)

		transactions, err := h.EnableBankingService.GetTransactions(
			c.Request.Context(),
			accountUID,
			dateFrom,
			dateTo,
		)

		if err != nil {
			log.Printf("⚠️  Error fetching transactions for %s: %v", accountName, err)
			continue
		}

		log.Printf("   ✅ Found %d transactions for %s", len(transactions), accountName)

		for _, tx := range transactions {
			// Convertir le montant
			amount := 0.0
			if parsed, err := strconv.ParseFloat(tx.TransactionAmount.Amount, 64); err == nil {
				amount = parsed
				// Si c'est un débit, rendre le montant négatif
				if tx.CreditDebitIndicator == "DBIT" {
					amount = -amount
				}
			}

			// Construire la description
			description := ""
			if len(tx.RemittanceInformation) > 0 {
				description = tx.RemittanceInformation[0]
			}
			if description == "" && tx.Creditor != nil {
				description = tx.Creditor.Name
			}
			if description == "" && tx.Debtor != nil {
				description = tx.Debtor.Name
			}
			if description == "" {
				description = "Transaction"
			}

			// Utiliser la date de réservation ou la date de valeur
			date := tx.BookingDate
			if date == "" {
				date = tx.ValueDate
			}
			if date == "" {
				date = tx.TransactionDate
			}

			allTransactions = append(allTransactions, TransactionDisplay{
				ID:          fmt.Sprintf("eb-%d", transactionID),
				AccountID:   accountID,
				AccountName: accountName,
				Amount:      amount,
				Currency:    tx.TransactionAmount.Currency,
				Description: description,
				Date:        date,
				Type:        tx.CreditDebitIndicator,
			})
			transactionID++
		}
	}

	log.Printf("✅ Total transactions retrieved: %d", len(allTransactions))

	c.JSON(http.StatusOK, gin.H{
		"transactions": allTransactions,
		"total":        len(allTransactions),
	})
}

// ============================================================================
// 8. DELETE CONNECTION - Supprimer une connexion
// ============================================================================

func (h *EnableBankingHandler) DeleteConnection(c *gin.Context) {
	connectionID := c.Param("id")
	userID := middleware.GetUserID(c)

	log.Printf("🗑️  Deleting connection: %s (user: %s)", connectionID, userID)

	// Récupérer le session ID avant de supprimer
	var sessionID string
	err := h.DB.QueryRow(`
		SELECT provider_connection_id 
		FROM banking_connections 
		WHERE id = $1 AND user_id = $2 AND provider = 'enablebanking'
	`, connectionID, userID).Scan(&sessionID)

	if err != nil {
		if err == sql.ErrNoRows {
			log.Println("❌ Connection not found or unauthorized")
			c.JSON(http.StatusNotFound, gin.H{"error": "Connection not found"})
		} else {
			log.Printf("❌ Database error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		}
		return
	}

	// Supprimer la session Enable Banking
	if sessionID != "" {
		if err := h.EnableBankingService.DeleteSession(c.Request.Context(), sessionID); err != nil {
			log.Printf("⚠️  Failed to delete Enable Banking session: %v", err)
			// Continue quand même avec la suppression locale
		}
	}

	// Supprimer les comptes associés
	_, err = h.DB.Exec(`
		DELETE FROM banking_accounts 
		WHERE connection_id = $1
	`, connectionID)

	if err != nil {
		log.Printf("❌ Failed to delete accounts: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete accounts"})
		return
	}

	// Supprimer la connexion
	_, err = h.DB.Exec(`
		DELETE FROM banking_connections 
		WHERE id = $1 AND user_id = $2
	`, connectionID, userID)

	if err != nil {
		log.Printf("❌ Failed to delete connection: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete connection"})
		return
	}

	log.Printf("✅ Connection deleted successfully")
	c.JSON(http.StatusOK, gin.H{"message": "Connection deleted successfully"})
}

// ============================================================================
// UTILITY FUNCTIONS
// ============================================================================

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
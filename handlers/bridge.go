package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"budget-api/middleware"
	"budget-api/services"

	"github.com/gin-gonic/gin"
)

type BridgeHandler struct {
	DB            *sql.DB
	Service       *services.BankingService
	BridgeService *services.BridgeService
}

func NewBridgeHandler(db *sql.DB) *BridgeHandler {
	return &BridgeHandler{
		DB:            db,
		Service:       services.NewBankingService(db),
		BridgeService: services.NewBridgeService(),
	}
}

// 1. Lister les banques disponibles
func (h *BridgeHandler) GetBanks(c *gin.Context) {
	banks, err := h.BridgeService.GetBanks(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch banks", "details": err.Error()})
		return
	}

	var displayBanks []map[string]interface{}
	for _, b := range banks {
		displayBanks = append(displayBanks, map[string]interface{}{
			"id":   b.ID,
			"name": b.Name,
			"logo": b.Images.Logo,
		})
	}

	c.JSON(http.StatusOK, gin.H{"banks": displayBanks})
}

// 2. Créer une Connect Session
func (h *BridgeHandler) CreateConnection(c *gin.Context) {
	userID := middleware.GetUserID(c)

	var userEmail string
	err := h.DB.QueryRow("SELECT email FROM users WHERE id = $1", userID).Scan(&userEmail)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get user email", "details": err.Error()})
		return
	}

	// TENTATIVE SIMPLE : On n'envoie PAS redirect_url dans le body pour l'instant
	// car c'est ce qui fait planter votre appel Bridge (Erreur 500 Invalid Body).
	// On laisse Bridge utiliser l'URL par défaut configurée dans le Dashboard.
	connectURL, err := h.BridgeService.CreateConnectItem(c.Request.Context(), userEmail)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create Bridge session", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"redirect_url": connectURL,
	})
}

// 3. Synchroniser les items et comptes DANS LE BUDGET ACTUEL
func (h *BridgeHandler) SyncAccounts(c *gin.Context) {
	userID := middleware.GetUserID(c)
	budgetID := c.Param("id")

	if budgetID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Budget ID required"})
		return
	}

	var userEmail string
	err := h.DB.QueryRow("SELECT email FROM users WHERE id = $1", userID).Scan(&userEmail)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get user email"})
		return
	}

	// 1. Récupérer TOUS les comptes depuis Bridge
	accounts, err := h.BridgeService.GetAccounts(c.Request.Context(), userEmail)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch accounts from Bridge", "details": err.Error()})
		return
	}

	if len(accounts) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "No accounts found.", "accounts_synced": 0})
		return
	}

	// 2. Récupérer les Items (pour avoir le nom des banques)
	items, _ := h.BridgeService.GetItems(c.Request.Context(), userEmail)
	itemMap := make(map[int64]services.BridgeItem)
	for _, item := range items {
		itemMap[item.ID] = item
	}

	accountsSynced := 0

	// 3. Boucle simplifiée : On sauvegarde (Upsert) la connexion puis le compte
	for _, acc := range accounts {
		// Nom de la banque
		institutionName := "Bridge Connection"
		providerIDStr := "0"
		
		if item, exists := itemMap[acc.ItemID]; exists {
			institutionName = fmt.Sprintf("Bank ID %d", item.ProviderID)
			providerIDStr = strconv.Itoa(item.ProviderID)
		}

		// A. Sauvegarder/Récupérer la Connexion (UPSERT géré par le service)
		// On utilise ItemID (string) comme identifiant unique de la connexion Bridge
		itemIDStr := strconv.FormatInt(acc.ItemID, 10)
		
		connID, err := h.Service.SaveConnectionWithTokens(
			c.Request.Context(),
			userID,
			budgetID,
			providerIDStr,   
			institutionName, 
			itemIDStr,       // Provider Connection ID (Unique key pour l'Upsert)
			"bridge-v3-managed",
			"",
			time.Now().AddDate(1, 0, 0),
		)

		if err != nil {
			fmt.Printf("Error ensuring connection for account %s: %v\n", acc.Name, err)
			continue
		}

		// B. Sauvegarder le Compte (UPSERT géré par le service)
		mask := acc.IBAN
		if len(mask) > 4 {
			mask = mask[len(mask)-4:]
		}

		err = h.Service.SaveAccount(
			c.Request.Context(),
			connID,
			strconv.FormatInt(acc.ID, 10), // External Account ID
			acc.Name,
			mask,
			acc.Currency,
			acc.Balance,
		)

		if err == nil {
			accountsSynced++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Accounts synchronized successfully",
		"accounts_synced": accountsSynced,
	})
}

// 4. Refresh Balances (Global ou Budget scoped)
func (h *BridgeHandler) RefreshBalances(c *gin.Context) {
	userID := middleware.GetUserID(c)
	var userEmail string
	h.DB.QueryRow("SELECT email FROM users WHERE id = $1", userID).Scan(&userEmail)
	
	accounts, err := h.BridgeService.GetAccounts(c.Request.Context(), userEmail)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch accounts"})
		return
	}

	updatedCount := 0
	for _, acc := range accounts {
		accountID := strconv.FormatInt(acc.ID, 10)
		result, err := h.DB.Exec(
			`UPDATE bank_accounts SET balance = $1, last_synced_at = NOW() WHERE external_account_id = $2`,
			acc.Balance, accountID,
		)
		if err == nil {
			rows, _ := result.RowsAffected()
			if rows > 0 { updatedCount++ }
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Balances refreshed", "updated_count": updatedCount})
}

// 5. Get Transactions
func (h *BridgeHandler) GetTransactions(c *gin.Context) {
	userID := middleware.GetUserID(c)
	var userEmail string
	h.DB.QueryRow("SELECT email FROM users WHERE id = $1", userID).Scan(&userEmail)
	
	transactions, err := h.BridgeService.GetTransactions(c.Request.Context(), userEmail, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch transactions"})
		return
	}
	
	// Transformer les ID en string pour le JSON
	type DisplayTransaction struct {
		ID          string  `json:"id"`
		AccountID   string  `json:"account_id"`
		Amount      float64 `json:"amount"`
		Currency    string  `json:"currency_code"`
		Description string  `json:"clean_description"`
		Date        string  `json:"date"`
	}

	var displayTransactions []DisplayTransaction
	for _, t := range transactions {
		uniqueID := fmt.Sprintf("%d-%d", t.ID, t.AccountID)
		if t.ID == 0 {
			uniqueID = strconv.FormatInt(time.Now().UnixNano(), 10)
		}

		displayTransactions = append(displayTransactions, DisplayTransaction{
			ID:          uniqueID,
			AccountID:   strconv.FormatInt(t.AccountID, 10),
			Amount:      t.Amount,
			Currency:    t.Currency,
			Description: t.Description,
			Date:        t.Date,
		})
	}

	c.JSON(http.StatusOK, gin.H{"transactions": displayTransactions})
}
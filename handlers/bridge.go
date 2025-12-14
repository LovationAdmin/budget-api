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

// 1. List Available Banks
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

type CreateConnectionRequest struct {
	RedirectURL string `json:"redirect_url"`
}

// 2. Create Connect Session
func (h *BridgeHandler) CreateConnection(c *gin.Context) {
	userID := middleware.GetUserID(c)

	var userEmail string
	err := h.DB.QueryRow("SELECT email FROM users WHERE id = $1", userID).Scan(&userEmail)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get user email", "details": err.Error()})
		return
	}

	// Read Redirect URL from frontend
	var req CreateConnectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fmt.Println("Warning: No JSON body provided or invalid format")
	}

	redirectURL := req.RedirectURL
	if redirectURL == "" {
		fmt.Println("Warning: redirect_url is empty")
	}

	// Call Service with 3 args
	connectURL, err := h.BridgeService.CreateConnectItem(c.Request.Context(), userEmail, redirectURL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create Bridge session", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"redirect_url": connectURL,
	})
}

// 3. Sync Accounts
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

	// 1. Fetch Accounts from Bridge
	accounts, err := h.BridgeService.GetAccounts(c.Request.Context(), userEmail)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch accounts from Bridge", "details": err.Error()})
		return
	}

	if len(accounts) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "No accounts found.", "accounts_synced": 0})
		return
	}

	// 2. Fetch Items for Bank Names
	items, _ := h.BridgeService.GetItems(c.Request.Context(), userEmail)
	itemMap := make(map[int64]services.BridgeItem)
	for _, item := range items {
		itemMap[item.ID] = item
	}

	accountsSynced := 0

	// 3. Loop & Upsert
	for _, acc := range accounts {
		institutionName := "Bridge Connection"
		providerIDStr := "0"
		
		if item, exists := itemMap[acc.ItemID]; exists {
			institutionName = fmt.Sprintf("Bank ID %d", item.ProviderID)
			providerIDStr = strconv.Itoa(item.ProviderID)
		}

		// Use ItemID as the unique identifier for the connection
		itemIDStr := strconv.FormatInt(acc.ItemID, 10)
		
		connID, err := h.Service.SaveConnectionWithTokens(
			c.Request.Context(),
			userID,
			budgetID,
			providerIDStr,   // Institution ID (e.g., 5)
			institutionName, // Name
			itemIDStr,       // Provider Connection ID (Unique Key: ItemID)
			"bridge-v3-managed",
			"",
			time.Now().AddDate(1, 0, 0),
		)

		if err != nil {
			fmt.Printf("Error ensuring connection for account %s: %v\n", acc.Name, err)
			continue
		}

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
		} else {
			fmt.Printf("Error saving account %s: %v\n", acc.Name, err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Accounts synchronized successfully",
		"accounts_synced": accountsSynced,
	})
}

// 4. Refresh Balances
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
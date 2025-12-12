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
	token, err := h.BridgeService.GetAccessToken(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to authenticate with Bridge", "details": err.Error()})
		return
	}

	banks, err := h.BridgeService.GetBanks(c.Request.Context(), token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch banks", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"banks": banks})
}

// 2. Créer une Connect Session (renvoie l'URL Bridge)
func (h *BridgeHandler) CreateConnection(c *gin.Context) {
	userID := middleware.GetUserID(c)

	token, err := h.BridgeService.GetAccessToken(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to authenticate with Bridge", "details": err.Error()})
		return
	}

	// Récupérer l'email de l'utilisateur
	var userEmail string
	err = h.DB.QueryRow("SELECT email FROM users WHERE id = $1", userID).Scan(&userEmail)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get user email", "details": err.Error()})
		return
	}

	// Créer une Connect Session (API v3)
	connectURL, err := h.BridgeService.CreateConnectItem(c.Request.Context(), token, userEmail)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create Bridge session", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"redirect_url": connectURL,
	})
}

// 3. Synchroniser les items et comptes après connexion Bridge
func (h *BridgeHandler) SyncAccounts(c *gin.Context) {
	userID := middleware.GetUserID(c)

	token, err := h.BridgeService.GetAccessToken(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to authenticate with Bridge"})
		return
	}

	// Récupérer tous les comptes depuis Bridge
	accounts, err := h.BridgeService.GetAccounts(c.Request.Context(), token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch accounts from Bridge"})
		return
	}

	if len(accounts) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"message": "No accounts found. Make sure you completed the Bridge connection.",
			"accounts_synced": 0,
		})
		return
	}

	// Récupérer les items pour avoir les noms des banques
	items, err := h.BridgeService.GetItems(c.Request.Context(), token)
	if err != nil {
		// Log l'erreur mais continue sans items (les noms de banque seront génériques)
		fmt.Printf("Warning: Failed to fetch items: %v\n", err)
		items = []services.BridgeItem{} 
	}

	// Créer un map des items par ID pour lookup rapide
	itemMap := make(map[int64]services.BridgeItem)
	for _, item := range items {
		itemMap[item.ID] = item
	}

	accountsSynced := 0

	// Pour chaque compte, créer ou mettre à jour en DB
	for _, acc := range accounts {
		// Trouver le nom de la banque via l'item
		institutionName := "Bridge Connection"
		
		// FIXED: Use ProviderID instead of BankID
		if item, exists := itemMap[acc.ItemID]; exists {
			institutionName = fmt.Sprintf("Bank ID %d", item.ProviderID)
		}

		// Vérifier si la connexion existe déjà
		var existingConnID string
		err := h.DB.QueryRow(
			`SELECT id FROM bank_connections 
			 WHERE user_id = $1 AND institution_id = $2`,
			userID,
			strconv.FormatInt(acc.ItemID, 10),
		).Scan(&existingConnID)

		var connID string
		if err == sql.ErrNoRows {
			// Créer une nouvelle connexion
			expiresAt := time.Now().AddDate(1, 0, 0)
			
			// FIXED: Ensure ProviderID is converted to string correctly
			providerIDStr := "0"
			if item, exists := itemMap[acc.ItemID]; exists {
				providerIDStr = strconv.Itoa(item.ProviderID)
			}

			connID, err = h.Service.SaveConnectionWithTokens(
				c.Request.Context(),
				userID,
				strconv.FormatInt(acc.ItemID, 10), // Institution ID (Item ID for unique ref)
				institutionName,
				providerIDStr,                     // Provider ID (The bank's global ID)
				token,                             // Access Token
				"",                                // Refresh Token (handled internally by Bridge)
				expiresAt,
			)
			if err != nil {
				fmt.Printf("Error saving connection: %v\n", err)
				continue
			}
		} else {
			connID = existingConnID
		}

		// Extraire les 4 derniers chiffres de l'IBAN
		mask := acc.IBAN
		if len(mask) > 4 {
			mask = mask[len(mask)-4:]
		}

		// Sauvegarder ou mettre à jour le compte
		err = h.Service.SaveAccount(
			c.Request.Context(),
			connID,
			strconv.FormatInt(acc.ID, 10),
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
		"total_accounts": len(accounts),
	})
}

// 4. Rafraîchir les soldes
func (h *BridgeHandler) RefreshBalances(c *gin.Context) {
	userID := middleware.GetUserID(c)

	token, err := h.BridgeService.GetAccessToken(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to authenticate with Bridge"})
		return
	}

	// Récupérer tous les comptes depuis Bridge
	accounts, err := h.BridgeService.GetAccounts(c.Request.Context(), token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch accounts from Bridge"})
		return
	}

	// Mettre à jour les soldes en DB
	updatedCount := 0
	for _, acc := range accounts {
		accountID := strconv.FormatInt(acc.ID, 10)

		// Mise à jour du solde
		result, err := h.DB.Exec(
			`UPDATE bank_accounts 
			 SET balance = $1, updated_at = NOW() 
			 WHERE external_account_id = $2 
			 AND connection_id IN (
				 SELECT id FROM bank_connections WHERE user_id = $3
			 )`,
			acc.Balance,
			accountID,
			userID,
		)

		if err == nil {
			rows, _ := result.RowsAffected()
			if rows > 0 {
				updatedCount++
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Balances refreshed",
		"updated_count": updatedCount,
	})
}
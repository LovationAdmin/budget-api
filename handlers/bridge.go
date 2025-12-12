package handlers

import (
	"database/sql"
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to authenticate with Bridge"})
		return
	}

	banks, err := h.BridgeService.GetBanks(c.Request.Context(), token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch banks"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"banks": banks})
}

// 2. Créer une connexion bancaire (renvoie l'URL de redirection)
func (h *BridgeHandler) CreateConnection(c *gin.Context) {
	userID := middleware.GetUserID(c)

	token, err := h.BridgeService.GetAccessToken(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to authenticate with Bridge"})
		return
	}

	// Récupérer l'email de l'utilisateur pour préfill
	var userEmail string
	err = h.DB.QueryRow("SELECT email FROM users WHERE id = $1", userID).Scan(&userEmail)
	if err != nil {
		userEmail = "" // Pas grave si on ne trouve pas l'email
	}

	redirectURL, err := h.BridgeService.CreateConnectItem(c.Request.Context(), token, userEmail)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create connection"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"redirect_url": redirectURL,
	})
}

// 3. Callback après connexion bancaire
func (h *BridgeHandler) HandleCallback(c *gin.Context) {
	userID := middleware.GetUserID(c)
	itemID := c.Query("item_id")

	if itemID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing item_id parameter"})
		return
	}

	token, err := h.BridgeService.GetAccessToken(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to authenticate with Bridge"})
		return
	}

	// Récupérer les comptes de Bridge
	accounts, err := h.BridgeService.GetAccounts(c.Request.Context(), token, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch accounts from Bridge"})
		return
	}

	// Créer une connexion en DB
	expiresAt := time.Now().AddDate(1, 0, 0)
	
	connID, err := h.Service.SaveConnectionWithTokens(
		c.Request.Context(),
		userID,
		itemID,
		"Bridge Connection",
		itemID,
		token, // On stocke le token Bridge
		"",
		expiresAt,
	)
	
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save connection"})
		return
	}

	// Sauvegarder chaque compte
	for _, acc := range accounts {
		iban := acc.IBAN
		if len(iban) > 4 {
			iban = iban[len(iban)-4:] // Garder les 4 derniers caractères
		}
		
		err = h.Service.SaveAccount(
			c.Request.Context(),
			connID,
			strconv.FormatInt(acc.ID, 10),
			acc.Name,
			iban,
			acc.Currency,
			acc.Balance,
		)
		
		if err != nil {
			// Log l'erreur mais continue avec les autres comptes
			continue
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Bank connected successfully",
		"accounts_count": len(accounts),
	})
}

// 4. Rafraîchir les soldes de tous les comptes
func (h *BridgeHandler) RefreshBalances(c *gin.Context) {
	userID := middleware.GetUserID(c)

	token, err := h.BridgeService.GetAccessToken(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to authenticate with Bridge"})
		return
	}

	// Récupérer tous les comptes de l'utilisateur depuis Bridge
	accounts, err := h.BridgeService.GetAccounts(c.Request.Context(), token, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch accounts from Bridge"})
		return
	}

	// Mettre à jour les soldes en DB
	updatedCount := 0
	for _, acc := range accounts {
		accountID := strconv.FormatInt(acc.ID, 10)
		
		// Mise à jour du solde dans la table bank_accounts
		_, err := h.DB.Exec(
			`UPDATE bank_accounts 
			 SET balance = $1, updated_at = NOW() 
			 WHERE external_id = $2 
			 AND connection_id IN (
				 SELECT id FROM bank_connections WHERE user_id = $3
			 )`,
			acc.Balance,
			accountID,
			userID,
		)
		
		if err == nil {
			updatedCount++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Balances refreshed",
		"updated_count": updatedCount,
	})
}
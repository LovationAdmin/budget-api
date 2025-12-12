package handlers

import (
	"database/sql"
	"net/http"
	"time"

	"budget-api/middleware"
	"budget-api/models"
	"budget-api/services"

	"github.com/gin-gonic/gin"
)

type BankingHandler struct {
	Service      *services.BankingService
	PlaidService *services.PlaidService
}

func NewBankingHandler(db *sql.DB) *BankingHandler {
	return &BankingHandler{
		Service:      services.NewBankingService(db),
		PlaidService: services.NewPlaidService(),
	}
}

// GetConnections lists all connections and accounts for the user
func (h *BankingHandler) GetConnections(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	connections, err := h.Service.GetUserConnections(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch connections"})
		return
	}

	totalReal, err := h.Service.GetRealityCheckSum(c.Request.Context(), userID)
	if err != nil {
		totalReal = 0
	}

	c.JSON(http.StatusOK, gin.H{
		"connections":     connections,
		"total_real_cash": totalReal,
	})
}

// UpdateAccountPool toggles the "Is Savings Pool" flag
func (h *BankingHandler) UpdateAccountPool(c *gin.Context) {
	userID := middleware.GetUserID(c)
	accountID := c.Param("id")

	var req models.UpdateAccountPoolRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err := h.Service.UpdateAccountPool(c.Request.Context(), accountID, userID, req.IsSavingsPool)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update account"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Account updated successfully"})
}

// DeleteConnection removes a bank connection
func (h *BankingHandler) DeleteConnection(c *gin.Context) {
	userID := middleware.GetUserID(c)
	connID := c.Param("id")

	err := h.Service.DeleteConnection(c.Request.Context(), connID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete connection"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Connection deleted successfully"})
}

// 1. Create Link Token (Frontend calls this to open Plaid Widget)
func (h *BankingHandler) CreateLinkToken(c *gin.Context) {
	userID := middleware.GetUserID(c)
	
	linkToken, err := h.PlaidService.CreateLinkToken(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create link token: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"link_token": linkToken})
}

// 2. Exchange Token (Called after user logs in on Frontend)
func (h *BankingHandler) ExchangeToken(c *gin.Context) {
	userID := middleware.GetUserID(c)
	
	var req struct {
		PublicToken     string `json:"public_token" binding:"required"`
		InstitutionId   string `json:"institution_id"`
		InstitutionName string `json:"institution_name"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// A. Exchange for permanent access token
	accessToken, itemID, err := h.PlaidService.ExchangePublicToken(c.Request.Context(), req.PublicToken)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to exchange token: " + err.Error()})
		return
	}

	// B. Save Connection Encrypted
	// Plaid tokens generally don't expire quickly, setting a default 2 year valid period
	expiresAt := time.Now().AddDate(2, 0, 0)
	
	connID, err := h.Service.SaveConnectionWithTokens(
		c.Request.Context(), 
		userID, 
		req.InstitutionId, 
		req.InstitutionName, 
		itemID, 
		accessToken, 
		"", // No refresh token flow for Plaid usually
		expiresAt,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save connection"})
		return
	}

	// C. Fetch & Save Initial Accounts
	plaidAccounts, err := h.PlaidService.GetBalances(c.Request.Context(), accessToken)
	if err == nil {
		for _, acc := range plaidAccounts {
			h.Service.SaveAccount(
				c.Request.Context(),
				connID,
				acc.AccountId,
				acc.Name,
				*acc.Mask.Get(),
				*acc.Balances.IsoCurrencyCode.Get(),
				*acc.Balances.Current.Get(),
			)
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Bank connected successfully"})
}
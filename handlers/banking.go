package handlers

import (
	"database/sql"
	"net/http"

	"budget-api/middleware"
	"budget-api/models"
	"budget-api/services"

	"github.com/gin-gonic/gin"
)

type BankingHandler struct {
	Service *services.BankingService
}

func NewBankingHandler(db *sql.DB) *BankingHandler {
	return &BankingHandler{
		Service: services.NewBankingService(db),
	}
}

// GetConnections liste toutes les connexions bancaires de l'utilisateur
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

// UpdateAccountPool met à jour le flag "is_savings_pool" d'un compte
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

// DeleteConnection supprime une connexion bancaire
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
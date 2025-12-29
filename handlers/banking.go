package handlers

import (
	"database/sql"
	"net/http"

	"github.com/LovationAdmin/budget-api/models"
	"github.com/LovationAdmin/budget-api/services"

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

// GetConnections liste les connexions DU BUDGET
func (h *BankingHandler) GetConnections(c *gin.Context) {
	budgetID := c.Param("id") // Récupéré depuis l'URL /budgets/:id/banking/connections
	if budgetID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Budget ID required"})
		return
	}

	connections, err := h.Service.GetBudgetConnections(c.Request.Context(), budgetID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch connections"})
		return
	}

	totalReal, err := h.Service.GetRealityCheckSum(c.Request.Context(), budgetID)
	if err != nil {
		// Si aucune ligne n'est trouvée (pas de compte épargne coché), le total est 0
		totalReal = 0
	}

	c.JSON(http.StatusOK, gin.H{
		"connections":     connections,
		"total_real_cash": totalReal,
	})
}

// UpdateAccountPool met à jour le flag "is_savings_pool"
func (h *BankingHandler) UpdateAccountPool(c *gin.Context) {
	accountID := c.Param("account_id")

	var req models.UpdateAccountPoolRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err := h.Service.UpdateAccountPool(c.Request.Context(), accountID, req.IsSavingsPool)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update account"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Account updated successfully"})
}

// DeleteConnection supprime une connexion bancaire
func (h *BankingHandler) DeleteConnection(c *gin.Context) {
	connID := c.Param("connection_id")

	err := h.Service.DeleteConnection(c.Request.Context(), connID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete connection"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Connection deleted successfully"})
}
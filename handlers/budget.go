// handlers/budget.go
// ✅ VERSION CORRIGÉE - Support location/currency

package handlers

import (
	"log"
	"net/http"

	"github.com/LovationAdmin/budget-api/services"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	budgetService *services.BudgetService
	emailService  *services.EmailService
}

func NewHandler(budgetService *services.BudgetService, emailService *services.EmailService) *Handler {
	return &Handler{
		budgetService: budgetService,
		emailService:  emailService,
	}
}

// GetBudgets returns all budgets for the authenticated user
func (h *Handler) GetBudgets(c *gin.Context) {
	userID := c.GetString("user_id")

	budgets, err := h.budgetService.GetUserBudgets(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get budgets"})
		return
	}

	c.JSON(http.StatusOK, budgets)
}

// CreateBudget creates a new budget
// ✅ CORRIGÉ : Support location et currency
func (h *Handler) CreateBudget(c *gin.Context) {
	var req struct {
		Name     string `json:"name" binding:"required"`
		Year     int    `json:"year"`
		Location string `json:"location"` // ✅ NOUVEAU
		Currency string `json:"currency"` // ✅ NOUVEAU
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Valeurs par défaut si non fournies
	if req.Location == "" {
		req.Location = "FR"
	}
	if req.Currency == "" {
		req.Currency = "EUR"
	}

	userID := c.GetString("user_id")

	// ✅ Passer location et currency au service
	budget, err := h.budgetService.CreateWithLocation(c.Request.Context(), req.Name, userID, req.Location, req.Currency)
	if err != nil {
		log.Printf("Error creating budget: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create budget"})
		return
	}

	c.JSON(http.StatusCreated, budget)
}

// GetBudget returns a specific budget
// ✅ Le service retournera automatiquement location/currency
func (h *Handler) GetBudget(c *gin.Context) {
	budgetID := c.Param("id")
	userID := c.GetString("user_id")

	budget, err := h.budgetService.GetByID(c.Request.Context(), budgetID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Budget not found"})
		return
	}

	c.JSON(http.StatusOK, budget)
}

// UpdateBudget updates a budget name
func (h *Handler) UpdateBudget(c *gin.Context) {
	budgetID := c.Param("id")
	userID := c.GetString("user_id")

	var req struct {
		Name string `json:"name" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check if user has access
	_, err := h.budgetService.GetByID(c.Request.Context(), budgetID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Budget not found"})
		return
	}

	if err := h.budgetService.Update(c.Request.Context(), budgetID, req.Name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update budget"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Budget updated successfully"})
}

// DeleteBudget deletes a budget
func (h *Handler) DeleteBudget(c *gin.Context) {
	budgetID := c.Param("id")
	userID := c.GetString("user_id")

	// Check if user is the owner
	budget, err := h.budgetService.GetByID(c.Request.Context(), budgetID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Budget not found"})
		return
	}

	if budget.OwnerID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Only the owner can delete the budget"})
		return
	}

	// Delete the budget
	if err := h.budgetService.Delete(c.Request.Context(), budgetID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete budget"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Budget deleted successfully"})
}

// GetBudgetData returns the JSON data for a budget
func (h *Handler) GetBudgetData(c *gin.Context) {
	budgetID := c.Param("id")
	userID := c.GetString("user_id")

	// Check access
	_, err := h.budgetService.GetByID(c.Request.Context(), budgetID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Budget not found"})
		return
	}

	data, err := h.budgetService.GetData(c.Request.Context(), budgetID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get budget data"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": data})
}

// UpdateBudgetData updates the JSON data for a budget
func (h *Handler) UpdateBudgetData(c *gin.Context) {
	budgetID := c.Param("id")
	userID := c.GetString("user_id")
	userName := c.GetString("user_name")

	// Check access
	_, err := h.budgetService.GetByID(c.Request.Context(), budgetID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Budget not found"})
		return
	}

	var req struct {
		Data map[string]interface{} `json:"data" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.budgetService.UpdateData(c.Request.Context(), budgetID, req.Data, userID, userName); err != nil {
		log.Printf("Error updating budget data: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update budget data"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Budget data updated successfully"})
}

// InviteMember invites a user to join the budget
func (h *Handler) InviteMember(c *gin.Context) {
	budgetID := c.Param("id")
	userID := c.GetString("user_id")

	// Check access
	_, err := h.budgetService.GetByID(c.Request.Context(), budgetID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Budget not found"})
		return
	}

	var req struct {
		Email string `json:"email" binding:"required,email"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	invitation, err := h.budgetService.InviteMember(c.Request.Context(), budgetID, req.Email, userID)
	if err != nil {
		log.Printf("Error inviting member: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Send invitation email
	go func() {
		if err := h.emailService.SendInvitation(req.Email, invitation.Token, budgetID); err != nil {
			log.Printf("Error sending invitation email: %v", err)
		}
	}()

	c.JSON(http.StatusOK, gin.H{"message": "Invitation sent successfully"})
}

// AcceptInvitation accepts an invitation to join a budget
func (h *Handler) AcceptInvitation(c *gin.Context) {
	userID := c.GetString("user_id")

	var req struct {
		Token string `json:"token" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.budgetService.AcceptInvitation(c.Request.Context(), req.Token, userID); err != nil {
		log.Printf("Error accepting invitation: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Invitation accepted successfully"})
}
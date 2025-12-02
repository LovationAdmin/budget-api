package handlers

import (
	"log"
	"net/http"

	"budget-api/services"

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
func (h *Handler) CreateBudget(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID := c.GetString("user_id")

	budget, err := h.budgetService.Create(c.Request.Context(), req.Name, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create budget"})
		return
	}

	c.JSON(http.StatusCreated, budget)
}

// GetBudget returns a specific budget
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

// UpdateBudget updates a budget
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

// DeleteBudget deletes a budget (NOUVEAU)
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

// GetBudgetData returns the data for a budget
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

// UpdateBudgetData updates the data for a budget
func (h *Handler) UpdateBudgetData(c *gin.Context) {
	budgetID := c.Param("id")
	userID := c.GetString("user_id")

	var req struct {
		Data interface{} `json:"data" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check access
	_, err := h.budgetService.GetByID(c.Request.Context(), budgetID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Budget not found"})
		return
	}

	if err := h.budgetService.UpdateData(c.Request.Context(), budgetID, req.Data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update budget data"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Budget data updated successfully"})
}

// InviteMember invites a member to a budget (MODIFIÉ)
func (h *Handler) InviteMember(c *gin.Context) {
	var req struct {
		Email string `json:"email" binding:"required,email"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	budgetID := c.Param("id")
	userID := c.GetString("user_id")

	// Check if user is the owner
	budget, err := h.budgetService.GetByID(c.Request.Context(), budgetID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Budget not found"})
		return
	}

	if budget.OwnerID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Only the owner can invite members"})
		return
	}

	// NOUVEAU : Check if there's an existing pending invitation
	existingInvitation, _ := h.budgetService.GetPendingInvitation(c.Request.Context(), budgetID, req.Email)
	if existingInvitation != nil {
		// Delete the old invitation
		if err := h.budgetService.DeleteInvitation(c.Request.Context(), existingInvitation.ID); err != nil {
			log.Printf("Failed to delete old invitation: %v", err)
		}
	}

	// Create invitation
	invitation, err := h.budgetService.CreateInvitation(c.Request.Context(), budgetID, req.Email, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Send email
	inviterName := budget.OwnerName
	if inviterName == "" {
		inviterName = "Un utilisateur"
	}

	if err := h.emailService.SendInvitation(req.Email, inviterName, budget.Name, invitation.Token); err != nil {
		log.Printf("Failed to send invitation email: %v", err)
		// Don't fail the request if email fails
	}

	c.JSON(http.StatusOK, gin.H{
		"message":    "Invitation sent successfully",
		"invitation": invitation,
	})
}

// AcceptInvitation accepts an invitation
func (h *Handler) AcceptInvitation(c *gin.Context) {
	var req struct {
		Token string `json:"token" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID := c.GetString("user_id")

	if err := h.budgetService.AcceptInvitation(c.Request.Context(), req.Token, userID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Invitation accepted successfully"})
}
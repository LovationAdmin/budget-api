// handlers/private_items.go
// Private items of the caller in a budget (see services/private_items.go).
//
//	GET    /budgets/:id/private-items          → { "items": { itemId: payload } }
//	PUT    /budgets/:id/private-items/:itemId  ← payload (JSON, ≤ 4 KB)
//	DELETE /budgets/:id/private-items/:itemId

package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/LovationAdmin/budget-api/services"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

type PrivateItemsHandler struct {
	budgets *services.BudgetService
	items   *services.PrivateItemService
}

func NewPrivateItemsHandler(budgets *services.BudgetService, items *services.PrivateItemService) *PrivateItemsHandler {
	return &PrivateItemsHandler{budgets: budgets, items: items}
}

// member checks that the caller belongs to the budget; it writes the error response otherwise.
func (h *PrivateItemsHandler) member(c *gin.Context) (budgetID, userID string, ok bool) {
	budgetID = c.Param("id")
	userID = c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return "", "", false
	}
	if _, err := h.budgets.GetByID(c.Request.Context(), budgetID, userID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Budget not found"})
		return "", "", false
	}
	return budgetID, userID, true
}

func (h *PrivateItemsHandler) List(c *gin.Context) {
	budgetID, userID, ok := h.member(c)
	if !ok {
		return
	}
	items, err := h.items.List(c.Request.Context(), budgetID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load private items"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *PrivateItemsHandler) Put(c *gin.Context) {
	budgetID, userID, ok := h.member(c)
	if !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, services.MaxPrivatePayloadBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid body"})
		return
	}
	if err := h.items.Put(c.Request.Context(), budgetID, userID, c.Param("itemId"), json.RawMessage(body)); err != nil {
		if errors.Is(err, services.ErrInvalidPrivateItem) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid private item"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save private item"})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *PrivateItemsHandler) Delete(c *gin.Context) {
	budgetID, userID, ok := h.member(c)
	if !ok {
		return
	}
	if err := h.items.Delete(c.Request.Context(), budgetID, userID, c.Param("itemId")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete private item"})
		return
	}
	c.Status(http.StatusNoContent)
}

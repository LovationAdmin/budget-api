// handlers/member_person.go
// « Who I am in this budget »: links a member's account to a person of the
// household. The personal overview (« Ma vue perso ») shows each user the
// numbers of the person they are, so a person belongs to one account only:
//
//	PUT /budgets/:id/members/:member_id/person  ← { "person_id": "p1" | "" | null }
//
// :member_id is a user id, as in DELETE /budgets/:id/members/:member_id.
// A member sets their own link: a person nobody else has, "" for « not in
// this household », null to forget. The budget's owner may clear someone
// else's link (a mistaken or abusive one), never set it.

package handlers

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/LovationAdmin/budget-api/services"
	"github.com/gin-gonic/gin"
)

func (h *Handler) SetMemberPerson(c *gin.Context) {
	budgetID := c.Param("id")
	userID := c.GetString("user_id")
	target := c.Param("member_id")

	var req struct {
		PersonID *string `json:"person_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || (req.PersonID != nil && len(*req.PersonID) > 100) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	budget, err := h.budgetService.GetByID(c.Request.Context(), budgetID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Budget not found"})
		return
	}
	if target != userID && (!budget.IsOwner || req.PersonID != nil) {
		c.JSON(http.StatusForbidden, gin.H{"error": tr(c,
			"Chacun relie lui-même son compte à sa fiche du foyer.",
			"Each member links their own account to their place in the household.")})
		return
	}

	switch err := h.budgetService.SetMemberPerson(c.Request.Context(), budgetID, target, req.PersonID); {
	case err == nil:
		c.JSON(http.StatusOK, gin.H{"person_id": req.PersonID})
	case errors.Is(err, services.ErrPersonTaken):
		c.JSON(http.StatusConflict, gin.H{"error": tr(c,
			"Cette personne est déjà reliée au compte d’un autre membre.",
			"This person is already linked to another member’s account.")})
	case errors.Is(err, services.ErrUnknownPerson):
		c.JSON(http.StatusBadRequest, gin.H{"error": tr(c,
			"Cette personne ne fait pas partie du foyer.",
			"This person isn’t part of the household.")})
	case errors.Is(err, sql.ErrNoRows):
		c.JSON(http.StatusNotFound, gin.H{"error": "Member not found"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save"})
	}
}

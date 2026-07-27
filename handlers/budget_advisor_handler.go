// handlers/budget_advisor_handler.go
// ============================================================================
// BUDGET ADVISOR HANDLER — Feature « Budget proposé par IA »
// ----------------------------------------------------------------------------
// POST /budgets/ai-proposal : reçoit un HouseholdInput, renvoie un
// BudgetProposal structuré généré par Claude. Utilisé aux deux points d'entrée
// (création d'un budget et « Recalculer avec l'IA » sur un budget existant).
//
// Confidentialité : on ne journalise ni freeText ni les montants — seulement
// des métadonnées non sensibles (type de foyer, nombre de membres).
// ============================================================================

package handlers

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LovationAdmin/budget-api/services"
)

type BudgetAdvisorHandler struct {
	advisor *services.BudgetAdvisorService
}

func NewBudgetAdvisorHandler(advisor *services.BudgetAdvisorService) *BudgetAdvisorHandler {
	return &BudgetAdvisorHandler{advisor: advisor}
}

// GenerateProposal handles POST /budgets/ai-proposal.
func (h *BudgetAdvisorHandler) GenerateProposal(c *gin.Context) {
	var input services.HouseholdInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Requête invalide : " + err.Error()})
		return
	}

	if len(input.Members) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Le foyer doit compter au moins un membre."})
		return
	}

	// Non-sensitive metadata only (never freeText or amounts).
	c.Set("advisor_household_type", input.HouseholdType)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 160*time.Second)
	defer cancel()

	proposal, err := h.advisor.GenerateProposal(ctx, input)
	if err != nil {
		// The error carries the upstream provider status/message (no user
		// financial data) — log it so failures are diagnosable in the server
		// logs while the client only sees a generic message.
		log.Printf("[AI advisor] generation failed (household=%s, members=%d): %v",
			input.HouseholdType, len(input.Members), err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "La génération du budget par l'IA a échoué. Réessayez dans un instant."})
		return
	}

	c.JSON(http.StatusOK, proposal)
}

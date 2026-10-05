// handlers/budget_advisor_handler.go
// ============================================================================
// BUDGET ADVISOR HANDLER — Feature « Budget proposé par IA »
// ----------------------------------------------------------------------------
// POST /budgets/ai-proposal        : reçoit un HouseholdInput, renvoie un
//                                    BudgetProposal structuré (JSON).
// POST /budgets/ai-proposal/stream : même chose en Server-Sent Events, avec la
//                                    progression réelle de la génération.
// Utilisés aux deux points d'entrée (création d'un budget et « Recalculer
// avec l'IA » sur un budget existant).
//
// Confidentialité : on ne journalise ni freeText ni les montants — seulement
// des métadonnées non sensibles (type de foyer, nombre de membres).
// ============================================================================

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LovationAdmin/budget-api/services"
)

// advisorDeadline bounds a whole generation, retry included. The web client
// waits a little longer, so the user always gets the server's explanation
// rather than a bare client timeout.
const advisorDeadline = 115 * time.Second

// sseHeartbeat keeps proxies from closing an idle stream while the model
// thinks.
const sseHeartbeat = 15 * time.Second

type BudgetAdvisorHandler struct {
	advisor *services.BudgetAdvisorService
}

func NewBudgetAdvisorHandler(advisor *services.BudgetAdvisorService) *BudgetAdvisorHandler {
	return &BudgetAdvisorHandler{advisor: advisor}
}

// bindHouseholdInput validates the request body; on failure it has already
// answered 400.
func bindHouseholdInput(c *gin.Context) (services.HouseholdInput, bool) {
	var input services.HouseholdInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Requête invalide : " + err.Error()})
		return input, false
	}
	if len(input.Members) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Le foyer doit compter au moins un membre."})
		return input, false
	}
	// Non-sensitive metadata only (never freeText or amounts).
	c.Set("advisor_household_type", input.HouseholdType)
	return input, true
}

// advisorErrorPayload is the error body of both endpoints.
func advisorErrorPayload(input services.HouseholdInput, err error) (int, gin.H) {
	var advErr *services.AdvisorError
	if !errors.As(err, &advErr) {
		advErr = &services.AdvisorError{Code: services.AdvisorErrUnavailable, Err: err}
	}
	// The error carries the upstream provider status/message (no user
	// financial data) — log it so failures are diagnosable in the server
	// logs while the client only sees a friendly message.
	log.Printf("[AI advisor] generation failed (household=%s, members=%d): %v",
		input.HouseholdType, len(input.Members), err)
	return advErr.HTTPStatus(), gin.H{
		"error":     advErr.UserMessage(),
		"code":      advErr.Code,
		"retryable": advErr.Retryable,
	}
}

// GenerateProposal handles POST /budgets/ai-proposal.
func (h *BudgetAdvisorHandler) GenerateProposal(c *gin.Context) {
	input, ok := bindHouseholdInput(c)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), advisorDeadline)
	defer cancel()

	proposal, err := h.advisor.GenerateProposal(ctx, input)
	if err != nil {
		status, body := advisorErrorPayload(input, err)
		c.JSON(status, body)
		return
	}

	c.JSON(http.StatusOK, proposal)
}

// StreamProposal handles POST /budgets/ai-proposal/stream. Events:
//
//	progress  {"stage": "...", "method"?: "...", "attempt"?: n}
//	result    BudgetProposal
//	error     {"error": "...", "code": "...", "retryable": bool}
//
// A comment line is sent every sseHeartbeat. Closing the connection cancels
// the generation upstream.
func (h *BudgetAdvisorHandler) StreamProposal(c *gin.Context) {
	input, ok := bindHouseholdInput(c)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), advisorDeadline)
	defer cancel()

	header := c.Writer.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache, no-transform")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	c.Writer.Flush()

	type outcome struct {
		proposal *services.BudgetProposal
		err      error
	}
	progress := make(chan services.AdvisorProgress, 32)
	done := make(chan outcome, 1)
	go func() {
		p, err := h.advisor.GenerateProposalWithProgress(ctx, input, func(ev services.AdvisorProgress) {
			// Never block the generation on a slow client.
			select {
			case progress <- ev:
			default:
			}
		})
		done <- outcome{p, err}
	}()

	send := func(event string, payload any) {
		data, err := json.Marshal(payload)
		if err != nil {
			return
		}
		fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, data)
		c.Writer.Flush()
	}

	heartbeat := time.NewTicker(sseHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case ev := <-progress:
			send("progress", ev)
		case <-heartbeat.C:
			fmt.Fprint(c.Writer, ": ping\n\n")
			c.Writer.Flush()
		case out := <-done:
			// Progress queued just before the end still goes out first.
			for pending := true; pending; {
				select {
				case ev := <-progress:
					send("progress", ev)
				default:
					pending = false
				}
			}
			if out.err != nil {
				_, body := advisorErrorPayload(input, out.err)
				send("error", body)
				return
			}
			send("result", out.proposal)
			return
		case <-c.Request.Context().Done():
			// Client gone: the deferred cancel stops the upstream call.
			return
		}
	}
}

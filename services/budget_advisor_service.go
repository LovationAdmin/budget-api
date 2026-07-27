package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// ============================================================================
// BUDGET ADVISOR SERVICE — Feature « Budget proposé par IA »
// ----------------------------------------------------------------------------
// À partir d'une situation de foyer (HouseholdInput), produit une répartition
// mensuelle structurée (BudgetProposal) via Claude. Le prompt système est le
// bloc « section 5 » du brief, collé verbatim ; l'exemple « section 6 » est
// injecté en few-shot. La sortie est parsée puis validée ; en cas d'échec de
// parsing, un unique retry est effectué.
//
// Confidentialité : ni freeText ni les montants ne sont journalisés ici.
// ============================================================================

// ---------------------------------------------------------------------------
// CONTRAT D'ENTRÉE — HouseholdInput (section 3)
// ---------------------------------------------------------------------------

type AdvisorMember struct {
	ID                      string   `json:"id"`
	Label                   string   `json:"label"`
	NetIncome               float64  `json:"netIncome"`
	VariableIncomeYearly    *float64 `json:"variableIncomeYearly,omitempty"`
	PersonalSpendingMonthly *float64 `json:"personalSpendingMonthly,omitempty"`
}

type AdvisorCharge struct {
	Label    string  `json:"label"`
	Amount   float64 `json:"amount"`
	Category string  `json:"category"`
	Scope    string  `json:"scope"`
	OwnerID  string  `json:"ownerId,omitempty"`
}

type AdvisorDebt struct {
	Label          string  `json:"label"`
	MonthlyPayment float64 `json:"monthlyPayment"`
	Scope          string  `json:"scope"`
	OwnerID        string  `json:"ownerId,omitempty"`
}

type AdvisorObjective struct {
	Label         string   `json:"label"`
	TargetAmount  *float64 `json:"targetAmount,omitempty"`
	HorizonMonths *int     `json:"horizonMonths,omitempty"`
	Priority      string   `json:"priority"`
}

type HouseholdInput struct {
	HouseholdType         string             `json:"householdType"`
	Country               string             `json:"country,omitempty"`
	Members               []AdvisorMember    `json:"members"`
	Charges               []AdvisorCharge    `json:"charges"`
	Debts                 []AdvisorDebt      `json:"debts,omitempty"`
	Objectives            []AdvisorObjective `json:"objectives"`
	WantsPersonalSavings  bool               `json:"wantsPersonalSavings"`
	AllowInterMemberTopUp bool               `json:"allowInterMemberTopUp"`
	PreferredMethod       string             `json:"preferredMethod,omitempty"`
	AnticipatedLifeEvents []string           `json:"anticipatedLifeEvents,omitempty"`
	Constraints           string             `json:"constraints,omitempty"`
	FreeText              string             `json:"freeText"`
}

// ---------------------------------------------------------------------------
// CONTRAT DE SORTIE — BudgetProposal (section 4)
// ---------------------------------------------------------------------------

type MemberBudget struct {
	MemberID                string  `json:"memberId"`
	MonthlyContribution     float64 `json:"monthlyContribution"`
	ResteAVivre             float64 `json:"resteAVivre"`
	PocketMoney             float64 `json:"pocketMoney"`
	PersonalSavingsCapacity float64 `json:"personalSavingsCapacity"`
	Feasibility             string  `json:"feasibility"`
}

type FundedBy struct {
	MemberID string  `json:"memberId"`
	Amount   float64 `json:"amount"`
}

type AllocationLine struct {
	Category string     `json:"category"`
	Label    string     `json:"label"`
	Amount   float64    `json:"amount"`
	Type     string     `json:"type"`
	FundedBy []FundedBy `json:"fundedBy"`
	Notes    string     `json:"notes,omitempty"`
}

type SavingsEnvelope struct {
	Name                string   `json:"name"`
	Priority            string   `json:"priority"`
	TargetAmount        *float64 `json:"targetAmount,omitempty"`
	HorizonMonths       *int     `json:"horizonMonths,omitempty"`
	MonthlyContribution float64  `json:"monthlyContribution"`
	VehicleSuggestion   string   `json:"vehicleSuggestion,omitempty"`
}

type SeparationHandling struct {
	Approach string `json:"approach"`
	Note     string `json:"note"`
}

type FeasibilityReport struct {
	Status          string   `json:"status"`
	BindingMemberID string   `json:"bindingMemberId,omitempty"`
	Issues          []string `json:"issues"`
	SuggestedLevers []string `json:"suggestedLevers"`
}

type BudgetProposal struct {
	MethodChosen         string             `json:"methodChosen"`
	MethodRationale      string             `json:"methodRationale"`
	AccountStructure     string             `json:"accountStructure"`
	AccountRationale     string             `json:"accountRationale"`
	MonthlyAllocation    []AllocationLine   `json:"monthlyAllocation"`
	PerMember            []MemberBudget     `json:"perMember"`
	SavingsEnvelopes     []SavingsEnvelope  `json:"savingsEnvelopes"`
	VariableIncomePolicy string             `json:"variableIncomePolicy"`
	SeparationHandling   SeparationHandling `json:"separationHandling"`
	Feasibility          FeasibilityReport  `json:"feasibility"`
	LifeEventNotes       []string           `json:"lifeEventNotes"`
	VehicleSuggestions   []string           `json:"vehicleSuggestions"`
	AssumptionsMade      []string           `json:"assumptionsMade"`
	OpenQuestions        []string           `json:"openQuestions"`
	Disclaimer           string             `json:"disclaimer"`
	Summary              string             `json:"summary"`
}

// ---------------------------------------------------------------------------
// SERVICE
// ---------------------------------------------------------------------------

type BudgetAdvisorService struct {
	ai        *ClaudeAIService
	model     string
	maxTokens int
}

func NewBudgetAdvisorService(ai *ClaudeAIService) *BudgetAdvisorService {
	// Use a dedicated Claude client with a longer HTTP timeout: a full
	// BudgetProposal (system prompt + few-shot + up to 4000 output tokens) can
	// take well over the shared client's 60s cap, which otherwise surfaces as a
	// failed generation. A dedicated client avoids affecting other features.
	svc := NewClaudeAIService()
	svc.httpClient = &http.Client{Timeout: 150 * time.Second}

	// Model is overridable via ADVISOR_MODEL so it can be switched to whatever
	// the deployed ANTHROPIC_API_KEY has access to, without a code change.
	// The prod org is on the Claude 5 family (Sonnet 4 returned 404), so default
	// to Sonnet 5 — a strong, cost-effective fit for structured generation.
	model := os.Getenv("ADVISOR_MODEL")
	if model == "" {
		model = "claude-sonnet-5"
	}

	// A full BudgetProposal (all arrays + French prose) exceeds 4000 output
	// tokens and was being truncated at the cap, producing invalid JSON.
	// Give it real headroom; overridable via ADVISOR_MAX_TOKENS.
	maxTokens := 12000
	if v := os.Getenv("ADVISOR_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxTokens = n
		}
	}

	return &BudgetAdvisorService{
		ai:        svc,
		model:     model,
		maxTokens: maxTokens,
	}
}

// buildAdvisorMessages assembles the few-shot + real-input conversation. It MUST
// end with a user message: the Claude 5 family rejects assistant-message prefill
// ("the conversation must end with a user message").
func buildAdvisorMessages(input HouseholdInput) ([]ClaudeMessage, error) {
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize household input: %w", err)
	}
	userContent := string(inputJSON) +
		"\n\nRenvoie un objet BudgetProposal conforme au schéma, en JSON uniquement — commence directement par { et termine par }, sans texte ni balises Markdown autour."
	return []ClaudeMessage{
		{Role: "user", Content: advisorFewShotInput},
		{Role: "assistant", Content: advisorFewShotOutput},
		{Role: "user", Content: userContent},
	}, nil
}

// GenerateProposal calls Claude with the section-5 system prompt + few-shot and
// returns a validated BudgetProposal. Retries once on failure.
func (s *BudgetAdvisorService) GenerateProposal(ctx context.Context, input HouseholdInput) (*BudgetProposal, error) {
	if len(input.Members) == 0 {
		return nil, fmt.Errorf("household must have at least one member")
	}

	messages, err := buildAdvisorMessages(input)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		// Stop early if the caller (HTTP request) is already gone, so we don't
		// burn a second ~1-minute LLM call after the client has timed out.
		if ctx.Err() != nil {
			return nil, fmt.Errorf("advisor aborted: %w", ctx.Err())
		}
		raw, err := s.ai.CallMessages(ctx, budgetAdvisorSystemPrompt, messages, s.model, s.maxTokens)
		if err != nil {
			lastErr = fmt.Errorf("advisor LLM call failed: %w", err)
			log.Printf("[AI advisor] attempt %d: %v", attempt+1, lastErr)
			continue
		}
		proposal, perr := parseProposal(raw)
		if perr != nil {
			prefix := strings.TrimSpace(raw)
			if len(prefix) > 200 {
				prefix = prefix[:200]
			}
			lastErr = fmt.Errorf("advisor returned invalid JSON: %w", perr)
			log.Printf("[AI advisor] attempt %d: %v | response starts: %q", attempt+1, lastErr, prefix)
			continue
		}
		sanitizeProposal(proposal)
		if verr := validateProposal(proposal, input); verr != nil {
			lastErr = fmt.Errorf("advisor proposal failed validation: %w", verr)
			log.Printf("[AI advisor] attempt %d: %v", attempt+1, lastErr)
			continue
		}
		return proposal, nil
	}

	return nil, lastErr
}

// parseProposal extracts the JSON object from a raw LLM response (tolerating any
// stray Markdown fences) and unmarshals it into a BudgetProposal.
func parseProposal(raw string) (*BudgetProposal, error) {
	cleaned := extractJSONObject(raw)
	if cleaned == "" {
		return nil, fmt.Errorf("no JSON object found in response")
	}
	var proposal BudgetProposal
	if err := json.Unmarshal([]byte(cleaned), &proposal); err != nil {
		return nil, err
	}
	return &proposal, nil
}

// extractJSONObject returns the substring from the first '{' to the last '}',
// stripping common ```json fences first.
func extractJSONObject(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start == -1 || end == -1 || end < start {
		return ""
	}
	return s[start : end+1]
}

// sanitizeProposal coerces non-critical fields to safe values in place, so a
// usable proposal is never rejected (and re-generated, doubling latency) over a
// minor discrepancy. Feasibility statuses drive UI colors, so unknown values are
// mapped to "tight"; an empty disclaimer gets the standard one.
func sanitizeProposal(p *BudgetProposal) {
	coerceStatus := func(s string) string {
		if isOneOf(s, "ok", "tight", "infeasible") {
			return s
		}
		return "tight"
	}
	p.Feasibility.Status = coerceStatus(p.Feasibility.Status)
	for i := range p.PerMember {
		p.PerMember[i].Feasibility = coerceStatus(p.PerMember[i].Feasibility)
	}
	if strings.TrimSpace(p.Disclaimer) == "" {
		p.Disclaimer = "Aide à la décision, pas un conseil financier ni juridique. Faites valider les volets fiscal, régime matrimonial et propriété par un professionnel."
	}
}

// validateProposal keeps only the hard structural checks that would break the
// UI if missing. Everything else is sanitized rather than rejected, so a single
// LLM call is enough in the common case (avoids the 2× latency of a retry).
func validateProposal(p *BudgetProposal, input HouseholdInput) error {
	if len(p.MonthlyAllocation) == 0 {
		return fmt.Errorf("monthlyAllocation is empty")
	}
	if len(p.PerMember) == 0 {
		return fmt.Errorf("perMember is empty")
	}
	if strings.TrimSpace(p.Summary) == "" {
		return fmt.Errorf("summary is empty")
	}
	return nil
}

func isOneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

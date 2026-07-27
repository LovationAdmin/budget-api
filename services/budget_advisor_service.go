package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
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
	// Default to Claude 3.5 Sonnet (v2), which is broadly available; the prod
	// key returned 404 for claude-sonnet-4-*, so that is not a safe default.
	model := os.Getenv("ADVISOR_MODEL")
	if model == "" {
		model = "claude-3-5-sonnet-20241022"
	}
	return &BudgetAdvisorService{
		ai:        svc,
		model:     model,
		maxTokens: 4000,
	}
}

// GenerateProposal calls Claude with the section-5 system prompt + few-shot and
// returns a validated BudgetProposal. It retries once on parse failure.
func (s *BudgetAdvisorService) GenerateProposal(ctx context.Context, input HouseholdInput) (*BudgetProposal, error) {
	if len(input.Members) == 0 {
		return nil, fmt.Errorf("household must have at least one member")
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize household input: %w", err)
	}

	userContent := string(inputJSON) + "\n\nRenvoie un objet BudgetProposal conforme au schéma, en JSON uniquement."

	messages := []ClaudeMessage{
		{Role: "user", Content: advisorFewShotInput},
		{Role: "assistant", Content: advisorFewShotOutput},
		{Role: "user", Content: userContent},
	}

	raw, err := s.ai.CallMessages(ctx, budgetAdvisorSystemPrompt, messages, s.model, s.maxTokens)
	if err != nil {
		return nil, fmt.Errorf("advisor LLM call failed: %w", err)
	}

	proposal, parseErr := parseProposal(raw)
	if parseErr != nil {
		// One retry with an explicit correction message appended.
		retryMessages := append(messages,
			ClaudeMessage{Role: "assistant", Content: raw},
			ClaudeMessage{Role: "user", Content: "Ta réponse n'était pas un JSON valide. Renvoie uniquement l'objet BudgetProposal, sans texte hors JSON ni balises Markdown."},
		)
		raw, err = s.ai.CallMessages(ctx, budgetAdvisorSystemPrompt, retryMessages, s.model, s.maxTokens)
		if err != nil {
			return nil, fmt.Errorf("advisor LLM retry failed: %w", err)
		}
		proposal, parseErr = parseProposal(raw)
		if parseErr != nil {
			return nil, fmt.Errorf("advisor returned invalid JSON after retry: %w", parseErr)
		}
	}

	if err := validateProposal(proposal, input); err != nil {
		return nil, fmt.Errorf("advisor proposal failed validation: %w", err)
	}

	return proposal, nil
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

// validateProposal enforces the structural invariants from the brief: known
// enum values, a per-member row for every member, and fundedBy sums matching
// each allocation line's amount.
func validateProposal(p *BudgetProposal, input HouseholdInput) error {
	if !isOneOf(p.MethodChosen, "prorata", "equal", "equalized_reste", "all_common") {
		return fmt.Errorf("invalid methodChosen: %q", p.MethodChosen)
	}
	if !isOneOf(p.AccountStructure, "three_accounts", "all_common_equal_pocket") {
		return fmt.Errorf("invalid accountStructure: %q", p.AccountStructure)
	}
	if !isOneOf(p.Feasibility.Status, "ok", "tight", "infeasible") {
		return fmt.Errorf("invalid feasibility.status: %q", p.Feasibility.Status)
	}
	if len(p.MonthlyAllocation) == 0 {
		return fmt.Errorf("monthlyAllocation is empty")
	}
	if len(p.PerMember) != len(input.Members) {
		return fmt.Errorf("perMember count (%d) does not match members (%d)", len(p.PerMember), len(input.Members))
	}

	// fundedBy of each line must sum to the line amount (tolerance 1 unit for rounding).
	for i, line := range p.MonthlyAllocation {
		var sum float64
		for _, f := range line.FundedBy {
			sum += f.Amount
		}
		if diff := sum - line.Amount; diff > 1.0 || diff < -1.0 {
			return fmt.Errorf("allocation line %d (%q): fundedBy sum %.2f != amount %.2f", i, line.Label, sum, line.Amount)
		}
	}

	if p.Summary == "" {
		return fmt.Errorf("summary is empty")
	}
	if p.Disclaimer == "" {
		return fmt.Errorf("disclaimer is empty")
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

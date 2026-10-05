package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// ============================================================================
// BUDGET ADVISOR SERVICE — Feature « Budget proposé par IA »
// ----------------------------------------------------------------------------
// À partir d'une situation de foyer (HouseholdInput), produit une répartition
// mensuelle structurée (BudgetProposal) via Claude. Le prompt système est le
// bloc « section 5 » du brief, collé verbatim ; l'exemple « section 6 » est
// injecté en few-shot. La réponse est contrainte par un JSON schema (sorties
// structurées) et streamée, ce qui permet de remonter une progression réelle
// au client. Un retry n'a lieu que s'il reste le temps d'une réponse complète.
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
	// AlreadySaved is what the pot already holds for this objective, so the
	// monthly pace only covers what is left to gather.
	AlreadySaved *float64 `json:"alreadySaved,omitempty"`
	Priority     string   `json:"priority"`
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
	effort    string
	maxTokens int
	// firstTextTimeout: an attempt still only thinking after this long is
	// unlikely to finish in time (prod: 2 min of thinking, then no answer);
	// it is stopped and retried at low effort while there is time left.
	firstTextTimeout time.Duration
}

// advisorMinRetryWindow is the time a second attempt needs to have a chance to
// finish. With less left, the error is returned at once rather than making
// the user wait for an attempt that would be cut off.
const advisorMinRetryWindow = 40 * time.Second

func NewBudgetAdvisorService(ai *ClaudeAIService) *BudgetAdvisorService {
	// Model is overridable via ADVISOR_MODEL so it can be switched to whatever
	// the deployed ANTHROPIC_API_KEY has access to, without a code change.
	// Sonnet 5.5: same price as Sonnet 5, faster output, and the model the
	// market analysis already runs on.
	model := os.Getenv("ADVISOR_MODEL")
	if model == "" {
		model = "claude-sonnet-5-5"
	}

	// Thinking depth. At the API default ("high") the model could spend the
	// whole token budget thinking and return no answer at all (prod: 12000
	// thinking tokens, 2 minutes, then an empty response). "medium" keeps the
	// reasoning the allocation needs at a fraction of the latency.
	// Overridable via ADVISOR_EFFORT (low | medium | high).
	effort := os.Getenv("ADVISOR_EFFORT")
	if effort == "" {
		effort = "medium"
	}

	// Thinking and answer share max_tokens; streamed, so a large budget does
	// not risk an HTTP timeout. Overridable via ADVISOR_MAX_TOKENS.
	maxTokens := 16000
	if v := os.Getenv("ADVISOR_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxTokens = n
		}
	}

	return &BudgetAdvisorService{
		ai:               ai,
		model:            model,
		effort:           effort,
		maxTokens:        maxTokens,
		firstTextTimeout: 50 * time.Second,
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

func (s *BudgetAdvisorService) request(messages []ClaudeMessage, effort string) ClaudeRequest {
	return ClaudeRequest{
		Model:     s.model,
		MaxTokens: s.maxTokens,
		System:    budgetAdvisorSystemPrompt,
		Messages:  messages,
		OutputConfig: &OutputConfig{
			Effort: effort,
			Format: JSONSchemaFormat(budgetProposalSchema),
		},
		Fallbacks: refusalFallback(s.model),
	}
}

// ---------------------------------------------------------------------------
// PROGRESSION
// ---------------------------------------------------------------------------

// Advisor progress stages, in generation order.
const (
	AdvisorStageAnalyzing   = "analyzing"
	AdvisorStageMethod      = "method"
	AdvisorStageAllocation  = "allocation"
	AdvisorStageMembers     = "members"
	AdvisorStageSavings     = "savings"
	AdvisorStageFeasibility = "feasibility"
	AdvisorStageSummary     = "summary"
	AdvisorStageRetry       = "retry"
)

// AdvisorProgress is reported while a proposal is generated.
type AdvisorProgress struct {
	Stage   string `json:"stage"`
	Method  string `json:"method,omitempty"`
	Attempt int    `json:"attempt,omitempty"`
}

// advisorStageKeys maps top-level keys of the streamed JSON to stages, in
// schema order.
var advisorStageKeys = []struct{ key, stage string }{
	{`"methodChosen"`, AdvisorStageMethod},
	{`"monthlyAllocation"`, AdvisorStageAllocation},
	{`"perMember"`, AdvisorStageMembers},
	{`"savingsEnvelopes"`, AdvisorStageSavings},
	{`"feasibility"`, AdvisorStageFeasibility},
	{`"summary"`, AdvisorStageSummary},
}

var methodValueRe = regexp.MustCompile(`"methodChosen"\s*:\s*"([a-z_]+)"`)

// progressTracker turns the growing JSON answer into stage changes. Each key
// is searched only after the previous one, so the per-member "feasibility"
// field does not trigger the top-level feasibility stage.
type progressTracker struct {
	emit    func(AdvisorProgress)
	next    int
	pos     int
	method  string
	started atomic.Bool // the answer has started (thinking is over)
}

func (t *progressTracker) onEvent(ev StreamEvent) {
	if ev.Kind != "text" {
		return
	}
	t.started.Store(true)
	for t.next < len(advisorStageKeys) {
		k := advisorStageKeys[t.next]
		idx := strings.Index(ev.Text[t.pos:], k.key)
		if idx < 0 {
			break
		}
		t.pos += idx + len(k.key)
		t.next++
		t.emit(AdvisorProgress{Stage: k.stage})
	}
	if t.method == "" && t.next > 0 {
		if m := methodValueRe.FindStringSubmatch(ev.Text); m != nil {
			t.method = m[1]
			t.emit(AdvisorProgress{Stage: AdvisorStageMethod, Method: m[1]})
		}
	}
}

// ---------------------------------------------------------------------------
// ERREURS
// ---------------------------------------------------------------------------

// Error codes returned to the client, which picks its message from them.
const (
	AdvisorErrTimeout       = "ai_timeout"
	AdvisorErrBusy          = "ai_busy"
	AdvisorErrUnavailable   = "ai_unavailable"
	AdvisorErrInvalidOutput = "ai_invalid_output"
	AdvisorErrRefused       = "ai_refused"
	AdvisorErrCanceled      = "ai_canceled"
)

var errInvalidProposal = errors.New("advisor returned an unusable proposal")

// AdvisorError is a failed generation, classified for the client.
type AdvisorError struct {
	Code      string
	Retryable bool
	Err       error
}

func (e *AdvisorError) Error() string {
	if e.Err == nil {
		return e.Code
	}
	return e.Code + ": " + e.Err.Error()
}

func (e *AdvisorError) Unwrap() error { return e.Err }

// UserMessage is the French message shown to the user.
func (e *AdvisorError) UserMessage() string {
	switch e.Code {
	case AdvisorErrTimeout:
		return "L'IA a mis trop de temps à répondre. Réessayez dans un instant."
	case AdvisorErrBusy:
		return "Le service IA est très sollicité en ce moment. Réessayez dans une minute."
	case AdvisorErrInvalidOutput:
		return "L'IA a produit une proposition incomplète. Réessayez dans un instant."
	case AdvisorErrRefused:
		return "L'IA n'a pas pu traiter cette demande. Reformulez la description de votre situation."
	case AdvisorErrCanceled:
		return "Génération annulée."
	default:
		return "Le service IA est momentanément indisponible. Réessayez plus tard."
	}
}

// HTTPStatus is the status the JSON endpoint answers with.
func (e *AdvisorError) HTTPStatus() int {
	switch e.Code {
	case AdvisorErrTimeout:
		return 504
	case AdvisorErrBusy, AdvisorErrUnavailable:
		return 503
	case AdvisorErrRefused:
		return 422
	case AdvisorErrCanceled:
		return 499
	default:
		return 502
	}
}

func classifyAdvisorError(ctx context.Context, err error) *AdvisorError {
	var apiErr *APIError
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return &AdvisorError{Code: AdvisorErrCanceled, Err: err}
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		return &AdvisorError{Code: AdvisorErrTimeout, Retryable: true, Err: err}
	case errors.Is(err, ErrRefusal):
		return &AdvisorError{Code: AdvisorErrRefused, Err: err}
	case errors.Is(err, ErrNotConfigured):
		return &AdvisorError{Code: AdvisorErrUnavailable, Err: err}
	case errors.As(err, &apiErr):
		if IsTransientAIError(err) {
			return &AdvisorError{Code: AdvisorErrBusy, Retryable: true, Err: err}
		}
		// 400/401/403/404: configuration (model, key, request shape).
		return &AdvisorError{Code: AdvisorErrUnavailable, Err: err}
	case errors.Is(err, ErrMaxTokens) || errors.Is(err, errInvalidProposal) || errors.Is(err, ErrMalformedResponse):
		return &AdvisorError{Code: AdvisorErrInvalidOutput, Retryable: true, Err: err}
	default:
		// Network failures, stalled streams.
		return &AdvisorError{Code: AdvisorErrBusy, Retryable: true, Err: err}
	}
}

// ---------------------------------------------------------------------------
// GÉNÉRATION
// ---------------------------------------------------------------------------

// GenerateProposal returns a validated BudgetProposal (see
// GenerateProposalWithProgress).
func (s *BudgetAdvisorService) GenerateProposal(ctx context.Context, input HouseholdInput) (*BudgetProposal, error) {
	return s.GenerateProposalWithProgress(ctx, input, nil)
}

// GenerateProposalWithProgress streams the proposal from Claude, reporting
// stages to onProgress (may be nil). A failed attempt is retried once, only
// when the failure may not repeat and the context leaves time for a full
// answer. Errors are *AdvisorError.
func (s *BudgetAdvisorService) GenerateProposalWithProgress(ctx context.Context, input HouseholdInput, onProgress func(AdvisorProgress)) (*BudgetProposal, error) {
	if len(input.Members) == 0 {
		return nil, fmt.Errorf("household must have at least one member")
	}
	emit := func(p AdvisorProgress) {
		if onProgress != nil {
			onProgress(p)
		}
	}

	messages, err := buildAdvisorMessages(input)
	if err != nil {
		return nil, err
	}

	effort := s.effort
	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		if attempt > 1 {
			if !hasTimeFor(ctx, advisorMinRetryWindow) {
				log.Printf("[AI advisor] no time left for a retry")
				break
			}
			emit(AdvisorProgress{Stage: AdvisorStageRetry, Attempt: attempt})
		}
		emit(AdvisorProgress{Stage: AdvisorStageAnalyzing, Attempt: attempt})

		tracker := &progressTracker{emit: emit}
		attemptCtx, cancelAttempt := context.WithCancel(ctx)
		var stuckThinking atomic.Bool
		if attempt == 1 && s.firstTextTimeout > 0 {
			guard := time.AfterFunc(s.firstTextTimeout, func() {
				if !tracker.started.Load() {
					stuckThinking.Store(true)
					cancelAttempt()
				}
			})
			defer guard.Stop()
		}
		res, err := s.ai.Stream(attemptCtx, s.request(messages, effort), tracker.onEvent)
		cancelAttempt()
		if stuckThinking.Load() && ctx.Err() == nil {
			err = fmt.Errorf("%w: still thinking after %s", ErrMaxTokens, s.firstTextTimeout)
		}
		if err == nil && res.StopReason == "max_tokens" {
			err = ErrMaxTokens
		}
		if err != nil {
			lastErr = fmt.Errorf("advisor LLM call failed: %w", err)
			log.Printf("[AI advisor] attempt %d (effort=%s): %v", attempt, effort, lastErr)
			if ctx.Err() != nil || !(IsTransientAIError(err) || errors.Is(err, ErrMaxTokens)) {
				break
			}
			if errors.Is(err, ErrMaxTokens) {
				// The budget went to thinking: think less on the retry.
				effort = "low"
			}
			if wait := advisorRetryDelay(err); wait > 0 && hasTimeFor(ctx, advisorMinRetryWindow+wait) {
				select {
				case <-time.After(wait):
				case <-ctx.Done():
				}
			}
			continue
		}

		proposal, perr := parseProposal(res.Text)
		if perr != nil {
			prefix := strings.TrimSpace(res.Text)
			if len(prefix) > 200 {
				prefix = prefix[:200]
			}
			lastErr = fmt.Errorf("%w: invalid JSON: %v", errInvalidProposal, perr)
			log.Printf("[AI advisor] attempt %d: %v | response starts: %q", attempt, lastErr, prefix)
			continue
		}
		sanitizeProposal(proposal)
		if verr := validateProposal(proposal, input); verr != nil {
			lastErr = fmt.Errorf("%w: %v", errInvalidProposal, verr)
			log.Printf("[AI advisor] attempt %d: %v", attempt, lastErr)
			continue
		}
		return proposal, nil
	}

	return nil, classifyAdvisorError(ctx, lastErr)
}

// advisorRetryDelay is the pause before retrying: an overloaded or
// rate-limited API rarely recovers instantly.
func advisorRetryDelay(err error) time.Duration {
	var apiErr *APIError
	if !errors.As(err, &apiErr) || (apiErr.Status != 429 && apiErr.Status < 500) {
		return 0
	}
	if apiErr.RetryAfter > 0 && apiErr.RetryAfter < 5*time.Second {
		return apiErr.RetryAfter
	}
	return 2 * time.Second
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
// mapped to "tight"; an empty disclaimer gets the standard one; missing lists
// become empty lists (the UI reads their length).
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
	for i := range p.MonthlyAllocation {
		if p.MonthlyAllocation[i].FundedBy == nil {
			p.MonthlyAllocation[i].FundedBy = []FundedBy{}
		}
	}
	for _, list := range []*[]string{
		&p.Feasibility.Issues, &p.Feasibility.SuggestedLevers, &p.LifeEventNotes,
		&p.VehicleSuggestions, &p.AssumptionsMade, &p.OpenQuestions,
	} {
		if *list == nil {
			*list = []string{}
		}
	}
	if p.SavingsEnvelopes == nil {
		p.SavingsEnvelopes = []SavingsEnvelope{}
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

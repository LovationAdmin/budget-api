package services

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// ============================================================================
// CLAUDE AI SERVICE - Client HTTP de l'API Messages d'Anthropic
// Utilisé par l'analyse de marché, le conseiller budgétaire et la
// catégorisation. Gère : effort, sorties structurées (JSON schema), streaming,
// stop_reason (max_tokens / refusal) et retries sur erreurs transitoires.
// ============================================================================

type ClaudeAIService struct {
	apiKey       string
	baseURL      string
	model        string
	maxTokens    int
	httpClient   *http.Client
	streamClient *http.Client
	// streamIdleTimeout aborts a stream that stops sending bytes (the API
	// sends periodic ping events, so silence means a stalled connection).
	streamIdleTimeout time.Duration
}

type ClaudeRequest struct {
	Model        string          `json:"model"`
	MaxTokens    int             `json:"max_tokens"`
	System       string          `json:"system,omitempty"` // Added System prompt support
	Messages     []ClaudeMessage `json:"messages"`
	OutputConfig *OutputConfig   `json:"output_config,omitempty"`
	// Fallbacks re-runs a request declined by the safety classifiers on another
	// model, server-side. "default" lets the API pick it by refusal category.
	Fallbacks string `json:"fallbacks,omitempty"`
	Stream    bool   `json:"stream,omitempty"`
}

// OutputConfig controls thinking depth (effort) and the response format.
type OutputConfig struct {
	Effort string        `json:"effort,omitempty"`
	Format *OutputFormat `json:"format,omitempty"`
}

// OutputFormat constrains the answer to a JSON schema (structured outputs).
type OutputFormat struct {
	Type   string          `json:"type"`
	Schema json.RawMessage `json:"schema"`
}

// JSONSchemaFormat returns a structured-output format for the given schema.
func JSONSchemaFormat(schema string) *OutputFormat {
	return &OutputFormat{Type: "json_schema", Schema: json.RawMessage(schema)}
}

type ClaudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ClaudeResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Model       string       `json:"model"`
	StopReason  string       `json:"stop_reason"`
	StopDetails *StopDetails `json:"stop_details"`
	Usage       struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// StopDetails is set when stop_reason is "refusal".
type StopDetails struct {
	Category    string `json:"category"`
	Explanation string `json:"explanation"`
}

// ClaudeResult is a completed Messages API call. Callers must check
// StopReason: "max_tokens" means Text is truncated (incomplete JSON).
type ClaudeResult struct {
	Text         string
	StopReason   string
	Model        string
	InputTokens  int
	OutputTokens int
}

// ============================================================================
// ERREURS TYPÉES
// ============================================================================

var (
	// ErrMaxTokens: the token budget ran out (often all spent on thinking)
	// before the answer was complete.
	ErrMaxTokens = errors.New("claude: output hit max_tokens before the answer was complete")
	// ErrRefusal: the safety classifiers declined the request.
	ErrRefusal = errors.New("claude: request declined")
	// ErrStreamStalled: the stream stopped sending bytes.
	ErrStreamStalled = errors.New("claude: stream stalled")
	// ErrNotConfigured: no API key in the environment.
	ErrNotConfigured = errors.New("ANTHROPIC_API_KEY not set")
	// ErrMalformedResponse: the answer could not be decoded; resending the
	// same request will not fix it.
	ErrMalformedResponse = errors.New("claude: malformed response")
)

// APIError is an error answer from the Messages API (HTTP status or an
// in-stream error event).
type APIError struct {
	Status     int
	Type       string
	Message    string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API returned status %d (%s): %s", e.Status, e.Type, e.Message)
}

// IsTransientAIError reports whether the same request may succeed if retried
// (rate limit, overload, server error, network failure, stalled stream).
func IsTransientAIError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status == 408 || apiErr.Status == 409 || apiErr.Status == 429 || apiErr.Status >= 500
	}
	if errors.Is(err, ErrRefusal) || errors.Is(err, ErrMaxTokens) || errors.Is(err, ErrNotConfigured) || errors.Is(err, ErrMalformedResponse) {
		return false
	}
	// Remaining errors are transport-level (connection reset, EOF, stall).
	// Request-building errors never reach here: they are returned before any
	// retry decision.
	return true
}

// statusForStreamError maps an in-stream error type to the HTTP status the
// same error would have had before the stream started.
func statusForStreamError(errType string) int {
	switch errType {
	case "overloaded_error":
		return 529
	case "rate_limit_error":
		return 429
	case "invalid_request_error":
		return 400
	case "authentication_error":
		return 401
	case "permission_error":
		return 403
	case "not_found_error":
		return 404
	case "request_too_large":
		return 413
	default:
		return 500
	}
}

func parseAPIError(status int, body []byte, header http.Header) *APIError {
	apiErr := &APIError{Status: status, Message: strings.TrimSpace(string(body))}
	var payload struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &payload) == nil && payload.Error.Type != "" {
		apiErr.Type = payload.Error.Type
		apiErr.Message = payload.Error.Message
	}
	if secs, err := strconv.Atoi(header.Get("retry-after")); err == nil && secs > 0 {
		apiErr.RetryAfter = time.Duration(secs) * time.Second
	}
	return apiErr
}

// ============================================================================
// CONFIGURATION
// ============================================================================

// FastClaudeModel is the small model used for one-word classifications.
// Overridable via CLAUDE_FAST_MODEL (claude-3-haiku-20240307 was retired).
func FastClaudeModel() string {
	if m := os.Getenv("CLAUDE_FAST_MODEL"); m != "" {
		return m
	}
	return "claude-haiku-4-5"
}

func NewClaudeAIService() *ClaudeAIService {
	// Overridable via CLAUDE_MODEL so a model retirement can be handled
	// without a deploy (claude-sonnet-4-20250514 was retired and returned 404,
	// which silently disabled every market analysis).
	model := os.Getenv("CLAUDE_MODEL")
	if model == "" {
		model = "claude-sonnet-5-5"
	}

	baseURL := strings.TrimRight(os.Getenv("ANTHROPIC_BASE_URL"), "/")
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}

	// Streams are bounded by the caller's context and the idle watchdog, not
	// by a whole-response timeout.
	streamTransport := http.DefaultTransport.(*http.Transport).Clone()
	streamTransport.ResponseHeaderTimeout = 60 * time.Second

	return &ClaudeAIService{
		apiKey:  os.Getenv("ANTHROPIC_API_KEY"),
		baseURL: baseURL,
		model:   model,
		// Claude 5 models think before answering (adaptive thinking is on by
		// default); leave headroom so the JSON answer is never truncated.
		maxTokens:         8000,
		httpClient:        &http.Client{Timeout: 60 * time.Second},
		streamClient:      &http.Client{Transport: streamTransport},
		streamIdleTimeout: 45 * time.Second,
	}
}

// refusalFallback returns the `fallbacks` value to send for a model. The
// "default" form is only accepted by the models below; CLAUDE_REFUSAL_FALLBACK=off
// disables it.
func refusalFallback(model string) string {
	if strings.EqualFold(os.Getenv("CLAUDE_REFUSAL_FALLBACK"), "off") {
		return ""
	}
	switch model {
	case "claude-sonnet-5-5", "claude-opus-5-5", "claude-opus-5", "claude-fable-5-1":
		return "default"
	}
	return ""
}

// ============================================================================
// 1. APPEL PRINCIPAL À CLAUDE (ANALYSE CONCURRENTIELLE)
// ============================================================================

func (s *ClaudeAIService) CallClaude(ctx context.Context, prompt string) (string, error) {
	if s.apiKey == "" {
		return "", ErrNotConfigured
	}

	requestBody := ClaudeRequest{
		Model:     s.model,
		MaxTokens: s.maxTokens,
		Messages: []ClaudeMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
	}

	return s.executeRequest(ctx, requestBody)
}

// CallStructured sends one user prompt and constrains the answer to the JSON
// schema. effort trades thinking depth for latency ("low" for lookups).
func (s *ClaudeAIService) CallStructured(ctx context.Context, prompt, schema, effort string, maxTokens int) (*ClaudeResult, error) {
	if s.apiKey == "" {
		return nil, ErrNotConfigured
	}
	if maxTokens <= 0 {
		maxTokens = s.maxTokens
	}
	req := ClaudeRequest{
		Model:        s.model,
		MaxTokens:    maxTokens,
		Messages:     []ClaudeMessage{{Role: "user", Content: prompt}},
		OutputConfig: &OutputConfig{Effort: effort, Format: JSONSchemaFormat(schema)},
		Fallbacks:    refusalFallback(s.model),
	}
	return s.Do(ctx, req)
}

// ============================================================================
// 2. CATEGORISATION INTELLIGENTE (NOUVEAU)
// Appelé si le mapping statique échoue. Utilise un prompt système strict.
// ============================================================================

func (s *ClaudeAIService) CategorizeLabel(ctx context.Context, label string) (string, error) {
	if s.apiKey == "" {
		return "OTHER", ErrNotConfigured
	}

	// Prompt Système : Instructions strictes pour la catégorisation
	systemPrompt := `You are a financial transaction classifier.
	Classify the user's transaction label into exactly ONE of these categories:
	MOBILE, INTERNET, ENERGY, INSURANCE, LOAN, BANK, TRANSPORT, SUBSCRIPTION, FOOD, HOUSING, HEALTH, SHOPPING.

	Rules:
	1. If it looks like a phone bill (Sosh, Free, SFR), return MOBILE.
	2. If it looks like an internet box (Livebox, Freebox), return INTERNET.
	3. If it looks like electricity/gas (EDF, Engie), return ENERGY.
	4. If it looks like insurance (Macif, AXA, Allianz), return INSURANCE.
	5. If it looks like a loan (Credit, Pret, Mensualite), return LOAN.
	6. If it matches nothing well, return OTHER.

	IMPORTANT: Return ONLY the category name (uppercase). No other text.`

	requestBody := ClaudeRequest{
		Model:     FastClaudeModel(), // Haiku for speed & low cost
		MaxTokens: 20,                // Very short response needed
		System:    systemPrompt,
		Messages: []ClaudeMessage{
			{
				Role:    "user",
				Content: fmt.Sprintf("Label: %s", label),
			},
		},
	}

	category, err := s.executeRequest(ctx, requestBody)
	if err != nil {
		return "OTHER", err
	}

	// Clean up response (remove whitespace, potential dots)
	cleanCat := strings.ToUpper(strings.TrimSpace(category))
	cleanCat = strings.Trim(cleanCat, ".")

	return cleanCat, nil
}

// ============================================================================
// 3. APPEL MULTI-MESSAGES (SYSTEM + FEW-SHOT)
// Prompt système + exemple few-shot + situation réelle, avec choix du modèle
// et du budget de tokens.
// ============================================================================

func (s *ClaudeAIService) CallMessages(ctx context.Context, system string, messages []ClaudeMessage, model string, maxTokens int) (string, error) {
	if s.apiKey == "" {
		return "", ErrNotConfigured
	}
	if model == "" {
		model = s.model
	}
	if maxTokens <= 0 {
		maxTokens = s.maxTokens
	}

	requestBody := ClaudeRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    system,
		Messages:  messages,
	}

	return s.executeRequest(ctx, requestBody)
}

// ============================================================================
// HELPER: EXECUTE REQUEST (texte brut, compatibilité)
// ============================================================================

func (s *ClaudeAIService) executeRequest(ctx context.Context, requestBody ClaudeRequest) (string, error) {
	res, err := s.Do(ctx, requestBody)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(res.Text) == "" {
		if res.StopReason == "max_tokens" {
			return "", ErrMaxTokens
		}
		return "", fmt.Errorf("no text block in response (stop_reason: %s)", res.StopReason)
	}
	return res.Text, nil
}

// ============================================================================
// APPEL NON-STREAMÉ AVEC RETRIES
// ============================================================================

// maxTransientRetries matches the official SDKs' default.
const maxTransientRetries = 2

// Do sends a non-streaming request, retrying transient failures (429, 5xx,
// overload, network) with a short backoff while the context leaves room.
func (s *ClaudeAIService) Do(ctx context.Context, req ClaudeRequest) (*ClaudeResult, error) {
	req.Stream = false
	var lastErr error
	for attempt := 0; attempt <= maxTransientRetries; attempt++ {
		if attempt > 0 {
			wait := retryBackoff(attempt, lastErr)
			if !hasTimeFor(ctx, wait+5*time.Second) {
				break
			}
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		res, err := s.doOnce(ctx, req)
		if err == nil {
			return res, nil
		}
		lastErr = err
		if !IsTransientAIError(err) || errors.Is(err, errBuildRequest) {
			return nil, err
		}
	}
	return nil, lastErr
}

func retryBackoff(attempt int, lastErr error) time.Duration {
	var apiErr *APIError
	if errors.As(lastErr, &apiErr) && apiErr.RetryAfter > 0 {
		if apiErr.RetryAfter > 10*time.Second {
			return 10 * time.Second
		}
		return apiErr.RetryAfter
	}
	return time.Duration(attempt*attempt) * time.Second
}

// hasTimeFor reports whether the context deadline (if any) leaves at least d.
func hasTimeFor(ctx context.Context, d time.Duration) bool {
	deadline, ok := ctx.Deadline()
	return !ok || time.Until(deadline) > d
}

// errBuildRequest marks a request that could not be built (never retried).
var errBuildRequest = errors.New("claude: invalid request")

func (s *ClaudeAIService) newHTTPRequest(ctx context.Context, req ClaudeRequest) (*http.Request, error) {
	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errBuildRequest, err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", s.baseURL+"/v1/messages", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errBuildRequest, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", s.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	if req.Fallbacks != "" {
		httpReq.Header.Set("anthropic-beta", "server-side-fallback-2026-07-01")
	}
	return httpReq, nil
}

func (s *ClaudeAIService) doOnce(ctx context.Context, req ClaudeRequest) (*ClaudeResult, error) {
	started := time.Now()
	httpReq, err := s.newHTTPRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, parseAPIError(resp.StatusCode, body, resp.Header)
	}

	var claudeResp ClaudeResponse
	if err := json.Unmarshal(body, &claudeResp); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedResponse, err)
	}

	// Concatenate every "text" block, not just the first. The Claude 5 family
	// can return a leading reasoning ("thinking") block whose Text is empty, so
	// reading Content[0] alone would drop the actual answer.
	var out strings.Builder
	for _, block := range claudeResp.Content {
		if block.Type == "text" {
			out.WriteString(block.Text)
		}
	}
	res := &ClaudeResult{
		Text:         out.String(),
		StopReason:   claudeResp.StopReason,
		Model:        claudeResp.Model,
		InputTokens:  claudeResp.Usage.InputTokens,
		OutputTokens: claudeResp.Usage.OutputTokens,
	}
	s.logUsage(res, time.Since(started))

	if res.StopReason == "refusal" {
		return nil, refusalError(claudeResp.StopDetails)
	}
	return res, nil
}

func refusalError(details *StopDetails) error {
	if details != nil && details.Category != "" {
		return fmt.Errorf("%w (category: %s)", ErrRefusal, details.Category)
	}
	return ErrRefusal
}

func (s *ClaudeAIService) logUsage(res *ClaudeResult, elapsed time.Duration) {
	fmt.Printf("[Claude AI] Model: %s | Tokens: In %d / Out %d | Stop: %s | %.1fs | Cost: $%.5f\n",
		res.Model,
		res.InputTokens,
		res.OutputTokens,
		res.StopReason,
		elapsed.Seconds(),
		s.EstimateCost(res.InputTokens, res.OutputTokens),
	)
}

// ============================================================================
// STREAMING (SSE)
// ============================================================================

// StreamEvent reports progress while a streamed answer is generated.
type StreamEvent struct {
	// Kind is "thinking" when a reasoning block starts, "text" on each
	// answer chunk.
	Kind string
	// Text is the answer accumulated so far (for "text").
	Text string
}

type streamPayload struct {
	Type    string `json:"type"`
	Message struct {
		Model string `json:"model"`
		Usage struct {
			InputTokens int `json:"input_tokens"`
		} `json:"usage"`
	} `json:"message"`
	ContentBlock struct {
		Type string `json:"type"`
	} `json:"content_block"`
	Delta struct {
		Type        string       `json:"type"`
		Text        string       `json:"text"`
		StopReason  string       `json:"stop_reason"`
		StopDetails *StopDetails `json:"stop_details"`
	} `json:"delta"`
	Usage struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Stream sends a streaming request and calls onEvent as the answer is
// generated. It does not retry: the caller decides, since part of the
// answer may already have been reported.
func (s *ClaudeAIService) Stream(ctx context.Context, req ClaudeRequest, onEvent func(StreamEvent)) (*ClaudeResult, error) {
	if s.apiKey == "" {
		return nil, ErrNotConfigured
	}
	req.Stream = true
	started := time.Now()

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var stalled atomic.Bool
	idle := time.AfterFunc(s.streamIdleTimeout, func() {
		stalled.Store(true)
		cancel()
	})
	defer idle.Stop()

	httpReq, err := s.newHTTPRequest(streamCtx, req)
	if err != nil {
		return nil, err
	}
	resp, err := s.streamClient.Do(httpReq)
	if err != nil {
		if stalled.Load() {
			return nil, ErrStreamStalled
		}
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return nil, parseAPIError(resp.StatusCode, body, resp.Header)
	}

	res := &ClaudeResult{Model: req.Model}
	var text strings.Builder
	var stopDetails *StopDetails
	completed := false

	reader := bufio.NewReaderSize(resp.Body, 64<<10)
	var data strings.Builder
	for {
		line, readErr := reader.ReadString('\n')
		if len(line) > 0 {
			idle.Reset(s.streamIdleTimeout)
		}
		line = strings.TrimRight(line, "\r\n")

		switch {
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		case line == "" && data.Len() > 0:
			var ev streamPayload
			if err := json.Unmarshal([]byte(data.String()), &ev); err != nil {
				return nil, fmt.Errorf("%w: stream event: %v", ErrMalformedResponse, err)
			}
			data.Reset()

			switch ev.Type {
			case "message_start":
				if ev.Message.Model != "" {
					res.Model = ev.Message.Model
				}
				res.InputTokens = ev.Message.Usage.InputTokens
			case "content_block_start":
				if ev.ContentBlock.Type == "thinking" && onEvent != nil {
					onEvent(StreamEvent{Kind: "thinking"})
				}
			case "content_block_delta":
				if ev.Delta.Type == "text_delta" {
					text.WriteString(ev.Delta.Text)
					if onEvent != nil {
						onEvent(StreamEvent{Kind: "text", Text: text.String()})
					}
				}
			case "message_delta":
				if ev.Delta.StopReason != "" {
					res.StopReason = ev.Delta.StopReason
				}
				if ev.Delta.StopDetails != nil {
					stopDetails = ev.Delta.StopDetails
				}
				if ev.Usage.OutputTokens > 0 {
					res.OutputTokens = ev.Usage.OutputTokens
				}
			case "message_stop":
				completed = true
			case "error":
				return nil, &APIError{
					Status:  statusForStreamError(ev.Error.Type),
					Type:    ev.Error.Type,
					Message: ev.Error.Message,
				}
			}
		}

		if completed {
			break
		}
		if readErr != nil {
			if stalled.Load() {
				return nil, ErrStreamStalled
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if readErr == io.EOF {
				return nil, fmt.Errorf("stream ended before message_stop: %w", io.ErrUnexpectedEOF)
			}
			return nil, fmt.Errorf("stream read failed: %w", readErr)
		}
	}

	res.Text = text.String()
	s.logUsage(res, time.Since(started))
	if res.StopReason == "refusal" {
		return nil, refusalError(stopDetails)
	}
	return res, nil
}

// ============================================================================
// ESTIMATION DES COÛTS
// ============================================================================

// Pricing of the default model (logging estimate only)
const (
	InputTokenPrice  = 0.000002 // $2 per million (Claude Sonnet 5.5)
	OutputTokenPrice = 0.000010 // $10 per million (Claude Sonnet 5.5)
)

func (s *ClaudeAIService) EstimateCost(inputTokens int, outputTokens int) float64 {
	inputCost := float64(inputTokens) * InputTokenPrice
	outputCost := float64(outputTokens) * OutputTokenPrice
	return inputCost + outputCost
}

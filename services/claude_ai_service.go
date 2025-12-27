package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ============================================================================
// CLAUDE AI SERVICE - Pour recherche de concurrents détaillée
// Utilise Claude Sonnet 3.5 (le plus intelligent pour analyses complexes)
// ============================================================================

type ClaudeAIService struct {
	apiKey     string
	model      string
	maxTokens  int
	httpClient *http.Client
}

type ClaudeRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	System    string          `json:"system,omitempty"` // Added System prompt support
	Messages  []ClaudeMessage `json:"messages"`
}

type ClaudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ClaudeResponse struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Role       string `json:"role"`
	Content    []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Model      string `json:"model"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func NewClaudeAIService() *ClaudeAIService {
	// Fallback to a valid model if env var is missing or incorrect
	model := "claude-3-5-sonnet-latest"
	
	return &ClaudeAIService{
		apiKey:     os.Getenv("ANTHROPIC_API_KEY"),
		model:      model,
		maxTokens:  2000,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// ============================================================================
// 1. APPEL PRINCIPAL À CLAUDE (ANALYSE CONCURRENTIELLE)
// ============================================================================

func (s *ClaudeAIService) CallClaude(ctx context.Context, prompt string) (string, error) {
	if s.apiKey == "" {
		return "", fmt.Errorf("ANTHROPIC_API_KEY not set")
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

// ============================================================================
// 2. CATEGORISATION INTELLIGENTE (NOUVEAU)
// Appelé si le mapping statique échoue. Utilise un prompt système strict.
// ============================================================================

func (s *ClaudeAIService) CategorizeLabel(ctx context.Context, label string) (string, error) {
	if s.apiKey == "" {
		return "OTHER", fmt.Errorf("ANTHROPIC_API_KEY not set")
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
		Model:     "claude-3-haiku-20240307", // Use Haiku for speed & low cost
		MaxTokens: 20,                       // Very short response needed
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
// HELPER: EXECUTE REQUEST
// ============================================================================

func (s *ClaudeAIService) executeRequest(ctx context.Context, requestBody ClaudeRequest) (string, error) {
	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		"POST",
		"https://api.anthropic.com/v1/messages",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", s.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var claudeResp ClaudeResponse
	if err := json.Unmarshal(body, &claudeResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(claudeResp.Content) == 0 {
		return "", fmt.Errorf("empty response from Claude")
	}

	// Log usage stats
	fmt.Printf("[Claude AI] Model: %s | Tokens: In %d / Out %d | Cost: $%.5f\n",
		claudeResp.Model,
		claudeResp.Usage.InputTokens,
		claudeResp.Usage.OutputTokens,
		s.EstimateCost(claudeResp.Usage.InputTokens, claudeResp.Usage.OutputTokens),
	)

	return claudeResp.Content[0].Text, nil
}

// ============================================================================
// ESTIMATION DES COÛTS
// ============================================================================

// Pricing (approximate for Claude 3.5 Sonnet)
const (
	InputTokenPrice  = 0.000003 // $3 per million
	OutputTokenPrice = 0.000015 // $15 per million
)

func (s *ClaudeAIService) EstimateCost(inputTokens int, outputTokens int) float64 {
	inputCost := float64(inputTokens) * InputTokenPrice
	outputCost := float64(outputTokens) * OutputTokenPrice
	return inputCost + outputCost
}
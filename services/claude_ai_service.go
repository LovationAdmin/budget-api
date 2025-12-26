package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// ============================================================================
// CLAUDE AI SERVICE
// Service pour appeler l'API Anthropic Claude de manière optimisée
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
	Messages  []ClaudeMessage `json:"messages"`
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
	Model        string `json:"model"`
	StopReason   string `json:"stop_reason"`
	Usage        Usage  `json:"usage"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func NewClaudeAIService() *ClaudeAIService {
	return &ClaudeAIService{
		apiKey:     os.Getenv("ANTHROPIC_API_KEY"),
		model:      "claude-sonnet-4-20250514", // Sonnet 4 pour meilleur rapport qualité/prix
		maxTokens:  2000,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// ============================================================================
// APPEL PRINCIPAL À CLAUDE
// ============================================================================

func (s *ClaudeAIService) CallClaude(ctx context.Context, prompt string) (string, error) {
	
	if s.apiKey == "" {
		return "", fmt.Errorf("ANTHROPIC_API_KEY not set")
	}

	// Construire la requête
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

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Créer la requête HTTP
	req, err := http.NewRequestWithContext(
		ctx,
		"POST",
		"https://api.anthropic.com/v1/messages",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Headers requis par Anthropic
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", s.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	// Envoyer la requête
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Lire la réponse
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Vérifier le status code
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parser la réponse
	var claudeResp ClaudeResponse
	if err := json.Unmarshal(body, &claudeResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	// Extraire le texte de la réponse
	if len(claudeResp.Content) == 0 {
		return "", fmt.Errorf("empty response from Claude")
	}

	responseText := claudeResp.Content[0].Text

	// Log des tokens utilisés (pour monitoring des coûts)
	fmt.Printf("[Claude AI] Tokens used - Input: %d, Output: %d, Total: %d\n",
		claudeResp.Usage.InputTokens,
		claudeResp.Usage.OutputTokens,
		claudeResp.Usage.InputTokens+claudeResp.Usage.OutputTokens,
	)

	return responseText, nil
}

// ============================================================================
// MÉTHODE SPÉCIALISÉE POUR LA RECHERCHE DE CONCURRENTS
// ============================================================================

func (s *ClaudeAIService) SearchCompetitors(
	ctx context.Context,
	category string,
	currentMerchant string,
	currentAmount float64,
	country string,
) (string, error) {
	
	prompt := fmt.Sprintf(`En tant qu'expert du marché %s, trouve les meilleurs concurrents pour:
- Catégorie: %s
- Fournisseur actuel: %s  
- Prix actuel: %.2f€/mois

Fournis une réponse JSON avec les 3-5 meilleurs concurrents disponibles actuellement.`,
		country, category, currentMerchant, currentAmount,
	)

	return s.CallClaude(ctx, prompt)
}

// ============================================================================
// ESTIMATION DES COÛTS
// ============================================================================

// Sonnet 4 pricing (décembre 2024)
const (
	InputTokenPrice  = 0.000003  // $3 per million tokens
	OutputTokenPrice = 0.000015  // $15 per million tokens
)

func (s *ClaudeAIService) EstimateCost(inputTokens int, outputTokens int) float64 {
	inputCost := float64(inputTokens) * InputTokenPrice
	outputCost := float64(outputTokens) * OutputTokenPrice
	return inputCost + outputCost
}

// Estimation approximative basée sur la longueur du prompt
func (s *ClaudeAIService) EstimatePromptCost(prompt string) float64 {
	// Approximation: ~4 caractères = 1 token
	estimatedInputTokens := len(prompt) / 4
	// Réponse typique: ~500 tokens
	estimatedOutputTokens := 500
	
	return s.EstimateCost(estimatedInputTokens, estimatedOutputTokens)
}
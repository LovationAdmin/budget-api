package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type AICategorizer struct {
	apiKey string
	client *http.Client
}

func NewAICategorizer() *AICategorizer {
	return &AICategorizer{
		apiKey: os.Getenv("ANTHROPIC_API_KEY"), // Clé Claude
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Structures pour l'API Anthropic
type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// PredictCategory demande à Claude de classifier un libellé
func (s *AICategorizer) PredictCategory(label string) (string, error) {
	if s.apiKey == "" {
		// Fallback silencieux si pas de clé configurée
		return "OTHER", fmt.Errorf("ANTHROPIC_API_KEY not set")
	}

	prompt := fmt.Sprintf(`
    Tu es un expert bancaire. Analyse le libellé : "%s".
    Catégorise-le STRICTEMENT dans une seule de ces catégories (en majuscules) :
    - ENERGY (électricité, gaz, eau)
    - MOBILE (forfait téléphone)
    - INTERNET (box, fibre, hébergement)
    - INSURANCE (assurance)
    - BANK (frais bancaires)
    - LOAN (crédit)
    - FOOD (courses, resto)
    - LEISURE (loisirs, streaming, sport)
    - TRANSPORT (essence, péage, transport)
    - HOUSING (loyer, travaux)
    - SHOPPING (achats divers)
    - HEALTH (santé)
    - SUBSCRIPTION (abonnements divers)
    - OTHER (si incertain)

    Réponds UNIQUEMENT par le mot clé. Pas de phrase.`, label)

	reqBody := anthropicRequest{
		Model:     "claude-3-haiku-20240307", // Modèle rapide et économique
		MaxTokens: 10,
		Messages: []anthropicMessage{
			{Role: "user", Content: prompt},
		},
	}

	jsonData, _ := json.Marshal(reqBody)

	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewBuffer(jsonData))
	if err != nil {
		return "OTHER", err
	}

	req.Header.Set("x-api-key", s.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "OTHER", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "OTHER", fmt.Errorf("anthropic error %d: %s", resp.StatusCode, string(body))
	}

	var result anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "OTHER", err
	}

	if len(result.Content) > 0 {
		category := strings.TrimSpace(strings.ToUpper(result.Content[0].Text))
		return category, nil
	}

	return "OTHER", nil
}
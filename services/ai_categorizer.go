package services

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/sashabaranov/go-openai"
)

type AICategorizer struct {
	client *openai.Client
}

func NewAICategorizer() *AICategorizer {
	apiKey := os.Getenv("OPENAI_API_KEY")
	// Si pas de clé, le client sera nil, on gérera le fallback
	if apiKey == "" {
		return &AICategorizer{client: nil}
	}
	return &AICategorizer{
		client: openai.NewClient(apiKey),
	}
}

// PredictCategory demande à l'IA de classifier un libellé
func (s *AICategorizer) PredictCategory(label string) (string, error) {
	if s.client == nil {
		return "OTHER", fmt.Errorf("OPENAI_API_KEY not set")
	}

	prompt := fmt.Sprintf(`
    Tu es un expert en classification bancaire française. Analyse le libellé : "%s".
    Catégorise-le STRICTEMENT dans une seule de ces catégories (en majuscules) :
    - ENERGY (électricité, gaz, eau, fioul)
    - MOBILE (forfait téléphone)
    - INTERNET (box, fibre, adsl, hébergement web)
    - INSURANCE (assurance habitation, auto, scolaire, santé)
    - BANK (frais bancaires, agios)
    - LOAN (crédit immo, conso, leasing)
    - FOOD (courses, supermarché, resto, livraison repas)
    - LEISURE (sport, streaming, cinéma, vacances, jeux)
    - TRANSPORT (essence, péage, train, avion, vtc, transports en commun)
    - HOUSING (loyer, charges copro, bricolage, ameublement)
    - SHOPPING (vêtements, e-commerce, cadeaux)
    - HEALTH (médecin, pharmacie)
    - SUBSCRIPTION (logiciels, services récurrents divers)
    - OTHER (si impossible à déterminer)

    Réponds UNIQUEMENT par le mot clé.`, label)

	resp, err := s.client.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model: openai.GPT4oMini, // Modèle économique et rapide
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
			MaxTokens: 10,
			Temperature: 0.3, // Faible température pour être déterministe
		},
	)

	if err != nil {
		return "OTHER", err
	}

	category := strings.TrimSpace(strings.ToUpper(resp.Choices[0].Message.Content))
	return category, nil
}
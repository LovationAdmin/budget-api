package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"budget-api/models"
)

// ============================================================================
// MARKET ANALYZER SERVICE
// Analyse les charges et trouve des concurrents meilleurs marchés
// ============================================================================

type MarketAnalyzerService struct {
	DB        *sql.DB
	AIService *ClaudeAIService
}

func NewMarketAnalyzerService(db *sql.DB, aiService *ClaudeAIService) *MarketAnalyzerService {
	return &MarketAnalyzerService{
		DB:        db,
		AIService: aiService,
	}
}

// ============================================================================
// MAIN ANALYSIS FUNCTION
// ============================================================================

func (s *MarketAnalyzerService) AnalyzeCharge(
	ctx context.Context,
	category string,
	merchantName string,
	currentAmount float64,
	country string,
) (*models.MarketSuggestion, error) {
	// ⭐ Trim spaces pour éviter les bugs avec " " vs ""
	merchantName = strings.TrimSpace(merchantName)
	
	log.Printf("[MarketAnalyzer] Analyzing: category=%s, merchant=%s, amount=%.2f, country=%s",
		category, merchantName, currentAmount, country)

	// 1. Essayer de récupérer depuis le cache
	cached, err := s.getCachedSuggestion(ctx, category, country, merchantName)
	if err == nil && cached != nil {
		log.Printf("[MarketAnalyzer] ✅ Cache HIT")
		return cached, nil
	}

	// 2. Cache MISS - Appeler Claude AI
	log.Printf("[MarketAnalyzer] ⚠️  Cache MISS - Calling Claude AI...")

	competitors, err := s.searchCompetitors(ctx, category, merchantName, currentAmount, country)
	if err != nil {
		return nil, fmt.Errorf("failed to search competitors: %w", err)
	}

	// 3. Créer la suggestion
	suggestion := &models.MarketSuggestion{
		Category:     category,
		Country:      country,
		MerchantName: merchantName,
		Competitors:  competitors,
		LastUpdated:  time.Now(),
		ExpiresAt:    time.Now().Add(30*24*time.Hour + 1*time.Minute), // 30 jours + 1 minute de marge
	}

	// 4. Sauvegarder dans le cache
	if err := s.saveSuggestionToCache(ctx, suggestion); err != nil {
		log.Printf("[MarketAnalyzer] ⚠️  Failed to save to cache: %v", err)
		// Ne pas échouer l'analyse si le cache ne fonctionne pas
	}

	return suggestion, nil
}

// ============================================================================
// COMPETITOR SEARCH via Claude AI
// ============================================================================

func (s *MarketAnalyzerService) searchCompetitors(
	ctx context.Context,
	category string,
	merchantName string,
	currentAmount float64,
	country string,
) ([]models.Competitor, error) {

	// Construire le prompt
	prompt := s.buildCompetitorSearchPrompt(category, merchantName, currentAmount, country)

	// Appeler Claude AI
	response, err := s.AIService.CallClaude(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("AI call failed: %w", err)
	}

	// Parser la réponse
	competitors, err := parseCompetitorsFromResponse(response)
	if err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}

	return competitors, nil
}

// ============================================================================
// PROMPT BUILDING
// ============================================================================

func (s *MarketAnalyzerService) buildCompetitorSearchPrompt(
	category string,
	merchantName string,
	currentAmount float64,
	country string,
) string {

	// Contexte par catégorie et pays
	categoryContexts := map[string]map[string]string{
		"ENERGY": {
			"FR": "Fournisseurs d'énergie français populaires: EDF, Engie, TotalEnergies, Eni, Vattenfall, Ekwateur, Planète OUI. Prix moyen: 90-120€/mois pour un appartement.",
			"BE": "Fournisseurs d'énergie belges: Engie, Luminus, Eni, TotalEnergies, Mega, Bolt. Prix moyen: 100-130€/mois.",
		},
		"INTERNET": {
			"FR": "Fournisseurs Internet français: Orange, SFR, Free, Bouygues Telecom, RED by SFR, Sosh. Prix fibre: 20-45€/mois.",
			"BE": "Fournisseurs Internet belges: Proximus, Telenet, VOO, Orange Belgium, Scarlet. Prix moyen: 30-50€/mois.",
		},
		"MOBILE": {
			"FR": "Forfaits mobiles français: Free Mobile, RED by SFR, Sosh, B&YOU, Prixtel, La Poste Mobile. Prix: 5-20€/mois pour 50-100 Go.",
			"BE": "Forfaits mobiles belges: Proximus, Orange Belgium, BASE, Mobile Vikings, Scarlet. Prix: 10-25€/mois.",
		},
		"INSURANCE": {
			"FR": "Assurances françaises: AXA, Allianz, Macif, MAIF, Groupama, GMF, Generali. Prix habitation: 15-40€/mois.",
			"BE": "Assurances belges: AG Insurance, Ethias, Belfius, AXA Belgium, Baloise. Prix habitation: 20-50€/mois.",
		},
		"LOAN": {
			"FR": "Banques et prêts français: Boursorama, Fortuneo, Hello bank, BNP Paribas, Crédit Agricole, LCL. Taux moyens: 3-4%.",
			"BE": "Banques belges: BNP Paribas Fortis, ING, Belfius, KBC, Argenta. Taux moyens: 3.5-4.5%.",
		},
		"BANK": {
			"FR": "Banques françaises: Boursorama, Fortuneo, Hello bank, N26, Revolut, BNP Paribas.",
			"BE": "Banques belges: BNP Paribas Fortis, ING, Belfius, KBC, Argenta.",
		},
	}

	context := categoryContexts[category][country]
	if context == "" {
		context = "Fournisseurs locaux pour " + category
	}

	currentProvider := merchantName
	if currentProvider == "" {
		currentProvider = "fournisseur actuel"
	}

	prompt := fmt.Sprintf(`Tu es un expert en comparaison de services et produits en %s. Un utilisateur paie actuellement %.2f€/mois à %s pour la catégorie %s.

CONTEXTE: %s

Ta mission: Trouver 3-5 alternatives RÉELLES et ACTUELLES qui pourraient lui faire économiser de l'argent.

RÈGLES IMPORTANTES:
1. UNIQUEMENT des fournisseurs/services RÉELS qui existent en %s
2. Prix RÉALISTES basés sur les offres actuelles de fin 2024 / début 2025
3. Prioriser les meilleures économies potentielles
4. Indiquer les avantages ET inconvénients honnêtement
5. Si le prix actuel est déjà excellent, le mentionner

Réponds UNIQUEMENT en JSON valide (pas de markdown, pas de backticks), selon ce format EXACT:

{
  "competitors": [
    {
      "name": "Nom du concurrent",
      "typical_price": 39.99,
      "best_offer": "Description courte de la meilleure offre actuelle",
      "potential_savings": 120.00,
      "pros": ["Avantage 1", "Avantage 2"],
      "cons": ["Inconvénient 1", "Inconvénient 2"],
      "affiliate_link": "",
      "contact_available": true
    }
  ]
}

EXEMPLE pour INTERNET à 50€/mois:
{
  "competitors": [
    {
      "name": "Free",
      "typical_price": 29.99,
      "best_offer": "Freebox Pop - Fibre 5 Gb/s à 29.99€/mois la première année",
      "potential_savings": 240.00,
      "pros": ["Prix attractif première année", "Débit élevé", "Sans engagement"],
      "cons": ["Service client perfectible", "Prix augmente après 1 an"],
      "affiliate_link": "",
      "contact_available": false
    },
    {
      "name": "RED by SFR",
      "typical_price": 25.00,
      "best_offer": "RED Box Fibre à 25€/mois sans engagement",
      "potential_savings": 300.00,
      "pros": ["Prix fixe à vie", "Sans engagement", "Appels illimités"],
      "cons": ["Débit limité à 1 Gb/s", "Pas de TV incluse"],
      "affiliate_link": "",
      "contact_available": false
    }
  ]
}

Analyse maintenant et réponds en JSON pur (sans markdown):`,
		country, currentAmount, currentProvider, category, context, country)

	return prompt
}

// ============================================================================
// ⭐ JSON PARSING - AMÉLIORÉ POUR GÉRER LES BACKTICKS MARKDOWN
// ============================================================================

type CompetitorSearchResponse struct {
	Competitors []models.Competitor `json:"competitors"`
}

func parseCompetitorsFromResponse(content string) ([]models.Competitor, error) {
	// ⭐ NOUVEAU: Nettoyer les backticks Markdown et espaces
	content = strings.TrimSpace(content)
	
	// Enlever les blocs markdown ```json et ```
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	
	// Enlever d'éventuels backticks simples au début/fin
	content = strings.Trim(content, "`")
	content = strings.TrimSpace(content)
	
	// Log pour debug
	if len(content) > 200 {
		log.Printf("[Parser] Cleaned JSON (first 200 chars): %s...", content[:200])
	} else {
		log.Printf("[Parser] Cleaned JSON: %s", content)
	}

	// Parser le JSON
	var response CompetitorSearchResponse
	if err := json.Unmarshal([]byte(content), &response); err != nil {
		// Log l'erreur avec plus de contexte
		log.Printf("[Parser] ❌ JSON parse error: %v", err)
		log.Printf("[Parser] Problematic content: %s", content)
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	if len(response.Competitors) == 0 {
		return nil, fmt.Errorf("no competitors found in response")
	}

	log.Printf("[Parser] ✅ Successfully parsed %d competitors", len(response.Competitors))

	// Calculer les économies si pas fournies
	for i := range response.Competitors {
		if response.Competitors[i].PotentialSavings == 0 {
			// Calculer sur 12 mois
			response.Competitors[i].PotentialSavings = 0 // Sera calculé par le frontend
		}
	}

	return response.Competitors, nil
}

// ============================================================================
// CACHE MANAGEMENT
// ============================================================================

func (s *MarketAnalyzerService) getCachedSuggestion(
	ctx context.Context,
	category string,
	country string,
	merchantName string,
) (*models.MarketSuggestion, error) {

	log.Printf("[MarketAnalyzer] 🔍 Cache lookup: category=%s, country=%s, merchant=%s", category, country, merchantName)

	var query string
	var args []interface{}

	// Ensure we pass time.Now() to match App time vs DB time
	if merchantName == "" {
		query = `
			SELECT id, category, country, merchant_name, competitors, last_updated, expires_at
			FROM market_suggestions
			WHERE category = $1 
			  AND country = $2 
			  AND merchant_name IS NULL
			  AND expires_at > $3
			ORDER BY last_updated DESC
			LIMIT 1
		`
		args = []interface{}{category, country, time.Now()}
	} else {
		query = `
			SELECT id, category, country, merchant_name, competitors, last_updated, expires_at
			FROM market_suggestions
			WHERE category = $1 
			  AND country = $2 
			  AND merchant_name = $3
			  AND expires_at > $4
			ORDER BY last_updated DESC
			LIMIT 1
		`
		args = []interface{}{category, country, merchantName, time.Now()}
	}

	var suggestion models.MarketSuggestion
	var competitorsJSON []byte
    // FIX: Use NullString to handle potential NULLs from the DB
	var dbMerchantName sql.NullString 

    // FIX: Scan into dbMerchantName instead of &suggestion.MerchantName directly
	err := s.DB.QueryRowContext(ctx, query, args...).Scan(
		&suggestion.ID,
		&suggestion.Category,
		&suggestion.Country,
		&dbMerchantName, // <--- Change this line
		&competitorsJSON,
		&suggestion.LastUpdated,
		&suggestion.ExpiresAt,
	)

	if err == sql.ErrNoRows {
		// Log removed to reduce noise
		return nil, fmt.Errorf("not found in cache")
	}
	if err != nil {
        // This log will help verify if any other errors persist
		log.Printf("[MarketAnalyzer] ❌ Scan Error: %v", err)
		return nil, fmt.Errorf("cache query failed: %w", err)
	}

    // FIX: Convert NullString back to normal string
	if dbMerchantName.Valid {
		suggestion.MerchantName = dbMerchantName.String
	} else {
		suggestion.MerchantName = ""
	}

	if err := json.Unmarshal(competitorsJSON, &suggestion.Competitors); err != nil {
		return nil, fmt.Errorf("failed to unmarshal competitors: %w", err)
	}

	return &suggestion, nil
}

func (s *MarketAnalyzerService) saveSuggestionToCache(
	ctx context.Context,
	suggestion *models.MarketSuggestion,
) error {

	// Sérialiser les competitors en JSON
	competitorsJSON, err := json.Marshal(suggestion.Competitors)
	if err != nil {
		return fmt.Errorf("failed to marshal competitors: %w", err)
	}

	merchantName := sql.NullString{}
	if suggestion.MerchantName != "" {
		merchantName.String = suggestion.MerchantName
		merchantName.Valid = true
	}

	// ⭐ ÉTAPE 1: Essayer d'insérer
	insertQuery := `
		INSERT INTO market_suggestions (category, country, merchant_name, competitors, last_updated, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT DO NOTHING
		RETURNING id
	`

	var insertedID string
	err = s.DB.QueryRowContext(ctx, insertQuery,
		suggestion.Category,
		suggestion.Country,
		merchantName,
		competitorsJSON,
		suggestion.LastUpdated,
		suggestion.ExpiresAt,
	).Scan(&insertedID)

	if err == sql.ErrNoRows {
		// Conflit - la ligne existe déjà, on update (opération normale)
		log.Printf("[MarketAnalyzer] ✅ Refreshing existing cache entry")
		
		var updateQuery string
		var updateArgs []interface{}
		
		if suggestion.MerchantName == "" {
			// Update pour suggestion générique (merchant_name IS NULL)
			updateQuery = `
				UPDATE market_suggestions 
				SET competitors = $1, last_updated = $2, expires_at = $3
				WHERE category = $4 AND country = $5 AND merchant_name IS NULL
			`
			updateArgs = []interface{}{
				competitorsJSON,
				suggestion.LastUpdated,
				suggestion.ExpiresAt,
				suggestion.Category,
				suggestion.Country,
			}
		} else {
			// Update pour suggestion merchant spécifique
			updateQuery = `
				UPDATE market_suggestions 
				SET competitors = $1, last_updated = $2, expires_at = $3
				WHERE category = $4 AND country = $5 AND merchant_name = $6
			`
			updateArgs = []interface{}{
				competitorsJSON,
				suggestion.LastUpdated,
				suggestion.ExpiresAt,
				suggestion.Category,
				suggestion.Country,
				suggestion.MerchantName,
			}
		}
		
		result, err := s.DB.ExecContext(ctx, updateQuery, updateArgs...)
		if err != nil {
			return fmt.Errorf("failed to update suggestion: %w", err)
		}
		
		rowsAffected, _ := result.RowsAffected()
		log.Printf("[MarketAnalyzer] ✅ Updated cache: %s/%s (%d rows affected)", suggestion.Category, suggestion.Country, rowsAffected)
	} else if err != nil {
		return fmt.Errorf("failed to save suggestion: %w", err)
	} else {
		log.Printf("[MarketAnalyzer] ✅ Saved to cache: %s/%s (ID: %s)", suggestion.Category, suggestion.Country, insertedID)
	}

	// ⭐ ÉTAPE 2: Vérifier immédiatement que c'est bien sauvegardé
	verifyQuery := `
		SELECT COUNT(*), MIN(expires_at), MAX(expires_at), NOW() 
		FROM market_suggestions 
		WHERE category = $1 AND country = $2
	`
	var count int
	var minExpires, maxExpires, now time.Time
	err = s.DB.QueryRowContext(ctx, verifyQuery, suggestion.Category, suggestion.Country).Scan(&count, &minExpires, &maxExpires, &now)
	if err == nil {
		log.Printf("[MarketAnalyzer] 🔍 Verification: %d entries - App time: %s, DB time: %s, expires: %s (valid: %v)", 
			count, 
			time.Now().Format("2006-01-02 15:04:05"), 
			now.Format("2006-01-02 15:04:05"),
			maxExpires.Format("2006-01-02 15:04:05"),
			maxExpires.After(time.Now()))
	}

	return nil
}

func (s *MarketAnalyzerService) CleanExpiredCache(ctx context.Context) error {
	query := `DELETE FROM market_suggestions WHERE expires_at < NOW()`

	result, err := s.DB.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to clean cache: %w", err)
	}

	rows, _ := result.RowsAffected()
	log.Printf("[MarketAnalyzer] 🧹 Cleaned %d expired cache entries", rows)

	return nil
}
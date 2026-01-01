package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/LovationAdmin/budget-api/models"
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
// CONSTANTS
// ============================================================================

const MaxCompetitors = 3 // Maximum number of suggestions to return

// ============================================================================
// CHARGE TYPE DETECTION (FOYER vs INDIVIDUEL)
// ============================================================================

type ChargeType string

const (
	ChargeTypeFoyer      ChargeType = "FOYER"      // 1 abonnement pour tout le foyer
	ChargeTypeIndividuel ChargeType = "INDIVIDUEL" // Chaque personne a son abonnement
)

func getChargeType(category string) ChargeType {
	category = strings.ToUpper(category)

	// Charges FOYER : 1 seul abonnement pour tout le monde
	foyerCategories := map[string]bool{
		"ENERGY": true, "INTERNET": true, "INSURANCE_HOME": true,
		"LOAN": true, "HOUSING": true, "BANK": true, 
		"LEISURE_STREAMING": true, "SUBSCRIPTION": true, // Often shared
	}

	// Charges INDIVIDUELLES : chaque personne a son propre abonnement
	individuelCategories := map[string]bool{
		"MOBILE": true, "INSURANCE_AUTO": true, "INSURANCE_HEALTH": true, 
		"TRANSPORT": true, "LEISURE_SPORT": true,
	}

	if foyerCategories[category] {
		return ChargeTypeFoyer
	}
	if individuelCategories[category] {
		return ChargeTypeIndividuel
	}
	return ChargeTypeFoyer
}

func getEffectiveAmount(category string, totalAmount float64, householdSize int) (float64, ChargeType) {
	chargeType := getChargeType(category)

	if chargeType == ChargeTypeIndividuel && householdSize > 1 {
		effective := totalAmount / float64(householdSize)
		log.Printf("[MarketAnalyzer] %s (INDIVIDUEL): %.2f€ / %d = %.2f€/personne",
			category, totalAmount, householdSize, effective)
		return effective, chargeType
	}

	log.Printf("[MarketAnalyzer] %s (FOYER): %.2f€ total", category, totalAmount)
	return totalAmount, chargeType
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
	householdSize int,
) (*models.MarketSuggestion, error) {
	merchantName = strings.TrimSpace(merchantName)
	effectiveAmount, chargeType := getEffectiveAmount(category, currentAmount, householdSize)

	log.Printf("[MarketAnalyzer] Analyzing: %s (Merchant: %s), %.2f€ effective, country=%s, household=%d",
		category, merchantName, effectiveAmount, country, householdSize)

	// 1. Try cache
	cached, err := s.getCachedSuggestion(ctx, category, country, merchantName)
	if err == nil && cached != nil {
		log.Printf("[MarketAnalyzer] ✅ Cache HIT")
		s.recalculateSavings(cached, effectiveAmount, householdSize, chargeType)
		s.limitToMaxCompetitors(cached)
		// ✅ FIX: Filter out current provider from cached results
		s.filterCurrentProvider(cached, merchantName)
		return cached, nil
	}

	// 2. Cache MISS - Call Claude AI
	log.Printf("[MarketAnalyzer] ⚠️ Cache MISS - Calling Claude AI...")

	competitors, err := s.searchCompetitors(ctx, category, merchantName, effectiveAmount, country, householdSize, chargeType)
	if err != nil {
		return nil, fmt.Errorf("failed to search competitors: %w", err)
	}

	// 3. Create suggestion (limited to MaxCompetitors)
	// ✅ FIX: Filter out current provider BEFORE saving/returning
	filteredCompetitors := s.filterCompetitorsList(competitors, merchantName)

	if len(filteredCompetitors) > MaxCompetitors {
		filteredCompetitors = filteredCompetitors[:MaxCompetitors]
	}

	suggestion := &models.MarketSuggestion{
		Category:     category,
		Country:      country,
		MerchantName: merchantName,
		Competitors:  filteredCompetitors,
		LastUpdated:  time.Now(),
		ExpiresAt:    time.Now().Add(30 * 24 * time.Hour),
	}

	// 4. Save to cache
	if err := s.saveSuggestionToCache(ctx, suggestion); err != nil {
		log.Printf("[MarketAnalyzer] ⚠️ Failed to save to cache: %v", err)
	}

	return suggestion, nil
}

// ✅ NEW: Helper to filter out the current provider from a list of competitors
func (s *MarketAnalyzerService) filterCompetitorsList(competitors []models.Competitor, currentMerchant string) []models.Competitor {
	if currentMerchant == "" {
		return competitors
	}
	
	var valid []models.Competitor
	normalizedCurrent := strings.ToLower(strings.TrimSpace(currentMerchant))

	for _, comp := range competitors {
		normalizedComp := strings.ToLower(strings.TrimSpace(comp.Name))
		
		// Check for exact match or containment (e.g. "Netflix" vs "Netflix Standard")
		if normalizedComp == normalizedCurrent || 
		   strings.Contains(normalizedComp, normalizedCurrent) || 
		   strings.Contains(normalizedCurrent, normalizedComp) {
			log.Printf("[MarketAnalyzer] ⚫ Filtering out current provider: %s (matches %s)", comp.Name, currentMerchant)
			continue
		}
		valid = append(valid, comp)
	}
	return valid
}

// ✅ NEW: Helper to filter out current provider from an existing Suggestion object (for cache hits)
func (s *MarketAnalyzerService) filterCurrentProvider(suggestion *models.MarketSuggestion, currentMerchant string) {
	suggestion.Competitors = s.filterCompetitorsList(suggestion.Competitors, currentMerchant)
}

// limitToMaxCompetitors ensures we never return more than MaxCompetitors
func (s *MarketAnalyzerService) limitToMaxCompetitors(suggestion *models.MarketSuggestion) {
	if len(suggestion.Competitors) > MaxCompetitors {
		suggestion.Competitors = suggestion.Competitors[:MaxCompetitors]
	}
}

// recalculateSavings adjusts potential savings based on effective amount
func (s *MarketAnalyzerService) recalculateSavings(
	suggestion *models.MarketSuggestion,
	effectiveAmount float64,
	householdSize int,
	chargeType ChargeType,
) {
	for i := range suggestion.Competitors {
		c := &suggestion.Competitors[i]
		savingsPerUnit := (effectiveAmount - c.TypicalPrice) * 12

		if chargeType == ChargeTypeIndividuel && householdSize > 1 {
			c.PotentialSavings = savingsPerUnit * float64(householdSize)
		} else {
			c.PotentialSavings = savingsPerUnit
		}

		if c.PotentialSavings < 0 {
			c.PotentialSavings = 0
		}
	}
	s.sortCompetitorsBySavings(suggestion)
}

func (s *MarketAnalyzerService) sortCompetitorsBySavings(suggestion *models.MarketSuggestion) {
	for i := 0; i < len(suggestion.Competitors)-1; i++ {
		for j := i + 1; j < len(suggestion.Competitors); j++ {
			if suggestion.Competitors[j].PotentialSavings > suggestion.Competitors[i].PotentialSavings {
				suggestion.Competitors[i], suggestion.Competitors[j] = suggestion.Competitors[j], suggestion.Competitors[i]
			}
		}
	}
}

// ============================================================================
// CLEAN CACHE
// ============================================================================

func (s *MarketAnalyzerService) CleanExpiredCache(ctx context.Context) error {
	result, err := s.DB.ExecContext(ctx, `DELETE FROM market_suggestions WHERE expires_at < NOW()`)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	log.Printf("[MarketAnalyzer] 🧹 Cleaned %d expired cache entries", rows)
	return nil
}

// ============================================================================
// 🆕 INVALIDATE CACHE FOR BUDGET
// ============================================================================

// InvalidateCacheForBudget invalide toutes les suggestions en cache pour un pays donné
// Appelé automatiquement quand les données d'un budget sont modifiées
func (s *MarketAnalyzerService) InvalidateCacheForBudget(ctx context.Context, country string) error {
	if country == "" {
		country = "FR" // Fallback
	}

	result, err := s.DB.ExecContext(ctx, 
		`DELETE FROM market_suggestions WHERE country = $1`, 
		country)
	
	if err != nil {
		log.Printf("[MarketAnalyzer] ❌ Failed to invalidate cache for country %s: %v", country, err)
		return err
	}

	rows, _ := result.RowsAffected()
	if rows > 0 {
		log.Printf("[MarketAnalyzer] 🗑️ Invalidated %d cache entries for country %s (budget data changed)", rows, country)
	}
	
	return nil
}

// ============================================================================
// COMPETITOR SEARCH via Claude AI
// ============================================================================

func (s *MarketAnalyzerService) searchCompetitors(
	ctx context.Context,
	category string,
	merchantName string,
	effectiveAmount float64,
	country string,
	householdSize int,
	chargeType ChargeType,
) ([]models.Competitor, error) {
	prompt := s.buildPrompt(category, merchantName, effectiveAmount, country, householdSize, chargeType)

	response, err := s.AIService.CallClaude(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("AI call failed: %w", err)
	}

	competitors, err := parseCompetitorsFromResponse(response)
	if err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}

	return competitors, nil
}

// ============================================================================
// PROMPT BUILDING - Requires URL and contact info
// ============================================================================

func (s *MarketAnalyzerService) buildPrompt(
	category string,
	merchantName string,
	effectiveAmount float64,
	country string,
	householdSize int,
	chargeType ChargeType,
) string {
	familyContext := "individu seul"
	if householdSize > 1 {
		familyContext = fmt.Sprintf("foyer de %d personnes", householdSize)
	}

	var chargeContext, priceContext string
	if chargeType == ChargeTypeIndividuel {
		chargeContext = fmt.Sprintf("Type INDIVIDUEL: %.2f€/mois PAR PERSONNE", effectiveAmount)
		priceContext = "par personne"
	} else {
		chargeContext = fmt.Sprintf("Type FOYER: %.2f€/mois TOTAL pour le foyer", effectiveAmount)
		priceContext = "pour le foyer"
	}

	// ✅ UPDATED: More precise category contexts
	categoryContext := map[string]string{
		"MOBILE":           "Forfaits mobiles avec appels/SMS illimités et data.",
		"INTERNET":         "Box internet (ADSL/Fibre).",
		"ENERGY":           "Fournisseurs d'électricité et/ou gaz.",
		"INSURANCE_AUTO":   "Assurance auto.",
		"INSURANCE_HOME":   "Assurance habitation.",
		"INSURANCE_HEALTH": "Mutuelle santé.",
		"LOAN":             "Crédits immobiliers ou consommation.",
		"LEISURE_SPORT":    "Abonnements salle de sport / fitness (Basic Fit, Fitness Park, etc).",
		"LEISURE_STREAMING": "Services de streaming vidéo/audio (Netflix, Spotify, etc).",
		"TRANSPORT":        "Abonnements transports en commun ou télépéage.",
		"HOUSING":          "Assurances ou services liés au logement (hors loyer).",
	}[strings.ToUpper(category)]

	if categoryContext == "" {
		categoryContext = "Service d'abonnement récurrent."
	}

	currentProvider := merchantName
	if currentProvider == "" {
		currentProvider = "fournisseur actuel inconnu"
	}

	return fmt.Sprintf(`Tu es un expert en comparaison de services en %s.

CONTEXTE:
- Client: %s
- Catégorie: %s
- Détails catégorie: %s
- %s
- Prix actuel: %.2f€/mois %s (chez %s)
- %s

MISSION: Trouve exactement 3 alternatives RÉELLES pour économiser.

RÈGLES OBLIGATOIRES:
1. Maximum 3 concurrents, pas plus
2. Fournisseurs RÉELS existant en %s en 2024-2025
3. Prix RÉALISTES basés sur les offres actuelles
4. ⚠️ OBLIGATOIRE: Chaque concurrent DOIT avoir:
   - "website_url": URL officielle du site web (OBLIGATOIRE)
   - "phone_number": numéro service client si disponible
   - "contact_email": email contact si disponible
5. Si le prix actuel (%.2f€) est déjà inférieur aux offres du marché, retourne {"competitors": []}
6. potential_savings = (prix_actuel - typical_price) * 12
7. IMPORTANT: Ne propose PAS le fournisseur actuel (%s) comme alternative !

Réponds UNIQUEMENT en JSON (sans markdown):
{
  "competitors": [
    {
      "name": "Nom fournisseur",
      "typical_price": 9.99,
      "best_offer": "Description courte de l'offre",
      "potential_savings": 96.00,
      "pros": ["Avantage 1", "Avantage 2"],
      "cons": ["Inconvénient 1"],
      "website_url": "https://www.fournisseur.fr",
      "phone_number": "0800 123 456",
      "contact_email": "contact@fournisseur.fr",
      "contact_available": true
    }
  ]
}`,
		country,
		familyContext,
		category,
		categoryContext,
		chargeContext,
		effectiveAmount,
		priceContext,
		currentProvider,
		categoryContext,
		country,
		effectiveAmount,
		currentProvider,
	)
}

// ============================================================================
// JSON PARSING
// ============================================================================

type CompetitorSearchResponse struct {
	Competitors []models.Competitor `json:"competitors"`
}

func parseCompetitorsFromResponse(content string) ([]models.Competitor, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var response CompetitorSearchResponse
	if err := json.Unmarshal([]byte(content), &response); err != nil {
		log.Printf("[Parser] ❌ JSON parse error: %v | Content: %s", err, content)
		return nil, err
	}

	// Filter out competitors with no savings and no website
	var valid []models.Competitor
	for _, c := range response.Competitors {
		// Must have positive savings
		if c.PotentialSavings <= 0 {
			continue
		}
		// Must have at least a website URL
		if c.AffiliateLink == "" && c.WebsiteURL == "" {
			log.Printf("[Parser] ⚠️ Skipping %s: no website URL", c.Name)
			continue
		}
		// Copy website_url to affiliate_link if needed (for frontend compatibility)
		if c.AffiliateLink == "" && c.WebsiteURL != "" {
			c.AffiliateLink = c.WebsiteURL
		}
		valid = append(valid, c)
	}

	return valid, nil
}

// ============================================================================
// CACHE MANAGEMENT
// ============================================================================

func (s *MarketAnalyzerService) getCachedSuggestion(ctx context.Context, category, country, merchantName string) (*models.MarketSuggestion, error) {
	var query string
	var args []interface{}

	if merchantName == "" {
		query = `SELECT id, category, country, merchant_name, competitors, last_updated, expires_at 
				 FROM market_suggestions 
				 WHERE category=$1 AND country=$2 AND merchant_name IS NULL AND expires_at > $3 
				 ORDER BY last_updated DESC LIMIT 1`
		args = []interface{}{category, country, time.Now()}
	} else {
		query = `SELECT id, category, country, merchant_name, competitors, last_updated, expires_at 
				 FROM market_suggestions 
				 WHERE category=$1 AND country=$2 AND merchant_name=$3 AND expires_at > $4 
				 ORDER BY last_updated DESC LIMIT 1`
		args = []interface{}{category, country, merchantName, time.Now()}
	}

	var suggestion models.MarketSuggestion
	var competitorsJSON []byte
	var dbMerchantName sql.NullString

	err := s.DB.QueryRowContext(ctx, query, args...).Scan(
		&suggestion.ID, &suggestion.Category, &suggestion.Country,
		&dbMerchantName, &competitorsJSON, &suggestion.LastUpdated, &suggestion.ExpiresAt,
	)
	if err != nil {
		return nil, err
	}

	if dbMerchantName.Valid {
		suggestion.MerchantName = dbMerchantName.String
	}

	if err := json.Unmarshal(competitorsJSON, &suggestion.Competitors); err != nil {
		return nil, err
	}

	return &suggestion, nil
}

func (s *MarketAnalyzerService) saveSuggestionToCache(ctx context.Context, suggestion *models.MarketSuggestion) error {
	competitorsJSON, err := json.Marshal(suggestion.Competitors)
	if err != nil {
		return err
	}

	merchantName := sql.NullString{String: suggestion.MerchantName, Valid: suggestion.MerchantName != ""}

	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO market_suggestions (category, country, merchant_name, competitors, last_updated, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT DO NOTHING`,
		suggestion.Category, suggestion.Country, merchantName, competitorsJSON, suggestion.LastUpdated, suggestion.ExpiresAt,
	)
	return err
}
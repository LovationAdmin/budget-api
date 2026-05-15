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

// bucketHouseholdSize collapses raw household_size into a small set of cache
// buckets so cardinality stays bounded. We bucket as 1, 2, 3, 4+ — beyond
// 4 the marginal effect on which providers are relevant is small enough that
// hitting the cache is more valuable than perfect granularity.
func bucketHouseholdSize(n int) int {
	if n < 1 {
		return 1
	}
	if n > 4 {
		return 4
	}
	return n
}

// normalizeCurrency upper-cases and falls back to EUR when empty/unset.
func normalizeCurrency(currency string) string {
	c := strings.ToUpper(strings.TrimSpace(currency))
	if c == "" {
		return "EUR"
	}
	return c
}

// normalizeCountry upper-cases and falls back to FR when empty/unset.
func normalizeCountry(country string) string {
	c := strings.ToUpper(strings.TrimSpace(country))
	if c == "" {
		return "FR"
	}
	return c
}

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
		log.Printf("[MarketAnalyzer] %s (INDIVIDUEL): %.2f / %d = %.2f/personne",
			category, totalAmount, householdSize, effective)
		return effective, chargeType
	}

	log.Printf("[MarketAnalyzer] %s (FOYER): %.2f total", category, totalAmount)
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
	currency string,
	householdSize int,
	chargeDescription string,
) (*models.MarketSuggestion, error) {
	merchantName = strings.TrimSpace(merchantName)
	country = normalizeCountry(country)
	currency = normalizeCurrency(currency)
	bucketedHH := bucketHouseholdSize(householdSize)
	effectiveAmount, chargeType := getEffectiveAmount(category, currentAmount, householdSize)

	log.Printf("[MarketAnalyzer] Analyzing: %s (Merchant: %s), %.2f %s effective, country=%s, household=%d (bucket=%d), desc='%s'",
		category, merchantName, effectiveAmount, currency, country, householdSize, bucketedHH, chargeDescription)

	// 1. CACHE STRATEGY : segmenté par (pays, devise, taille foyer, marchand)
	cached, err := s.getCachedSuggestion(ctx, category, country, currency, bucketedHH, merchantName)
	if err == nil && cached != nil {
		log.Printf("[MarketAnalyzer] ✅ Cache HIT for %s (%s/%s, hh=%d)", category, country, currency, bucketedHH)

		s.recalculateSavings(cached, effectiveAmount, householdSize, chargeType)
		s.limitToMaxCompetitors(cached)
		s.filterCurrentProvider(cached, merchantName)
		return cached, nil
	}

	// 2. CACHE MISS : Appel AI avec contexte géographique et monétaire
	log.Printf("[MarketAnalyzer] ⚠️ Cache MISS. AI Prompting for %s in %s (%s, hh=%d)...", category, country, currency, bucketedHH)

	competitors, err := s.searchCompetitors(ctx, category, merchantName, effectiveAmount, country, currency, householdSize, chargeType, chargeDescription)
	if err != nil {
		return nil, fmt.Errorf("failed to search competitors: %w", err)
	}

	// 3. Filtrage et Création de la suggestion
	filteredCompetitors := s.filterCompetitorsList(competitors, merchantName)

	if len(filteredCompetitors) > MaxCompetitors {
		filteredCompetitors = filteredCompetitors[:MaxCompetitors]
	}

	suggestion := &models.MarketSuggestion{
		Category:      category,
		Country:       country,
		Currency:      currency,
		HouseholdSize: bucketedHH,
		MerchantName:  merchantName,
		Competitors:   filteredCompetitors,
		LastUpdated:   time.Now(),
		ExpiresAt:     time.Now().Add(30 * 24 * time.Hour),
	}

	// 4. Save to cache
	if err := s.saveSuggestionToCache(ctx, suggestion); err != nil {
		log.Printf("[MarketAnalyzer] ⚠️ Failed to save to cache: %v", err)
	}

	return suggestion, nil
}

// ✅ Helper to filter out the current provider from a list of competitors
func (s *MarketAnalyzerService) filterCompetitorsList(competitors []models.Competitor, currentMerchant string) []models.Competitor {
	if currentMerchant == "" {
		return competitors
	}
	
	var valid []models.Competitor
	normalizedCurrent := strings.ToLower(strings.TrimSpace(currentMerchant))

	for _, comp := range competitors {
		normalizedComp := strings.ToLower(strings.TrimSpace(comp.Name))
		
		// Check for exact match or containment
		if normalizedComp == normalizedCurrent || 
		   (len(normalizedCurrent) > 3 && strings.Contains(normalizedComp, normalizedCurrent)) || 
		   (len(normalizedComp) > 3 && strings.Contains(normalizedCurrent, normalizedComp)) {
			log.Printf("[MarketAnalyzer] ⚫ Filtering out current provider: %s (matches %s)", comp.Name, currentMerchant)
			continue
		}
		valid = append(valid, comp)
	}
	return valid
}

// ✅ Helper to filter out current provider from an existing Suggestion object (for cache hits)
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
// CLEAN & INVALIDATE CACHE
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

// InvalidateCacheForBudget invalide toutes les suggestions en cache pour un pays donné
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
	currency string,
	householdSize int,
	chargeType ChargeType,
	chargeDescription string,
) ([]models.Competitor, error) {
	
	prompt := s.buildPrompt(category, merchantName, effectiveAmount, country, currency, householdSize, chargeType, chargeDescription)

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
// PROMPT BUILDING
// ============================================================================

func (s *MarketAnalyzerService) buildPrompt(
	category string,
	merchantName string,
	effectiveAmount float64,
	country string,
	currency string,
	householdSize int,
	chargeType ChargeType,
	chargeDescription string,
) string {
	familyContext := "individu seul"
	if householdSize > 1 {
		familyContext = fmt.Sprintf("foyer de %d personnes", householdSize)
	}

	// ✅ CORRECTION ICI : Suppression de priceContext qui était inutilisé
	var chargeContext string
	if chargeType == ChargeTypeIndividuel {
		chargeContext = fmt.Sprintf("Type INDIVIDUEL: %.2f %s/mois PAR PERSONNE", effectiveAmount, currency)
	} else {
		chargeContext = fmt.Sprintf("Type FOYER: %.2f %s/mois TOTAL pour le foyer", effectiveAmount, currency)
	}

	categoryContext := map[string]string{
		"MOBILE": 			"Forfaits mobiles avec appels/SMS illimités et data.",
		"INTERNET": 		"Box internet (ADSL/Fibre).",
		"ENERGY": 			"Fournisseurs d'électricité et/ou gaz.",
		"INSURANCE_AUTO": 	"Assurance auto.",
		"INSURANCE_HOME": 	"Assurance habitation.",
		"INSURANCE_HEALTH": "Mutuelle santé.",
		"LOAN": 			"Crédits immobiliers ou consommation.",
		"LEISURE_SPORT": 	"Abonnements salle de sport / fitness (Basic Fit, Fitness Park, etc).",
		"LEISURE_STREAMING":"Services de streaming vidéo/audio (Netflix, Spotify, etc).",
		"TRANSPORT": 		"Abonnements transports en commun ou télépéage.",
		"HOUSING": 			"Assurances ou services liés au logement (hors loyer).",
	}[strings.ToUpper(category)]

	if categoryContext == "" {
		categoryContext = "Service d'abonnement récurrent."
	}

	currentProvider := merchantName
	if currentProvider == "" {
		currentProvider = "fournisseur actuel inconnu"
	}

	userDetailsString := "Aucun détail technique fourni."
	if chargeDescription != "" {
		userDetailsString = fmt.Sprintf("DÉTAILS SPÉCIFIQUES FOURNIS PAR L'UTILISATEUR : '%s'. (Utilise impérativement ces infos pour estimer la consommation, la surface ou le type d'offre équivalente).", chargeDescription)
	}

	return fmt.Sprintf(`Tu es un expert en comparaison de services en %s.

CONTEXTE:
- Client: %s
- Catégorie: %s
- Détails catégorie: %s
- %s
- Prix actuel: %.2f %s /mois (chez %s)
- %s  <-- INFO CRITIQUE ICI

MISSION: Trouve exactement 3 alternatives RÉELLES pour économiser, disponibles en %s.

RÈGLES CRITIQUES DE COMPARAISON ("APPLES TO APPLES"):
1. Si l'utilisateur a fourni des détails (ex: "35m2", "12kVA", "Tous risques", "Netflix 4 écrans"), tes suggestions DOIVENT correspondre à ces critères techniques ou de confort.
2. Si le prix actuel semble très élevé pour les détails fournis (ex: 70€ pour 20m2), signale-le dans les "pros" des concurrents (ex: "Votre tarif actuel est 30%% au-dessus de la moyenne").
3. Si le prix actuel est bas grâce aux détails (ex: tarif social), ne propose que si tu trouves vraiment mieux.
4. Les prix doivent être exprimés en %s. Si nécessaire, convertis approximativement.

RÈGLES OBLIGATOIRES DE SORTIE:
1. Maximum 3 concurrents.
2. Fournisseurs RÉELS existant en %s.
3. ⚠️ OBLIGATOIRE: Chaque concurrent DOIT avoir:
   - "website_url": URL officielle du site web (OBLIGATOIRE)
   - "phone_number": numéro service client si disponible
   - "contact_email": email contact si disponible
4. potential_savings = (prix_actuel - typical_price) * 12.
5. IMPORTANT: Ne propose PAS le fournisseur actuel (%s) comme alternative !

Réponds UNIQUEMENT en JSON (sans markdown):
{
  "competitors": [
	{
	  "name": "Nom fournisseur",
	  "typical_price": 9.99,
	  "best_offer": "Offre équivalente aux critères fournis",
	  "potential_savings": 96.00,
	  "pros": ["Avantage technique 1", "Avantage prix 2"],
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
		effectiveAmount, currency, currentProvider,
		userDetailsString,
		country,
		currency,
		country,
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

// GetCachedSuggestion exposes the cache lookup with the full key (country,
// currency, bucketed household size, merchant). Callers must pre-normalize
// country/currency/household via normalizeCountry/normalizeCurrency/
// bucketHouseholdSize when they want to bypass the AnalyzeCharge entry point.
func (s *MarketAnalyzerService) GetCachedSuggestion(ctx context.Context, category, country, currency string, householdSize int, merchantName string) (*models.MarketSuggestion, error) {
	return s.getCachedSuggestion(ctx, category, country, currency, householdSize, merchantName)
}

func (s *MarketAnalyzerService) getCachedSuggestion(ctx context.Context, category, country, currency string, householdSize int, merchantName string) (*models.MarketSuggestion, error) {
	var query string
	var args []interface{}

	if merchantName == "" {
		query = `SELECT id, category, country, currency, household_size, merchant_name, competitors, last_updated, expires_at
				 FROM market_suggestions
				 WHERE category=$1 AND country=$2 AND currency=$3 AND household_size=$4 AND merchant_name IS NULL AND expires_at > $5
				 ORDER BY last_updated DESC LIMIT 1`
		args = []interface{}{category, country, currency, householdSize, time.Now()}
	} else {
		query = `SELECT id, category, country, currency, household_size, merchant_name, competitors, last_updated, expires_at
				 FROM market_suggestions
				 WHERE category=$1 AND country=$2 AND currency=$3 AND household_size=$4 AND merchant_name=$5 AND expires_at > $6
				 ORDER BY last_updated DESC LIMIT 1`
		args = []interface{}{category, country, currency, householdSize, merchantName, time.Now()}
	}

	var suggestion models.MarketSuggestion
	var competitorsJSON []byte
	var dbMerchantName sql.NullString

	err := s.DB.QueryRowContext(ctx, query, args...).Scan(
		&suggestion.ID, &suggestion.Category, &suggestion.Country,
		&suggestion.Currency, &suggestion.HouseholdSize,
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
		INSERT INTO market_suggestions (category, country, currency, household_size, merchant_name, competitors, last_updated, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT DO NOTHING`,
		suggestion.Category, suggestion.Country, suggestion.Currency, suggestion.HouseholdSize,
		merchantName, competitorsJSON, suggestion.LastUpdated, suggestion.ExpiresAt,
	)
	return err
}
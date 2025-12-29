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
// CHARGE TYPE DETECTION (FOYER vs INDIVIDUEL)
// ============================================================================

// ChargeType determines how to interpret the total amount
type ChargeType string

const (
	// FOYER = 1 abonnement pour tout le foyer (ex: Internet, Énergie)
	// Le montant total est comparé directement avec les offres du marché
	ChargeTypeFoyer ChargeType = "FOYER"

	// INDIVIDUEL = somme des abonnements de chaque personne (ex: Mobile)
	// Le montant doit être divisé par householdSize pour obtenir le prix/personne
	ChargeTypeIndividuel ChargeType = "INDIVIDUEL"
)

// getChargeType returns the charge type based on category
// This determines whether to divide by household size or not
func getChargeType(category string) ChargeType {
	category = strings.ToUpper(category)

	// Charges FOYER : 1 seul abonnement pour tout le monde
	foyerCategories := map[string]bool{
		"ENERGY":         true, // 1 compteur EDF pour la maison
		"INTERNET":       true, // 1 box internet partagée
		"INSURANCE_HOME": true, // 1 assurance habitation
		"LOAN":           true, // 1 crédit immobilier
		"HOUSING":        true, // Loyer
		"BANK":           true, // 1 compte bancaire famille
	}

	// Charges INDIVIDUELLES : chaque personne a son propre abonnement
	individuelCategories := map[string]bool{
		"MOBILE":           true, // Chaque personne a son forfait
		"INSURANCE_AUTO":   true, // Chaque conducteur/voiture
		"INSURANCE_HEALTH": true, // Chaque personne a sa mutuelle
		"TRANSPORT":        true, // Chaque personne a son Navigo
	}

	if foyerCategories[category] {
		return ChargeTypeFoyer
	}
	if individuelCategories[category] {
		return ChargeTypeIndividuel
	}

	// Par défaut, considérer comme FOYER (ne pas diviser)
	return ChargeTypeFoyer
}

// getEffectiveAmountPerUnit calculates the amount to compare with market offers
// For INDIVIDUEL charges, divides by household size
// For FOYER charges, returns the total amount as-is
func getEffectiveAmountPerUnit(category string, totalAmount float64, householdSize int) float64 {
	chargeType := getChargeType(category)

	if chargeType == ChargeTypeIndividuel && householdSize > 1 {
		effectiveAmount := totalAmount / float64(householdSize)
		log.Printf("[MarketAnalyzer] Category %s is INDIVIDUEL: %.2f€ / %d personnes = %.2f€/personne",
			category, totalAmount, householdSize, effectiveAmount)
		return effectiveAmount
	}

	log.Printf("[MarketAnalyzer] Category %s is FOYER: comparing total %.2f€ directly",
		category, totalAmount)
	return totalAmount
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

	// Calculer le montant effectif par unité (diviser si INDIVIDUEL)
	effectiveAmount := getEffectiveAmountPerUnit(category, currentAmount, householdSize)
	chargeType := getChargeType(category)

	log.Printf("[MarketAnalyzer] Analyzing: category=%s (%s), merchant=%s, total=%.2f€, effective=%.2f€, country=%s, household=%d",
		category, chargeType, merchantName, currentAmount, effectiveAmount, country, householdSize)

	// 1. Essayer de récupérer depuis le cache
	cached, err := s.getCachedSuggestion(ctx, category, country, merchantName)
	if err == nil && cached != nil {
		log.Printf("[MarketAnalyzer] ✅ Cache HIT")

		// Recalculer les économies potentielles basées sur le montant effectif
		s.recalculateSavings(cached, effectiveAmount, householdSize, chargeType)

		return cached, nil
	}

	// 2. Cache MISS - Appeler Claude AI
	log.Printf("[MarketAnalyzer] ⚠️  Cache MISS - Calling Claude AI...")

	competitors, err := s.searchCompetitors(ctx, category, merchantName, effectiveAmount, country, householdSize, chargeType)
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
		ExpiresAt:    time.Now().Add(30 * 24 * time.Hour), // 30 jours
	}

	// 4. Sauvegarder dans le cache
	if err := s.saveSuggestionToCache(ctx, suggestion); err != nil {
		log.Printf("[MarketAnalyzer] ⚠️  Failed to save to cache: %v", err)
	}

	return suggestion, nil
}

// recalculateSavings adjusts potential savings based on effective amount and charge type
func (s *MarketAnalyzerService) recalculateSavings(
	suggestion *models.MarketSuggestion,
	effectiveAmount float64,
	householdSize int,
	chargeType ChargeType,
) {
	for i := range suggestion.Competitors {
		competitor := &suggestion.Competitors[i]

		// Économie par unité (personne ou foyer)
		savingsPerUnit := (effectiveAmount - competitor.TypicalPrice) * 12 // Annuel

		if chargeType == ChargeTypeIndividuel && householdSize > 1 {
			// Pour les charges individuelles, l'économie totale = économie par personne * nombre de personnes
			competitor.PotentialSavings = savingsPerUnit * float64(householdSize)
		} else {
			competitor.PotentialSavings = savingsPerUnit
		}

		// Si économie négative (le concurrent est plus cher), mettre à 0
		if competitor.PotentialSavings < 0 {
			competitor.PotentialSavings = 0
		}
	}

	// Trier par économie potentielle décroissante
	s.sortCompetitorsBySavings(suggestion)
}

// sortCompetitorsBySavings sorts competitors by potential savings (highest first)
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
	query := `DELETE FROM market_suggestions WHERE expires_at < NOW()`

	result, err := s.DB.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to clean cache: %w", err)
	}

	rows, _ := result.RowsAffected()
	log.Printf("[MarketAnalyzer] 🧹 Cleaned %d expired cache entries", rows)

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

	// Construire le prompt
	prompt := s.buildCompetitorSearchPrompt(category, merchantName, effectiveAmount, country, householdSize, chargeType)

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
	effectiveAmount float64,
	country string,
	householdSize int,
	chargeType ChargeType,
) string {

	// Contexte famille
	familyContext := "individu seul"
	if householdSize > 1 {
		familyContext = fmt.Sprintf("foyer de %d personnes", householdSize)
	}

	// Contexte du type de charge
	var chargeContext string
	var priceContext string

	if chargeType == ChargeTypeIndividuel {
		chargeContext = fmt.Sprintf(`ATTENTION: Cette charge est de type INDIVIDUEL (chaque personne a son propre abonnement).
Le montant %.2f€/mois est le prix PAR PERSONNE (montant total divisé par %d personnes).
Compare avec des offres INDIVIDUELLES du marché.`, effectiveAmount, householdSize)
		priceContext = "par personne"
	} else {
		chargeContext = fmt.Sprintf(`Cette charge est de type FOYER (1 seul abonnement pour toute la famille).
Le montant %.2f€/mois est le prix TOTAL pour le foyer.
Compare avec des offres équivalentes pour un foyer.`, effectiveAmount)
		priceContext = "pour le foyer"
	}

	// Contexte par catégorie
	categoryContext := ""
	switch strings.ToUpper(category) {
	case "MOBILE":
		categoryContext = "Forfaits mobiles avec appels/SMS illimités et data. Compare les offres sans engagement."
	case "INTERNET":
		categoryContext = "Box internet (ADSL/Fibre). Prix box seule, sans TV ni mobile."
	case "ENERGY":
		categoryContext = "Fournisseurs d'électricité et/ou gaz. Tarifs réglementés ou offres de marché."
	case "INSURANCE_AUTO":
		categoryContext = "Assurance auto tous risques ou tiers. Prix moyen pour un conducteur standard."
	case "INSURANCE_HOME":
		categoryContext = "Assurance habitation (propriétaire ou locataire)."
	case "INSURANCE_HEALTH":
		categoryContext = "Mutuelle santé individuelle ou familiale."
	case "LOAN":
		categoryContext = "Crédits immobiliers ou à la consommation. Taux et conditions actuels."
	}

	currentProvider := merchantName
	if currentProvider == "" {
		currentProvider = "fournisseur actuel"
	}

	prompt := fmt.Sprintf(`Tu es un expert en comparaison de services en %s.

CONTEXTE:
- Client: %s
- Catégorie: %s
- %s
- Prix actuel: %.2f€/mois %s (chez %s)

%s

%s

Ta mission: Trouver 3-5 alternatives RÉELLES et ACTUELLES pour économiser.

RÈGLES STRICTES:
1. UNIQUEMENT des fournisseurs RÉELS qui existent en %s en 2024-2025
2. Prix RÉALISTES basés sur les offres ACTUELLES du marché
3. Prioriser les meilleures économies potentielles
4. Indiquer les avantages ET inconvénients honnêtement
5. ⚠️ Si le prix actuel (%.2f€) est DÉJÀ INFÉRIEUR aux offres du marché, retourne une liste vide ou indique "Vous avez déjà une excellente offre"
6. Calcule potential_savings = (prix_actuel - typical_price) * 12 (économie annuelle)
7. Inclure numéros de téléphone et emails de contact si disponibles publiquement

Réponds UNIQUEMENT en JSON valide (sans markdown, sans backticks), format EXACT:
{
  "competitors": [
    {
      "name": "Nom du fournisseur",
      "typical_price": 9.99,
      "best_offer": "Description de l'offre en 1 ligne",
      "potential_savings": 96.00,
      "pros": ["Avantage 1", "Avantage 2"],
      "cons": ["Inconvénient 1"],
      "phone_number": "0800 123 456",
      "contact_email": "contact@fournisseur.fr",
      "affiliate_link": "https://www.fournisseur.fr/offre",
      "contact_available": true
    }
  ]
}

Si aucune économie n'est possible (le prix actuel est déjà excellent), retourne:
{
  "competitors": []
}`,
		country,
		familyContext,
		category,
		chargeContext,
		effectiveAmount,
		priceContext,
		currentProvider,
		categoryContext,
		chargeContext,
		country,
		effectiveAmount,
	)

	return prompt
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
	content = strings.Trim(content, "`")

	var response CompetitorSearchResponse
	if err := json.Unmarshal([]byte(content), &response); err != nil {
		log.Printf("[Parser] ❌ JSON parse error: %v | Content: %s", err, content)
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	// Filtrer les concurrents avec économie <= 0
	var validCompetitors []models.Competitor
	for _, c := range response.Competitors {
		if c.PotentialSavings > 0 {
			validCompetitors = append(validCompetitors, c)
		}
	}

	return validCompetitors, nil
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
		&suggestion.ID, &suggestion.Category, &suggestion.Country, &dbMerchantName, &competitorsJSON, &suggestion.LastUpdated, &suggestion.ExpiresAt,
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
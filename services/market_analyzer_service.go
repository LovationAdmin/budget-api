package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// ============================================================================
// MARKET ANALYZER SERVICE
// Analyse les charges et trouve les meilleurs concurrents par marché national
// Utilise un cache intelligent pour minimiser les appels à Claude AI
// ============================================================================

type MarketAnalyzerService struct {
	db         *sql.DB
	aiService  *ClaudeAIService
	cacheDays  int // Durée de validité du cache
}

type Competitor struct {
	Name             string  `json:"name"`
	TypicalPrice     float64 `json:"typical_price"`
	BestOffer        string  `json:"best_offer"`
	PotentialSavings float64 `json:"potential_savings"`
	AffiliateLink    string  `json:"affiliate_link"`
	Pros             []string `json:"pros"`
	Cons             []string `json:"cons"`
	ContactAvailable bool    `json:"contact_available"`
}

type MarketSuggestion struct {
	ID               string       `json:"id"`
	Category         string       `json:"category"`
	Country          string       `json:"country"`
	MerchantName     string       `json:"merchant_name,omitempty"`
	Competitors      []Competitor `json:"competitors"`
	LastUpdated      time.Time    `json:"last_updated"`
	ExpiresAt        time.Time    `json:"expires_at"`
}

func NewMarketAnalyzerService(db *sql.DB, aiService *ClaudeAIService) *MarketAnalyzerService {
	return &MarketAnalyzerService{
		db:         db,
		aiService:  aiService,
		cacheDays:  30, // Cache de 30 jours par défaut
	}
}

// ============================================================================
// ANALYSE PRINCIPALE - Point d'entrée
// ============================================================================

func (s *MarketAnalyzerService) AnalyzeCharge(
	ctx context.Context,
	category string,
	merchantName string,
	currentAmount float64,
	userCountry string,
) (*MarketSuggestion, error) {
	
	log.Printf("[MarketAnalyzer] Analyzing: category=%s, merchant=%s, amount=%.2f, country=%s",
		category, merchantName, currentAmount, userCountry)

	// 1. Vérifier le cache
	cached, err := s.getCachedSuggestion(ctx, category, userCountry, merchantName)
	if err == nil && cached != nil {
		log.Printf("[MarketAnalyzer] ✅ Cache HIT for %s/%s", category, userCountry)
		return cached, nil
	}

	log.Printf("[MarketAnalyzer] ⚠️ Cache MISS - Calling Claude AI...")

	// 2. Appeler Claude AI pour rechercher les concurrents
	competitors, err := s.searchCompetitors(ctx, category, merchantName, currentAmount, userCountry)
	if err != nil {
		return nil, fmt.Errorf("failed to search competitors: %w", err)
	}

	// 3. Créer et stocker la suggestion
	suggestion := &MarketSuggestion{
		ID:           fmt.Sprintf("market_%s_%s_%d", category, userCountry, time.Now().Unix()),
		Category:     category,
		Country:      userCountry,
		MerchantName: merchantName,
		Competitors:  competitors,
		LastUpdated:  time.Now(),
		ExpiresAt:    time.Now().AddDate(0, 0, s.cacheDays),
	}

	// 4. Sauvegarder dans le cache
	if err := s.saveSuggestionToCache(ctx, suggestion); err != nil {
		log.Printf("[MarketAnalyzer] ⚠️ Failed to save to cache: %v", err)
	}

	return suggestion, nil
}

// ============================================================================
// RECHERCHE DE CONCURRENTS VIA CLAUDE AI
// ============================================================================

func (s *MarketAnalyzerService) searchCompetitors(
	ctx context.Context,
	category string,
	currentMerchant string,
	currentAmount float64,
	country string,
) ([]Competitor, error) {

	// Construire le prompt optimisé pour Claude
	prompt := s.buildCompetitorSearchPrompt(category, currentMerchant, currentAmount, country)

	// Appeler Claude AI
	response, err := s.aiService.CallClaude(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("claude API error: %w", err)
	}

	// Parser la réponse JSON
	var competitors []Competitor
	if err := json.Unmarshal([]byte(response), &competitors); err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}

	return competitors, nil
}

// ============================================================================
// CONSTRUCTION DU PROMPT POUR CLAUDE
// ============================================================================

func (s *MarketAnalyzerService) buildCompetitorSearchPrompt(
	category string,
	currentMerchant string,
	currentAmount float64,
	country string,
) string {
	
	countryName := s.getCountryName(country)
	categoryContext := s.getCategoryContext(category, country)

	prompt := fmt.Sprintf(`Tu es un expert en comparaison de services et produits pour le marché %s.

CONTEXTE:
- Catégorie: %s
- Fournisseur actuel: %s
- Prix actuel: %.2f€/mois
- Marché: %s (%s)

INFORMATIONS DE MARCHÉ:
%s

TÂCHE:
Trouve les 3-5 meilleurs concurrents disponibles sur le marché %s pour cette catégorie.
Pour chaque concurrent, fournis:
1. Le nom exact
2. Le prix typique mensuel
3. La meilleure offre actuelle (promotion, première année, etc.)
4. L'économie potentielle annuelle par rapport au prix actuel
5. 2-3 avantages principaux
6. 1-2 inconvénients potentiels
7. Si un service de rappel/contact est disponible

CONTRAINTES:
- Utilise UNIQUEMENT des informations vérifiables et à jour
- Priorise les offres réellement disponibles en %s
- Calcule les économies de manière réaliste
- N'invente pas de prix ou d'offres

RÉPONSE ATTENDUE (JSON strict):
[
  {
    "name": "Nom du concurrent",
    "typical_price": 25.99,
    "best_offer": "Description de la meilleure offre",
    "potential_savings": 240.00,
    "affiliate_link": "",
    "pros": ["Avantage 1", "Avantage 2"],
    "cons": ["Inconvénient 1"],
    "contact_available": true
  }
]

Réponds UNIQUEMENT avec le JSON, sans texte avant ou après.`,
		countryName,
		category,
		currentMerchant,
		currentAmount,
		country,
		countryName,
		categoryContext,
		countryName,
		time.Now().Format("2006"),
	)

	return prompt
}

// ============================================================================
// CONTEXTES PAR CATÉGORIE ET PAYS
// ============================================================================

func (s *MarketAnalyzerService) getCategoryContext(category string, country string) string {
	contexts := map[string]map[string]string{
		"ENERGY": {
			"FR": `En France, le marché de l'énergie est libéralisé depuis 2007. Principaux acteurs:
- EDF: Opérateur historique, tarif réglementé disponible
- Engie, TotalEnergies, Eni: Grands fournisseurs alternatifs
- Mint Energie, Ohm Energie, Ekwateur: Fournisseurs verts/discount
Prix moyen: 90-150€/mois pour un foyer`,
			
			"BE": `En Belgique, marché libéralisé avec trois régions distinctes.
- Engie Electrabel: Opérateur historique
- Luminus, Mega, Bolt: Alternatifs compétitifs
Prix moyen: 100-180€/mois`,
		},
		
		"INTERNET": {
			"FR": `Marché français de la fibre très concurrentiel.
- Free, Orange, SFR, Bouygues: Les 4 grands opérateurs
- Red by SFR, Sosh: Offres low-cost
- OVH, K-Net: Opérateurs régionaux
Prix typique: 20-45€/mois pour fibre 1Gb/s`,
			
			"BE": `Belgique avec plusieurs opérateurs selon la région.
- Proximus, VOO, Telenet: Principaux acteurs
Prix moyen: 40-70€/mois`,
		},
		
		"MOBILE": {
			"FR": `Marché mobile français ultra-compétitif.
- Free Mobile: Pionnier du low-cost (10-20€)
- Sosh, Red by SFR, B&You: Marques sans engagement
- Prixtel, Réglo Mobile: MVNOs compétitifs
Prix moyen: 10-25€/mois pour 50-100Go`,
			
			"BE": `Marché belge avec MVNOs actifs.
- Orange, Proximus, Base: Opérateurs principaux
- Mobile Vikings, EDPnet: Alternatifs
Prix moyen: 15-30€/mois`,
		},
		
		"INSURANCE": {
			"FR": `Assurance habitation en France très segmentée.
- Groupama, MAIF, MACSF: Mutuelles
- Allianz, AXA: Assureurs traditionnels
- Luko, Acheel: Néo-assureurs digitaux
Prix moyen: 200-400€/an selon logement`,
			
			"BE": `Assurance habitation belge.
- AG Insurance, Ethias, Baloise: Leaders
Prix moyen: 250-500€/an`,
		},
	}

	if catMap, exists := contexts[category]; exists {
		if context, exists := catMap[country]; exists {
			return context
		}
	}

	return fmt.Sprintf("Marché %s pour la catégorie %s - données limitées, utilise des informations générales.", country, category)
}

func (s *MarketAnalyzerService) getCountryName(code string) string {
	countries := map[string]string{
		"FR": "français",
		"BE": "belge",
		"ES": "espagnol",
		"DE": "allemand",
		"IT": "italien",
		"NL": "néerlandais",
		"PT": "portugais",
	}
	
	if name, exists := countries[code]; exists {
		return name
	}
	return code
}

// ============================================================================
// GESTION DU CACHE
// ============================================================================

func (s *MarketAnalyzerService) getCachedSuggestion(
	ctx context.Context,
	category string,
	country string,
	merchantName string,
) (*MarketSuggestion, error) {

	query := `
		SELECT id, category, country, merchant_name, competitors, last_updated, expires_at
		FROM market_suggestions
		WHERE category = $1 
		  AND country = $2
		  AND (merchant_name = $3 OR merchant_name IS NULL)
		  AND expires_at > NOW()
		ORDER BY 
		  CASE WHEN merchant_name = $3 THEN 0 ELSE 1 END,
		  last_updated DESC
		LIMIT 1
	`

	var suggestion MarketSuggestion
	var competitorsJSON []byte
	var merchantNameDB sql.NullString

	err := s.db.QueryRowContext(ctx, query, category, country, merchantName).Scan(
		&suggestion.ID,
		&suggestion.Category,
		&suggestion.Country,
		&merchantNameDB,
		&competitorsJSON,
		&suggestion.LastUpdated,
		&suggestion.ExpiresAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil // Pas de cache
	}
	if err != nil {
		return nil, err
	}

	// Parser les competitors
	if err := json.Unmarshal(competitorsJSON, &suggestion.Competitors); err != nil {
		return nil, fmt.Errorf("invalid cached data: %w", err)
	}

	if merchantNameDB.Valid {
		suggestion.MerchantName = merchantNameDB.String
	}

	return &suggestion, nil
}

func (s *MarketAnalyzerService) saveSuggestionToCache(
	ctx context.Context,
	suggestion *MarketSuggestion,
) error {

	competitorsJSON, err := json.Marshal(suggestion.Competitors)
	if err != nil {
		return fmt.Errorf("failed to marshal competitors: %w", err)
	}

	query := `
		INSERT INTO market_suggestions (
			id, category, country, merchant_name, competitors, last_updated, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (category, country, merchant_name) 
		DO UPDATE SET 
			competitors = EXCLUDED.competitors,
			last_updated = EXCLUDED.last_updated,
			expires_at = EXCLUDED.expires_at
	`

	var merchantName interface{} = sql.NullString{Valid: false}
	if suggestion.MerchantName != "" {
		merchantName = suggestion.MerchantName
	}

	_, err = s.db.ExecContext(ctx, query,
		suggestion.ID,
		suggestion.Category,
		suggestion.Country,
		merchantName,
		competitorsJSON,
		suggestion.LastUpdated,
		suggestion.ExpiresAt,
	)

	if err != nil {
		return fmt.Errorf("failed to save suggestion: %w", err)
	}

	log.Printf("[MarketAnalyzer] ✅ Saved to cache: %s/%s (expires: %s)",
		suggestion.Category, suggestion.Country, suggestion.ExpiresAt.Format("2006-01-02"))

	return nil
}

// ============================================================================
// NETTOYAGE DU CACHE EXPIRÉ (à appeler périodiquement)
// ============================================================================

func (s *MarketAnalyzerService) CleanExpiredCache(ctx context.Context) error {
	query := `DELETE FROM market_suggestions WHERE expires_at < NOW()`
	
	result, err := s.db.ExecContext(ctx, query)
	if err != nil {
		return err
	}

	deleted, _ := result.RowsAffected()
	log.Printf("[MarketAnalyzer] 🧹 Cleaned %d expired cache entries", deleted)
	
	return nil
}
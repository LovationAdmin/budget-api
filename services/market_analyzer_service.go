package services

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	effort    string
}

func NewMarketAnalyzerService(db *sql.DB, aiService *ClaudeAIService) *MarketAnalyzerService {
	// A lookup of three offers needs little reasoning: "low" effort answers in
	// a few seconds instead of 5-15 s at the API default. MARKET_EFFORT overrides.
	effort := os.Getenv("MARKET_EFFORT")
	if effort == "" {
		effort = "low"
	}
	return &MarketAnalyzerService{
		DB:        db,
		AIService: aiService,
		effort:    effort,
	}
}

// ============================================================================
// CONSTANTS
// ============================================================================

const MaxCompetitors = 3 // Maximum number of suggestions to return

// marketMaxTokens covers the (low-effort) thinking plus three offers.
const marketMaxTokens = 6000

// marketAICallTimeout bounds one competitor search, independently of the
// request that triggered it (the result is cached for everyone).
const marketAICallTimeout = 60 * time.Second

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

// maxDescriptionRunes bounds the user-provided details sent to the model.
const maxDescriptionRunes = 200

// normalizeDescription lower-cases, collapses whitespace and truncates the
// user-provided details so equivalent inputs share a cache entry.
func normalizeDescription(desc string) string {
	d := strings.Join(strings.Fields(strings.ToLower(desc)), " ")
	if r := []rune(d); len(r) > maxDescriptionRunes {
		d = string(r[:maxDescriptionRunes])
	}
	return d
}

// cacheVariant segments the cache by price band (25% wide) and by the
// user-provided details. Competitor prices are estimated for an equivalent
// consumption, so offers found for a 400 €/month energy bill must not answer
// a 60 € one, nor offers for "80m², chauffage élec" a studio.
func cacheVariant(effectiveAmount float64, description string) string {
	band := 0
	if effectiveAmount > 0 {
		band = int(math.Floor(math.Log(effectiveAmount) / math.Log(1.25)))
	}
	v := "p" + strconv.Itoa(band)
	if description != "" {
		sum := sha256.Sum256([]byte(description))
		v += "-" + hex.EncodeToString(sum[:4])
	}
	return v
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

	// Charges INDIVIDUELLES : chaque personne a son propre abonnement.
	// Tout le reste (énergie, box, assurance habitation, prêt, streaming…)
	// est un abonnement unique pour le foyer.
	individuelCategories := map[string]bool{
		"MOBILE": true, "INSURANCE_AUTO": true, "INSURANCE_HEALTH": true,
		"TRANSPORT": true, "LEISURE_SPORT": true,
	}

	if individuelCategories[category] {
		return ChargeTypeIndividuel
	}
	return ChargeTypeFoyer
}

func getEffectiveAmount(category string, totalAmount float64, householdSize int) (float64, ChargeType) {
	chargeType := getChargeType(category)
	if chargeType == ChargeTypeIndividuel && householdSize > 1 {
		return totalAmount / float64(householdSize), chargeType
	}
	return totalAmount, chargeType
}

// ============================================================================
// MAIN ANALYSIS FUNCTION
// ============================================================================

// ChargeAnalysis is one charge to compare with the market.
type ChargeAnalysis struct {
	Category      string
	MerchantName  string
	Amount        float64 // monthly, whole household
	Country       string
	Currency      string
	HouseholdSize int
	Description   string
}

// AnalyzeCharge returns cheaper alternatives for a charge (see Analyze).
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
	suggestion, _, err := s.Analyze(ctx, ChargeAnalysis{
		Category:      category,
		MerchantName:  merchantName,
		Amount:        currentAmount,
		Country:       country,
		Currency:      currency,
		HouseholdSize: householdSize,
		Description:   chargeDescription,
	})
	return suggestion, err
}

// Analyze returns up to MaxCompetitors cheaper alternatives for a charge,
// best savings first, and whether the competitor list came from the cache.
// Savings are always computed here for this household's exact amount — never
// taken from the model (its arithmetic and per-person vs household basis are
// unreliable) nor from the household that filled the cache entry.
func (s *MarketAnalyzerService) Analyze(ctx context.Context, req ChargeAnalysis) (*models.MarketSuggestion, bool, error) {
	category := strings.ToUpper(strings.TrimSpace(req.Category))
	merchantName := strings.TrimSpace(req.MerchantName)
	country := normalizeCountry(req.Country)
	currency := normalizeCurrency(req.Currency)
	householdSize := req.HouseholdSize
	if householdSize < 1 {
		householdSize = 1
	}
	bucketedHH := bucketHouseholdSize(householdSize)
	description := normalizeDescription(req.Description)
	effectiveAmount, chargeType := getEffectiveAmount(category, req.Amount, householdSize)
	variant := cacheVariant(effectiveAmount, description)

	log.Printf("[MarketAnalyzer] Analyzing: %s (%s), country=%s/%s, household=%d (bucket=%d), variant=%s",
		category, chargeType, country, currency, householdSize, bucketedHH, variant)

	// 1. CACHE : segmenté par (pays, devise, taille foyer, marchand, variante)
	suggestion, err := s.getCachedSuggestion(ctx, category, country, currency, bucketedHH, merchantName, variant)
	fromCache := err == nil && suggestion != nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		log.Printf("[MarketAnalyzer] ⚠️ Cache lookup failed, asking the AI: %v", err)
	}

	if fromCache {
		log.Printf("[MarketAnalyzer] ✅ Cache HIT for %s (%s/%s, hh=%d, %s)", category, country, currency, bucketedHH, variant)
	} else {
		// 2. CACHE MISS : un seul appel IA par clé, même si plusieurs analyses
		// (onglets, membres du foyer) la demandent en même temps.
		log.Printf("[MarketAnalyzer] ⚠️ Cache MISS. AI Prompting for %s in %s (%s, hh=%d)...", category, country, currency, bucketedHH)
		key := strings.Join([]string{category, country, currency, strconv.Itoa(bucketedHH), strings.ToLower(merchantName), variant}, "|")
		competitors, err := fetchOnce(ctx, key, func(fetchCtx context.Context) ([]models.Competitor, error) {
			competitors, err := s.searchCompetitors(fetchCtx, category, merchantName, effectiveAmount, country, currency, householdSize, chargeType, description)
			if err != nil {
				return nil, err
			}
			competitors = s.filterCompetitorsList(competitors, merchantName)
			if len(competitors) > MaxCompetitors {
				competitors = competitors[:MaxCompetitors]
			}
			entry := newSuggestion(category, country, currency, bucketedHH, merchantName, competitors)
			if err := s.saveSuggestionToCache(fetchCtx, entry, variant); err != nil {
				log.Printf("[MarketAnalyzer] ⚠️ Failed to save to cache: %v", err)
			}
			return competitors, nil
		})
		if err != nil {
			return nil, false, fmt.Errorf("failed to search competitors: %w", err)
		}
		suggestion = newSuggestion(category, country, currency, bucketedHH, merchantName, competitors)
	}

	// 3. Économies pour CE foyer, puis seulement les offres moins chères.
	s.filterCurrentProvider(suggestion, merchantName)
	s.PriceForHousehold(suggestion, req.Amount, householdSize)
	return suggestion, fromCache, nil
}

// cacheTTL is how long a competitor list stays valid.
const cacheTTL = 30 * 24 * time.Hour

func newSuggestion(category, country, currency string, bucketedHH int, merchantName string, competitors []models.Competitor) *models.MarketSuggestion {
	return &models.MarketSuggestion{
		Category:      category,
		Country:       country,
		Currency:      currency,
		HouseholdSize: bucketedHH,
		MerchantName:  merchantName,
		Competitors:   competitors,
		LastUpdated:   time.Now(),
		ExpiresAt:     time.Now().Add(cacheTTL),
	}
}

// PriceForHousehold sets each offer's yearly savings for this household
// (amount: monthly, whole household) and keeps the cheaper offers, best first.
func (s *MarketAnalyzerService) PriceForHousehold(suggestion *models.MarketSuggestion, amount float64, householdSize int) {
	if householdSize < 1 {
		householdSize = 1
	}
	effectiveAmount, chargeType := getEffectiveAmount(suggestion.Category, amount, householdSize)
	s.recalculateSavings(suggestion, effectiveAmount, householdSize, chargeType)
	cheaper := []models.Competitor{}
	for _, c := range suggestion.Competitors {
		if c.PotentialSavings > 0 {
			cheaper = append(cheaper, c)
		}
	}
	suggestion.Competitors = cheaper
	s.limitToMaxCompetitors(suggestion)
}

// ============================================================================
// DÉDOUBLONNAGE DES APPELS IA EN COURS
// ============================================================================

type inflightSearch struct {
	done        chan struct{}
	competitors []models.Competitor
	err         error
}

var (
	inflightMu       sync.Mutex
	inflightSearches = map[string]*inflightSearch{}
)

// fetchOnce runs fetch once per key at a time: concurrent callers with the
// same key wait for the running call and share its result. The call runs
// detached from the caller's cancellation, so its (already paid) result
// still reaches the cache if the user leaves.
func fetchOnce(ctx context.Context, key string, fetch func(context.Context) ([]models.Competitor, error)) ([]models.Competitor, error) {
	inflightMu.Lock()
	if call, ok := inflightSearches[key]; ok {
		inflightMu.Unlock()
		select {
		case <-call.done:
			return cloneCompetitors(call.competitors), call.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	call := &inflightSearch{done: make(chan struct{})}
	inflightSearches[key] = call
	inflightMu.Unlock()

	go func() {
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), marketAICallTimeout)
		defer cancel()
		// Always release waiters, even if fetch panics (this goroutine is
		// outside gin's recovery: a panic would otherwise crash the server).
		defer func() {
			if r := recover(); r != nil {
				call.competitors, call.err = nil, fmt.Errorf("competitor search panicked: %v", r)
			}
			inflightMu.Lock()
			delete(inflightSearches, key)
			inflightMu.Unlock()
			close(call.done)
		}()
		call.competitors, call.err = fetch(fetchCtx)
	}()

	select {
	case <-call.done:
		return cloneCompetitors(call.competitors), call.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func cloneCompetitors(in []models.Competitor) []models.Competitor {
	if in == nil {
		return []models.Competitor{}
	}
	out := make([]models.Competitor, len(in))
	copy(out, in)
	return out
}

// ✅ Helper to filter out the current provider from a list of competitors
func (s *MarketAnalyzerService) filterCompetitorsList(competitors []models.Competitor, currentMerchant string) []models.Competitor {
	if currentMerchant == "" {
		return competitors
	}

	valid := []models.Competitor{}
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

// recalculateSavings sets each competitor's yearly savings for the whole
// household from the charge's effective amount, and sorts best first.
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

		c.PotentialSavings = math.Round(c.PotentialSavings*100) / 100
		if c.PotentialSavings < 0 {
			c.PotentialSavings = 0
		}
	}
	s.sortCompetitorsBySavings(suggestion)
}

func (s *MarketAnalyzerService) sortCompetitorsBySavings(suggestion *models.MarketSuggestion) {
	sort.SliceStable(suggestion.Competitors, func(i, j int) bool {
		return suggestion.Competitors[i].PotentialSavings > suggestion.Competitors[j].PotentialSavings
	})
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

// competitorsSchema constrains the answer (structured outputs). Savings are
// not asked for: they are computed server-side.
const competitorsSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["competitors"],
  "properties": {
    "competitors": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["name", "typical_price", "best_offer", "pros", "cons", "website_url"],
        "properties": {
          "name": { "type": "string" },
          "typical_price": { "type": "number" },
          "best_offer": { "type": "string" },
          "pros": { "type": "array", "items": { "type": "string" } },
          "cons": { "type": "array", "items": { "type": "string" } },
          "website_url": { "type": "string" },
          "phone_number": { "type": "string" },
          "contact_email": { "type": "string" }
        }
      }
    }
  }
}`

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

	res, err := s.AIService.CallStructured(ctx, prompt, competitorsSchema, s.effort, marketMaxTokens)
	if err != nil {
		return nil, fmt.Errorf("AI call failed: %w", err)
	}
	if res.StopReason == "max_tokens" {
		return nil, fmt.Errorf("AI call failed: %w", ErrMaxTokens)
	}

	competitors, err := parseCompetitorsFromResponse(res.Text)
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

	var chargeContext, priceBasis string
	if chargeType == ChargeTypeIndividuel {
		chargeContext = fmt.Sprintf("Type INDIVIDUEL: %.2f %s/mois PAR PERSONNE", effectiveAmount, currency)
		priceBasis = "par personne"
	} else {
		chargeContext = fmt.Sprintf("Type FOYER: %.2f %s/mois TOTAL pour le foyer", effectiveAmount, currency)
		priceBasis = "pour tout le foyer"
	}

	categoryContext := map[string]string{
		"MOBILE":            "Forfaits mobiles avec appels/SMS illimités et data.",
		"INTERNET":          "Box internet (ADSL/Fibre).",
		"ENERGY":            "Fournisseurs d'électricité et/ou gaz.",
		"INSURANCE_AUTO":    "Assurance auto.",
		"INSURANCE_HOME":    "Assurance habitation.",
		"INSURANCE_HEALTH":  "Mutuelle santé.",
		"LOAN":              "Crédits immobiliers ou consommation.",
		"LEISURE_SPORT":     "Abonnements salle de sport / fitness (Basic Fit, Fitness Park, etc).",
		"LEISURE_STREAMING": "Services de streaming vidéo/audio (Netflix, Spotify, etc).",
		"TRANSPORT":         "Abonnements transports en commun ou télépéage.",
		"HOUSING":           "Assurances ou services liés au logement (hors loyer).",
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
- %s  <-- INFO CRITIQUE ICI

MISSION: Trouve jusqu'à 3 alternatives RÉELLES et moins chères, disponibles en %s.

RÈGLES CRITIQUES DE COMPARAISON ("APPLES TO APPLES"):
1. Si l'utilisateur a fourni des détails (ex: "35m2", "12kVA", "Tous risques", "Netflix 4 écrans"), tes suggestions DOIVENT correspondre à ces critères techniques ou de confort.
2. Si le prix actuel semble très élevé pour les détails fournis (ex: 70€ pour 20m2), signale-le dans les "pros" des concurrents (ex: "Votre tarif actuel est 30%% au-dessus de la moyenne").
3. Si le prix actuel est bas grâce aux détails (ex: tarif social), ne propose que si tu trouves vraiment mieux.
4. Les prix doivent être exprimés en %s. Si nécessaire, convertis approximativement.

RÈGLES DE SORTIE:
1. Maximum 3 concurrents, du moins cher au plus cher.
2. Fournisseurs RÉELS existant en %s, avec une offre réellement souscriptible.
3. typical_price : prix mensuel en %s de l'offre équivalente, %s (même base que le prix actuel).
4. website_url : URL officielle du fournisseur (obligatoire). phone_number et contact_email : uniquement si tu es certain qu'ils sont exacts, sinon omets-les.
5. Ne propose PAS le fournisseur actuel (%s).
6. Si aucune alternative n'est réellement moins chère pour un service équivalent, renvoie une liste vide plutôt que d'inventer un prix.
7. pros, cons et best_offer en français, concis.`,
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
		currency, priceBasis,
		currentProvider,
	)
}

// ============================================================================
// JSON PARSING
// ============================================================================

type CompetitorSearchResponse struct {
	Competitors []models.Competitor `json:"competitors"`
}

// normalizeWebsite returns an absolute http(s) URL, or "" if it is not one.
func normalizeWebsite(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !strings.Contains(u.Host, ".") {
		return ""
	}
	return u.String()
}

func parseCompetitorsFromResponse(content string) ([]models.Competitor, error) {
	cleaned := extractJSONObject(content)
	if cleaned == "" {
		return nil, fmt.Errorf("no JSON object found in response")
	}

	var response CompetitorSearchResponse
	if err := json.Unmarshal([]byte(cleaned), &response); err != nil {
		log.Printf("[Parser] ❌ JSON parse error: %v", err)
		return nil, err
	}

	// Keep real, priced offers with a working website link. Savings are
	// computed later, per household.
	valid := []models.Competitor{}
	for _, c := range response.Competitors {
		c.Name = strings.TrimSpace(c.Name)
		if c.Name == "" || c.TypicalPrice <= 0 {
			continue
		}
		c.WebsiteURL = normalizeWebsite(c.WebsiteURL)
		if c.WebsiteURL == "" {
			log.Printf("[Parser] ⚠️ Skipping %s: no website URL", c.Name)
			continue
		}
		// Copy website_url to affiliate_link if needed (for frontend compatibility)
		if c.AffiliateLink == "" {
			c.AffiliateLink = c.WebsiteURL
		}
		c.ContactAvailable = c.PhoneNumber != "" || c.ContactEmail != ""
		if c.Pros == nil {
			c.Pros = []string{}
		}
		if c.Cons == nil {
			c.Cons = []string{}
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
// It returns the most recent entry of any price band / details variant.
func (s *MarketAnalyzerService) GetCachedSuggestion(ctx context.Context, category, country, currency string, householdSize int, merchantName string) (*models.MarketSuggestion, error) {
	return s.getCachedSuggestion(ctx, category, country, currency, householdSize, merchantName, "")
}

// getCachedSuggestion looks up a non-expired entry; an empty variant matches
// any variant.
func (s *MarketAnalyzerService) getCachedSuggestion(ctx context.Context, category, country, currency string, householdSize int, merchantName, variant string) (*models.MarketSuggestion, error) {
	query := `SELECT id, category, country, currency, household_size, merchant_name, competitors, last_updated, expires_at
			 FROM market_suggestions
			 WHERE category=$1 AND country=$2 AND currency=$3 AND household_size=$4 AND expires_at > $5`
	args := []interface{}{category, country, currency, householdSize, time.Now()}

	if merchantName == "" {
		query += ` AND merchant_name IS NULL`
	} else {
		args = append(args, merchantName)
		query += fmt.Sprintf(` AND merchant_name=$%d`, len(args))
	}
	if variant != "" {
		args = append(args, variant)
		query += fmt.Sprintf(` AND variant=$%d`, len(args))
	}
	query += ` ORDER BY last_updated DESC LIMIT 1`

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
	if suggestion.Competitors == nil {
		suggestion.Competitors = []models.Competitor{}
	}

	return &suggestion, nil
}

func (s *MarketAnalyzerService) saveSuggestionToCache(ctx context.Context, suggestion *models.MarketSuggestion, variant string) error {
	competitorsJSON, err := json.Marshal(suggestion.Competitors)
	if err != nil {
		return err
	}

	merchantName := sql.NullString{String: suggestion.MerchantName, Valid: suggestion.MerchantName != ""}

	// An expired entry with the same key would make the insert a no-op.
	if _, err := s.DB.ExecContext(ctx,
		`DELETE FROM market_suggestions WHERE expires_at <= NOW() AND category = $1 AND country = $2`,
		suggestion.Category, suggestion.Country,
	); err != nil {
		return err
	}

	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO market_suggestions (category, country, currency, household_size, merchant_name, variant, competitors, last_updated, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT DO NOTHING`,
		suggestion.Category, suggestion.Country, suggestion.Currency, suggestion.HouseholdSize,
		merchantName, variant, competitorsJSON, suggestion.LastUpdated, suggestion.ExpiresAt,
	)
	return err
}

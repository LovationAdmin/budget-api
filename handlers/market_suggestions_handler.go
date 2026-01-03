package handlers

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LovationAdmin/budget-api/models"
	"github.com/LovationAdmin/budget-api/services"
)

// ============================================================================
// MARKET SUGGESTIONS HANDLER
// Endpoints for obtaining personalized competitor suggestions
// ============================================================================

type MarketSuggestionsHandler struct {
	DB             *sql.DB
	MarketAnalyzer *services.MarketAnalyzerService
	AIService      *services.ClaudeAIService
	WS             *WSHandler // Added WebSocket Handler
}

// Updated Constructor to accept WSHandler
func NewMarketSuggestionsHandler(db *sql.DB, ws *WSHandler) *MarketSuggestionsHandler {
	aiService := services.NewClaudeAIService()
	marketAnalyzer := services.NewMarketAnalyzerService(db, aiService)

	return &MarketSuggestionsHandler{
		DB:             db,
		MarketAnalyzer: marketAnalyzer,
		AIService:      aiService,
		WS:             ws,
	}
}

// ✅ HELPER : Récupérer la config du budget (Source de vérité)
func (h *MarketSuggestionsHandler) getBudgetConfig(ctx context.Context, budgetID string) (string, string, error) {
	var location, currency sql.NullString
	// On récupère location et currency, avec fallback si null dans la BDD
	err := h.DB.QueryRowContext(ctx, 
		"SELECT location, currency FROM budgets WHERE id = $1", 
		budgetID).Scan(&location, &currency)
	
	loc := "FR"
	cur := "EUR"

	if err == nil {
		if location.Valid && location.String != "" { loc = location.String }
		if currency.Valid && currency.String != "" { cur = currency.String }
	}
	
	return loc, cur, err
}

// ============================================================================
// 1. ANALYZE A SPECIFIC CHARGE
// POST /api/v1/suggestions/analyze
// ============================================================================

type AnalyzeChargeRequest struct {
	// ⚠️ MODIFICATION : budget_id devient optionnel pour l'outil public
	BudgetID      string  `json:"budget_id"` 
	Category      string  `json:"category" binding:"required"`
	MerchantName  string  `json:"merchant_name"`
	CurrentAmount float64 `json:"current_amount" binding:"required"`
	HouseholdSize int     `json:"household_size"`
	Description   string  `json:"description,omitempty"`
	// ✅ AJOUTS : Pour l'outil public SmartTools (valeurs manuelles)
	Country       string  `json:"country,omitempty"`
	Currency      string  `json:"currency,omitempty"`
}

func (h *MarketSuggestionsHandler) AnalyzeCharge(c *gin.Context) {
	var req AnalyzeChargeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	var country, currency string

	// LOGIQUE DE DÉTECTION DU CONTEXTE
	if req.BudgetID != "" {
		// Cas 1 : Appel depuis un Budget (Utilisateur connecté) -> On prend la config BDD
		// On ignore les params country/currency de la requête dans ce cas pour la sécurité/cohérence
		var err error
		country, currency, err = h.getBudgetConfig(c.Request.Context(), req.BudgetID)
		if err != nil {
			log.Printf("Failed to get budget config for %s: %v", req.BudgetID, err)
			country, currency = "FR", "EUR"
		}
	} else {
		// Cas 2 : Appel Public (SmartTools) -> On prend les params de la requête
		country = req.Country
		currency = req.Currency
		
		// Fallbacks par défaut si non fournis
		if country == "" { country = "FR" }
		if currency == "" { currency = "EUR" }
	}

	householdSize := req.HouseholdSize
	if householdSize < 1 {
		householdSize = 1
	}

	log.Printf("[MarketSuggestions] Analyzing single charge: %s (Loc: %s, Curr: %s) - %.2f - Desc: %s",
		req.Category, country, currency, req.CurrentAmount, req.Description)

	suggestion, err := h.MarketAnalyzer.AnalyzeCharge(
		c.Request.Context(),
		req.Category,
		req.MerchantName,
		req.CurrentAmount,
		country,  // ✅ PAYS DÉTECTÉ
		currency, // ✅ DEVISE DÉTECTÉE
		householdSize,
		req.Description,
	)

	if err != nil {
		log.Printf("Market analysis failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to analyze charge"})
		return
	}

	c.JSON(http.StatusOK, suggestion)
}

// ============================================================================
// 2. BULK ANALYZE ALL CHARGES IN A BUDGET (ASYNC FIX)
// POST /api/v1/budgets/:id/suggestions/bulk-analyze
// ============================================================================

type ChargeToAnalyze struct {
	ID           string  `json:"id"`
	Category     string  `json:"category"`
	Label        string  `json:"label"`
	Amount       float64 `json:"amount"`
	MerchantName string  `json:"merchant_name,omitempty"`
	Description  string  `json:"description,omitempty"`
}

type BulkAnalyzeRequest struct {
	Charges       []ChargeToAnalyze `json:"charges" binding:"required"`
	HouseholdSize int               `json:"household_size"`
}

func (h *MarketSuggestionsHandler) BulkAnalyzeCharges(c *gin.Context) {
	userID := c.GetString("user_id")
	budgetID := c.Param("id")

	// 1. Check access
	hasAccess, err := h.checkBudgetAccess(c.Request.Context(), userID, budgetID)
	if err != nil || !hasAccess {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	var req BulkAnalyzeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	// 2. Respond IMMEDIATELY to prevent timeout (HTTP 202 Accepted)
	c.JSON(http.StatusAccepted, gin.H{
		"message": "Analysis started in background",
		"status":  "processing",
	})

	// 3. Launch background processing
	go func() {
		bgCtx := context.Background()

		// ✅ RECUPERATION DE LA CONFIG BUDGET AU DÉBUT DU PROCESS
		country, currency, err := h.getBudgetConfig(bgCtx, budgetID)
		if err != nil {
			log.Printf("[Async] Error: Could not fetch budget config for %s. Using defaults.", budgetID)
			country, currency = "FR", "EUR"
		}

		householdSize := req.HouseholdSize
		if householdSize < 1 {
			householdSize = 1
		}

		log.Printf("[Async] Bulk analyzing %d charges for budget %s (Loc: %s, Curr: %s)", len(req.Charges), budgetID, country, currency)

		var suggestions []models.ChargeSuggestion
		totalSavings := 0.0
		cacheHits := 0
		aiCallsMade := 0

		for _, charge := range req.Charges {
			
			// FIX: Force re-check of "LEISURE" category for existing data
			analysisCategory := charge.Category
			if charge.Category == "LEISURE" || charge.Category == "OTHER" {
				refined := determineCategory(charge.Label)
				if refined != "OTHER" && refined != "LEISURE" {
					log.Printf("[Recategorize] Upgrading '%s' from %s to %s", charge.Label, charge.Category, refined)
					analysisCategory = refined
				}
			}

			// Check whitelist
			if !h.isSuggestionRelevant(analysisCategory) {
				continue
			}

			time.Sleep(100 * time.Millisecond)

			suggestion, err := h.MarketAnalyzer.AnalyzeCharge(
				bgCtx,
				analysisCategory,
				charge.MerchantName,
				charge.Amount,
				country,  // ✅ PAYS
				currency, // ✅ DEVISE
				householdSize,
				charge.Description,
			)

			if err != nil {
				log.Printf("[Async] Failed to analyze charge %s: %v", charge.ID, err)
				continue
			}

			if len(suggestion.Competitors) > 0 {
				bestSavings := suggestion.Competitors[0].PotentialSavings
				totalSavings += bestSavings

				suggestions = append(suggestions, models.ChargeSuggestion{
					ChargeID:    charge.ID,
					ChargeLabel: charge.Label,
					Suggestion:  suggestion,
				})

				aiCallsMade++
			}
		}

		log.Printf("[Async] Analysis complete for budget %s. Found %.2f %s savings.", budgetID, totalSavings, currency)

		// 4. Notify Frontend via WebSocket
		if h.WS != nil {
			responsePayload := map[string]interface{}{
				"type": "suggestions_ready",
				"data": map[string]interface{}{
					"suggestions":             suggestions,
					"total_potential_savings": totalSavings,
					"household_size":          householdSize,
					"cache_hits":              cacheHits,
					"ai_calls_made":           aiCallsMade,
					"currency":                currency, // ✅
				},
			}
			
			h.WS.BroadcastJSON(budgetID, responsePayload)
		}
	}()
}

// ============================================================================
// 3. GET CACHED SUGGESTIONS FOR A CATEGORY
// GET /api/v1/suggestions/category/:category
// ============================================================================

func (h *MarketSuggestionsHandler) GetCategorySuggestions(c *gin.Context) {
	userID := c.GetString("user_id")
	category := c.Param("category")

	// Fallback si pas de budgetID
	userCountry, err := h.getUserCountry(c.Request.Context(), userID)
	if err != nil {
		userCountry = "FR"
	}

	suggestion, err := h.MarketAnalyzer.AnalyzeCharge(
		c.Request.Context(),
		category,
		"",
		0,
		userCountry,
		"EUR", // Default currency
		1,
		"", // Pas de description
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch suggestions"})
		return
	}

	if suggestion == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "No suggestions found"})
		return
	}

	c.JSON(http.StatusOK, suggestion)
}

// ============================================================================
// 4. CLEAN EXPIRED CACHE
// ============================================================================

func (h *MarketSuggestionsHandler) CleanExpiredCache(c *gin.Context) {
	if err := h.MarketAnalyzer.CleanExpiredCache(c.Request.Context()); err != nil {
		log.Printf("Failed to clean cache: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clean cache"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Cache cleaned successfully"})
}

// ============================================================================
// 5. CATEGORIZE CHARGE
// ============================================================================

func (h *MarketSuggestionsHandler) CategorizeCharge(c *gin.Context) {
	var req struct {
		Label string `json:"label"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Label required"})
		return
	}

	label := strings.TrimSpace(req.Label)
	if label == "" {
		c.JSON(http.StatusOK, gin.H{"label": "", "category": "OTHER"})
		return
	}

	category := determineCategory(label)

	if category == "OTHER" && len(label) > 3 {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		aiCategory, err := h.AIService.CategorizeLabel(ctx, label)
		if err == nil {
			category = aiCategory
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"label":    req.Label,
		"category": category,
	})
}

// ============================================================================
// HELPERS
// ============================================================================

func determineCategory(label string) string {
	l := strings.ToUpper(strings.TrimSpace(label))

	keywords := map[string][]string{
		"MOBILE": {
			"MOBILE", "PORTABLE", "SOSH", "BOUYGUES", "FREE", "ORANGE", "SFR",
			"RED BY", "PRIXTEL", "NRJ MOBILE", "LEBARA", "LYCA", "YOUPI", "CORIOLIS",
			"FORFAIT",
		},
		"INTERNET": {
			"BOX", "FIBRE", "ADSL", "INTERNET", "NUMERICABLE", "STARLINK",
			"NORDNET", "OVH", "K-NET", "LIVEBOX", "BBOX", "FREEBOX",
		},
		"ENERGY": {
			"EDF", "ENGIE", "TOTAL", "ENERGIE", "ELEC", "GAZ", "ENI",
			"ILEK", "PLANETE OUI", "VATTENFALL", "MINT", "OHM", "MEGA", "BUTAGAZ", "SUEZ", "VEOLIA",
		},
		"INSURANCE": {
			"ASSURANCE", "AXA", "MAIF", "ALLIANZ", "MACIF", "GROUPAMA", "GMF",
			"MATMUT", "GENERALI", "MMA", "MAAF", "DIRECT ASSURANCE", "OLIVIER",
			"LEOCARE", "LUKO", "ALAN", "MGEN", "HARMONIE", "MUTUELLE", "PREVOYANCE",
		},
		"LOAN": {
			"PRET", "CREDIT", "ECHEANCE", "EMPRUNT", "MENSUALITE", "IMMOBILIER",
			"COFIDIS", "CETELEM", "SOFINCO", "FLOA", "BOURSORAMA", "FRANFINANCE", "YOUNITED",
		},
		"BANK": {
			"BANQUE", "CREDIT AGRICOLE", "SOCIETE GENERALE", "BNP", "LCL",
			"POSTALE", "CAISSE EPARGNE", "POPULAIRE", "CIC", "REVOLUT", "N26",
			"BOURSO", "FORTUNEO", "MONABANQ", "HELLO", "QONTO", "SHINE",
		},
		"TRANSPORT": {
			"NAVIGO", "RATP", "SNCF", "TGV", "OUIGO", "UBER", "BOLT", "TAXI",
			"LIME", "AUTOROUTE", "PEAGE", "VINCI", "APRR", "SANEF", "TOTAL ENERGIES", "ESSO", "BP", "SHELL",
		},
		"SUBSCRIPTION": {
			"ICLOUD", "DROPBOX", "ADOBE", "MICROSOFT", "GOOGLE ONE", "CHATGPT", "MIDJOURNEY",
		},
		"FOOD": {
			"CARREFOUR", "LECLERC", "AUCHAN", "INTERMARCHE", "LIDL", "ALDI", "MONOPRIX",
			"FRANPRIX", "SUPER U", "HYPER U", "CASINO", "PICARD", "UBER EATS", "DELIVEROO",
			"MC DO", "MCDONALD", "BK", "BURGER KING", "KFC", "STARBUCKS",
		},
		"HOUSING": {
			"LOYER", "RENT", "APPARTEMENT", "LOGEMENT", "CHARGES LOCATIVES", "FONCIA", "CITYA",
		},
		"LEISURE_SPORT": {
			"SPORT", "GYM", "FITNESS", "BASIC FIT", "KEEP COOL", "NEONESS", "CROSSFIT", "ORANGE BLEUE",
			"CLUB", "PISCINE", "YOGA",
		},
		"LEISURE_STREAMING": {
			"NETFLIX", "SPOTIFY", "AMAZON", "PRIME", "DISNEY", "CANAL",
			"APPLE TV", "APPLE MUSIC", "YOUTUBE", "DEEZER", "HBO", "PARAMOUNT", "SALTO", "OCS", "TWITCH",
		},
	}

	for cat, keys := range keywords {
		for _, k := range keys {
			if strings.Contains(l, k) {
				if k == "SFR" || k == "ORANGE" || k == "BOUYGUES" || k == "FREE" {
					if strings.Contains(l, "BOX") || strings.Contains(l, "FIBRE") || strings.Contains(l, "FIXE") {
						return "INTERNET"
					}
					if strings.Contains(l, "MOBILE") || strings.Contains(l, "FORFAIT") {
						return "MOBILE"
					}
					return "MOBILE"
				}
				return cat
			}
		}
	}
	return "OTHER"
}

func (h *MarketSuggestionsHandler) getUserCountry(ctx context.Context, userID string) (string, error) {
	var country sql.NullString
	err := h.DB.QueryRowContext(ctx, "SELECT country FROM users WHERE id = $1", userID).Scan(&country)
	if err != nil || !country.Valid || country.String == "" {
		return "FR", nil
	}
	return country.String, nil
}

func (h *MarketSuggestionsHandler) checkBudgetAccess(ctx context.Context, userID string, budgetID string) (bool, error) {
	var exists bool
	err := h.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM budget_members WHERE budget_id = $1 AND user_id = $2)`, budgetID, userID).Scan(&exists)
	return exists, err
}

func (h *MarketSuggestionsHandler) isSuggestionRelevant(category string) bool {
	relevantCategories := map[string]bool{
		"ENERGY":            true,
		"INTERNET":          true,
		"MOBILE":            true,
		"INSURANCE":         true,
		"INSURANCE_AUTO":    true,
		"INSURANCE_HOME":    true,
		"INSURANCE_HEALTH":  true,
		"LOAN":              true,
		"BANK":              true,
		"TRANSPORT":         true,
		"LEISURE":           true, 
		"LEISURE_SPORT":     true,
		"LEISURE_STREAMING": true,
		"SUBSCRIPTION":      true,
		"HOUSING":           true,
	}
	return relevantCategories[strings.ToUpper(category)]
}
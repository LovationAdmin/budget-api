package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
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
}

func NewMarketSuggestionsHandler(db *sql.DB) *MarketSuggestionsHandler {
	aiService := services.NewClaudeAIService()
	marketAnalyzer := services.NewMarketAnalyzerService(db, aiService)

	return &MarketSuggestionsHandler{
		DB:             db,
		MarketAnalyzer: marketAnalyzer,
		AIService:      aiService,
	}
}

// ============================================================================
// 1. ANALYZE A SPECIFIC CHARGE
// POST /api/v1/suggestions/analyze
// ============================================================================

type AnalyzeChargeRequest struct {
	Category      string  `json:"category" binding:"required"`
	MerchantName  string  `json:"merchant_name"`
	CurrentAmount float64 `json:"current_amount" binding:"required"`
}

func (h *MarketSuggestionsHandler) AnalyzeCharge(c *gin.Context) {
	userID := c.GetString("user_id")

	var req AnalyzeChargeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	userCountry, err := h.getUserCountry(c.Request.Context(), userID)
	if err != nil {
		userCountry = "FR"
	}

	log.Printf("[MarketSuggestions] Analyzing single charge for user %s: %s - %.2f€", userID, req.Category, req.CurrentAmount)

	// Default household size to 1 for single charge analysis
	suggestion, err := h.MarketAnalyzer.AnalyzeCharge(
		c.Request.Context(),
		req.Category,
		req.MerchantName,
		req.CurrentAmount,
		userCountry,
		1, // Default household size
	)

	if err != nil {
		log.Printf("Market analysis failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to analyze charge"})
		return
	}

	c.JSON(http.StatusOK, suggestion)
}

// ============================================================================
// 2. BULK ANALYZE ALL CHARGES IN A BUDGET
// POST /api/v1/budgets/:id/suggestions/bulk-analyze
// ============================================================================

type ChargeToAnalyze struct {
	ID           string  `json:"id"`
	Category     string  `json:"category"`
	Label        string  `json:"label"`
	Amount       float64 `json:"amount"`
	MerchantName string  `json:"merchant_name,omitempty"`
}

type BulkAnalyzeRequest struct {
	Charges []ChargeToAnalyze `json:"charges" binding:"required"`
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

	// 2. Get User Country
	userCountry, err := h.getUserCountry(c.Request.Context(), userID)
	if err != nil {
		userCountry = "FR"
	}

	// 3. Get Household Size from budget data (count of "people" in the budget)
	householdSize := h.getHouseholdSizeFromBudget(c.Request.Context(), budgetID)

	log.Printf("[MarketSuggestions] Bulk analyzing %d charges for budget %s (Household: %d persons)",
		len(req.Charges), budgetID, householdSize)

	var suggestions []models.ChargeSuggestion
	cacheHits := 0
	aiCalls := 0
	totalSavings := 0.0

	for _, charge := range req.Charges {
		if !h.isSuggestionRelevant(charge.Category) {
			continue
		}

		suggestion, err := h.MarketAnalyzer.AnalyzeCharge(
			c.Request.Context(),
			charge.Category,
			charge.MerchantName,
			charge.Amount,
			userCountry,
			householdSize, // Pass actual household size
		)

		if err != nil {
			log.Printf("Failed to analyze charge %s: %v", charge.ID, err)
			continue
		}

		// Detect if it was a cache hit or an AI call
		if time.Since(suggestion.LastUpdated) < 5*time.Second {
			aiCalls++
		} else {
			cacheHits++
		}

		// Only add suggestion if there are actual savings opportunities
		if len(suggestion.Competitors) > 0 {
			bestSavings := suggestion.Competitors[0].PotentialSavings
			totalSavings += bestSavings

			suggestions = append(suggestions, models.ChargeSuggestion{
				ChargeID:    charge.ID,
				ChargeLabel: charge.Label,
				Suggestion:  suggestion,
			})
		}
	}

	response := models.BulkAnalyzeResponse{
		Suggestions:           suggestions,
		CacheHits:             cacheHits,
		AICallsMade:           aiCalls,
		TotalPotentialSavings: totalSavings,
		HouseholdSize:         householdSize, // Include in response for frontend display
	}

	log.Printf("[MarketSuggestions] ✅ Bulk analysis complete: %d suggestions with savings, %d cache hits, %d AI calls, %.2f€ potential savings",
		len(suggestions), cacheHits, aiCalls, totalSavings)

	c.JSON(http.StatusOK, response)
}

// ============================================================================
// 3. GET CACHED SUGGESTIONS FOR A CATEGORY
// GET /api/v1/suggestions/category/:category
// ============================================================================

func (h *MarketSuggestionsHandler) GetCategorySuggestions(c *gin.Context) {
	userID := c.GetString("user_id")
	category := c.Param("category")

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
		1, // Default household size
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
// 4. CLEAN EXPIRED CACHE (Admin/Cron)
// POST /api/v1/admin/suggestions/clean-cache
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
// 5. CATEGORIZE CHARGE (Hybrid: Static + AI Fallback)
// POST /api/v1/categorize
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

	// Step 1: Try Static Keyword Matching
	category := determineCategory(label)

	// Step 2: AI Fallback
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

// getHouseholdSizeFromBudget retrieves the number of "people" (revenue sources) from the budget data
// This represents the actual household size, not the number of budget collaborators
func (h *MarketSuggestionsHandler) getHouseholdSizeFromBudget(ctx context.Context, budgetID string) int {
	var dataJSON []byte
	err := h.DB.QueryRowContext(ctx,
		"SELECT data FROM budget_data WHERE budget_id = $1",
		budgetID,
	).Scan(&dataJSON)

	if err != nil {
		log.Printf("[MarketSuggestions] Failed to get budget data for household size: %v", err)
		return 1 // Default to 1 person
	}

	// Parse the JSON to extract "people" array
	var budgetData struct {
		People []interface{} `json:"people"`
	}

	if err := json.Unmarshal(dataJSON, &budgetData); err != nil {
		log.Printf("[MarketSuggestions] Failed to parse budget data: %v", err)
		return 1
	}

	peopleCount := len(budgetData.People)
	if peopleCount < 1 {
		return 1
	}

	log.Printf("[MarketSuggestions] Detected household size: %d persons", peopleCount)
	return peopleCount
}

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
			"NORDNET", "OVH", "K-NET",
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
			"NETFLIX", "SPOTIFY", "AMAZON", "PRIME", "DISNEY", "CANAL",
			"APPLE", "GOOGLE", "YOUTUBE", "DEEZER", "HBO", "PARAMOUNT", "ICLOUD", "DROPBOX",
		},
		"FOOD": {
			"CARREFOUR", "LECLERC", "AUCHAN", "INTERMARCHE", "LIDL", "ALDI", "MONOPRIX",
			"FRANPRIX", "SUPER U", "HYPER U", "CASINO", "PICARD", "UBER EATS", "DELIVEROO",
			"MC DO", "MCDONALD", "BK", "BURGER KING", "KFC", "STARBUCKS",
		},
		"HOUSING": {
			"LOYER", "RENT", "APPARTEMENT", "LOGEMENT", "CHARGES LOCATIVES",
		},
		"LEISURE": {
			"SPORT", "GYM", "FITNESS", "BASIC FIT", "KEEP COOL", "NEONESS",
			"CINEMA", "UGC", "PATHE", "GAUMONT",
		},
	}

	for cat, keys := range keywords {
		for _, k := range keys {
			if strings.Contains(l, k) {
				// Refinement for telecom providers
				if k == "SFR" || k == "ORANGE" || k == "BOUYGUES" || k == "FREE" {
					if strings.Contains(l, "BOX") || strings.Contains(l, "FIBRE") || strings.Contains(l, "FIXE") {
						return "INTERNET"
					}
					if strings.Contains(l, "MOBILE") || strings.Contains(l, "FORFAIT") {
						return "MOBILE"
					}
					return "MOBILE" // Default for these providers
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
		"ENERGY":           true,
		"INTERNET":         true,
		"MOBILE":           true,
		"INSURANCE":        true,
		"INSURANCE_AUTO":   true,
		"INSURANCE_HOME":   true,
		"INSURANCE_HEALTH": true,
		"LOAN":             true,
		"BANK":             true,
	}
	return relevantCategories[strings.ToUpper(category)]
}
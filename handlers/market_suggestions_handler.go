// handlers/market_suggestions_handler.go
// ============================================================================
// MARKET SUGGESTIONS HANDLER - Analyse IA des charges pour trouver des économies
// ============================================================================
// VERSION CORRIGÉE : Logging sécurisé sans données personnelles/financières
// ============================================================================

package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/LovationAdmin/budget-api/models"
	"github.com/LovationAdmin/budget-api/services"
	"github.com/LovationAdmin/budget-api/utils"
)

// ============================================================================
// HANDLER STRUCT
// ============================================================================

type MarketSuggestionsHandler struct {
	DB             *sql.DB
	MarketAnalyzer *services.MarketAnalyzerService
	WS             *WSHandler
}

func NewMarketSuggestionsHandler(db *sql.DB, analyzer *services.MarketAnalyzerService, ws *WSHandler) *MarketSuggestionsHandler {
	return &MarketSuggestionsHandler{
		DB:             db,
		MarketAnalyzer: analyzer,
		WS:             ws,
	}
}

// ============================================================================
// CATEGORY DETECTION HELPERS
// ============================================================================

// determineCategory détecte la catégorie à partir du libellé
func determineCategory(label string) string {
	labelLower := strings.ToLower(label)

	// Énergie
	if strings.Contains(labelLower, "edf") ||
		strings.Contains(labelLower, "engie") ||
		strings.Contains(labelLower, "total") ||
		strings.Contains(labelLower, "électricité") ||
		strings.Contains(labelLower, "electricite") ||
		strings.Contains(labelLower, "gaz") ||
		strings.Contains(labelLower, "énergie") ||
		strings.Contains(labelLower, "energie") {
		return "ENERGY"
	}

	// Internet
	if strings.Contains(labelLower, "orange") ||
		strings.Contains(labelLower, "sfr") ||
		strings.Contains(labelLower, "bouygues") ||
		strings.Contains(labelLower, "free") ||
		strings.Contains(labelLower, "internet") ||
		strings.Contains(labelLower, "fibre") ||
		strings.Contains(labelLower, "box") {
		return "INTERNET"
	}

	// Mobile
	if strings.Contains(labelLower, "mobile") ||
		strings.Contains(labelLower, "téléphone") ||
		strings.Contains(labelLower, "telephone") ||
		strings.Contains(labelLower, "forfait") {
		return "MOBILE"
	}

	// Assurances
	if strings.Contains(labelLower, "assurance") ||
		strings.Contains(labelLower, "axa") ||
		strings.Contains(labelLower, "maif") ||
		strings.Contains(labelLower, "macif") ||
		strings.Contains(labelLower, "matmut") ||
		strings.Contains(labelLower, "groupama") {
		if strings.Contains(labelLower, "auto") || strings.Contains(labelLower, "voiture") {
			return "INSURANCE_AUTO"
		}
		if strings.Contains(labelLower, "habitation") || strings.Contains(labelLower, "maison") || strings.Contains(labelLower, "logement") {
			return "INSURANCE_HOME"
		}
		if strings.Contains(labelLower, "santé") || strings.Contains(labelLower, "sante") || strings.Contains(labelLower, "mutuelle") {
			return "INSURANCE_HEALTH"
		}
		return "INSURANCE_HOME" // Default insurance
	}

	// Streaming
	if strings.Contains(labelLower, "netflix") ||
		strings.Contains(labelLower, "spotify") ||
		strings.Contains(labelLower, "disney") ||
		strings.Contains(labelLower, "amazon prime") ||
		strings.Contains(labelLower, "deezer") ||
		strings.Contains(labelLower, "canal") ||
		strings.Contains(labelLower, "streaming") {
		return "LEISURE_STREAMING"
	}

	// Sport
	if strings.Contains(labelLower, "sport") ||
		strings.Contains(labelLower, "fitness") ||
		strings.Contains(labelLower, "gym") ||
		strings.Contains(labelLower, "salle") ||
		strings.Contains(labelLower, "basic fit") ||
		strings.Contains(labelLower, "keep cool") {
		return "LEISURE_SPORT"
	}

	// Banque
	if strings.Contains(labelLower, "banque") ||
		strings.Contains(labelLower, "frais bancaires") ||
		strings.Contains(labelLower, "carte") {
		return "BANK"
	}

	// Prêt / Crédit
	if strings.Contains(labelLower, "prêt") ||
		strings.Contains(labelLower, "pret") ||
		strings.Contains(labelLower, "crédit") ||
		strings.Contains(labelLower, "credit") ||
		strings.Contains(labelLower, "emprunt") {
		return "LOAN"
	}

	// Transport
	if strings.Contains(labelLower, "transport") ||
		strings.Contains(labelLower, "navigo") ||
		strings.Contains(labelLower, "sncf") ||
		strings.Contains(labelLower, "ratp") ||
		strings.Contains(labelLower, "abonnement train") {
		return "TRANSPORT"
	}

	return "OTHER"
}

// isSuggestionRelevant vérifie si une catégorie est éligible aux suggestions
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
		"LEISURE_SPORT":     true,
		"LEISURE_STREAMING": true,
		"SUBSCRIPTION":      true,
		"HOUSING":           true,
	}
	return relevantCategories[strings.ToUpper(category)]
}

// ============================================================================
// HELPER METHODS
// ============================================================================

// checkBudgetAccess vérifie si l'utilisateur a accès au budget
func (h *MarketSuggestionsHandler) checkBudgetAccess(ctx context.Context, userID, budgetID string) (bool, error) {
	var count int
	err := h.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM budget_members 
		WHERE budget_id = $1 AND user_id = $2
	`, budgetID, userID).Scan(&count)

	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// getBudgetConfig récupère la localisation et devise d'un budget
func (h *MarketSuggestionsHandler) getBudgetConfig(ctx context.Context, budgetID string) (string, string, error) {
	var location, currency string
	err := h.DB.QueryRowContext(ctx, `
		SELECT COALESCE(location, 'FR'), COALESCE(currency, 'EUR') 
		FROM budgets WHERE id = $1
	`, budgetID).Scan(&location, &currency)

	if err != nil {
		return "FR", "EUR", err
	}
	return location, currency, nil
}

// ============================================================================
// 1. ANALYZE SINGLE CHARGE
// POST /api/v1/suggestions/analyze
// ============================================================================

type AnalyzeChargeRequest struct {
	Category      string  `json:"category" binding:"required"`
	MerchantName  string  `json:"merchant_name"`
	Amount        float64 `json:"amount" binding:"required"`
	Country       string  `json:"country"`
	Currency      string  `json:"currency"`
	HouseholdSize int     `json:"household_size"`
	Description   string  `json:"description"`
}

func (h *MarketSuggestionsHandler) AnalyzeCharge(c *gin.Context) {
	var req AnalyzeChargeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	// Defaults
	country := req.Country
	if country == "" {
		country = "FR"
	}
	currency := req.Currency
	if currency == "" {
		currency = "EUR"
	}
	householdSize := req.HouseholdSize
	if householdSize < 1 {
		householdSize = 1
	}

	// ✅ LOGGING SÉCURISÉ - Pas de montant ni données personnelles
	utils.LogAIAnalysis("SingleAnalyze", req.Category, country, 1)

	suggestion, err := h.MarketAnalyzer.AnalyzeCharge(
		c.Request.Context(),
		req.Category,
		req.MerchantName,
		req.Amount,
		country,
		currency,
		householdSize,
		req.Description,
	)

	if err != nil {
		utils.SafeError("Single charge analysis failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Analysis failed",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, suggestion)
}

// ============================================================================
// 2. GET CATEGORY SUGGESTIONS (CACHED)
// GET /api/v1/suggestions/category/:category
// ============================================================================

func (h *MarketSuggestionsHandler) GetCategorySuggestions(c *gin.Context) {
	category := strings.ToUpper(c.Param("category"))
	country := c.DefaultQuery("country", "FR")

	// ✅ LOGGING SÉCURISÉ
	utils.SafeInfo("Fetching cached suggestions for %s in %s", category, country)

	// Check cache
	var suggestion models.MarketSuggestion
	var competitorsJSON []byte

	err := h.DB.QueryRowContext(c.Request.Context(), `
		SELECT id, category, country, competitors, last_updated, expires_at
		FROM market_suggestions
		WHERE category = $1 AND country = $2 AND merchant_name IS NULL AND expires_at > $3
		ORDER BY last_updated DESC
		LIMIT 1
	`, category, country, time.Now()).Scan(
		&suggestion.ID,
		&suggestion.Category,
		&suggestion.Country,
		&competitorsJSON,
		&suggestion.LastUpdated,
		&suggestion.ExpiresAt,
	)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "No cached suggestions found",
			"message": "Use POST /suggestions/analyze to generate new suggestions",
		})
		return
	}

	if err != nil {
		utils.SafeError("Database error fetching suggestions: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"suggestion": suggestion,
		"cached":     true,
	})
}

// ============================================================================
// 3. BULK ANALYZE ALL CHARGES IN A BUDGET (ASYNC)
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

	// ✅ LOGGING SÉCURISÉ - Pas d'ID complet ni de montants
	utils.LogBudgetAction("BulkAnalyze-Start", budgetID, userID)
	utils.SafeInfo("Bulk analysis requested for %d charges", len(req.Charges))

	// 2. Respond IMMEDIATELY to prevent timeout (HTTP 202 Accepted)
	c.JSON(http.StatusAccepted, gin.H{
		"message": "Analysis started in background",
		"status":  "processing",
	})

	// 3. Launch background processing
	go func() {
		bgCtx := context.Background()

		// Récupération de la config budget
		country, currency, err := h.getBudgetConfig(bgCtx, budgetID)
		if err != nil {
			utils.SafeWarn("Could not fetch budget config, using defaults")
			country, currency = "FR", "EUR"
		}

		householdSize := req.HouseholdSize
		if householdSize < 1 {
			householdSize = 1
		}

		// ✅ LOGGING SÉCURISÉ
		utils.LogAIAnalysis("BulkAnalyze-Process", "MULTIPLE", country, len(req.Charges))

		var suggestions []models.ChargeSuggestion
		totalSavings := 0.0
		cacheHits := 0
		aiCallsMade := 0
		processedCount := 0

		for _, charge := range req.Charges {
			// Vérifier/corriger la catégorie
			analysisCategory := charge.Category
			if charge.Category == "LEISURE" || charge.Category == "OTHER" || charge.Category == "" {
				refined := determineCategory(charge.Label)
				if refined != "OTHER" && refined != "LEISURE" {
					utils.SafeDebug("Recategorized charge from %s to %s", charge.Category, refined)
					analysisCategory = refined
				}
			}

			// Vérifier si la catégorie est éligible
			if !h.isSuggestionRelevant(analysisCategory) {
				continue
			}

			// Petit délai pour éviter de surcharger l'API
			time.Sleep(100 * time.Millisecond)

			suggestion, err := h.MarketAnalyzer.AnalyzeCharge(
				bgCtx,
				analysisCategory,
				charge.MerchantName,
				charge.Amount,
				country,
				currency,
				householdSize,
				charge.Description,
			)

			if err != nil {
				utils.SafeWarn("Failed to analyze charge: %v", err)
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

			processedCount++
		}

		// ✅ LOGGING SÉCURISÉ - Pas de montant total exact
		utils.SafeInfo("Bulk analysis complete: %d charges processed, %d suggestions found", processedCount, len(suggestions))
		utils.LogBudgetAction("BulkAnalyze-Complete", budgetID, userID)

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
					"currency":                currency,
				},
			}

			h.WS.BroadcastJSON(budgetID, responsePayload)
		}
	}()
}

// ============================================================================
// 4. CATEGORIZE TRANSACTION LABEL
// POST /api/v1/categorize
// ============================================================================

type CategorizeRequest struct {
	Label string `json:"label" binding:"required"`
}

func (h *MarketSuggestionsHandler) CategorizeLabel(c *gin.Context) {
	var req CategorizeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Label is required"})
		return
	}

	// ✅ LOGGING SÉCURISÉ - Pas de libellé complet
	utils.SafeDebug("Categorizing label (length: %d)", len(req.Label))

	category := determineCategory(req.Label)

	c.JSON(http.StatusOK, gin.H{
		"label":    req.Label,
		"category": category,
	})
}
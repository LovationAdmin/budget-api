package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"

	"budget-api/middleware"
	"budget-api/services"

	"github.com/gin-gonic/gin"
)

// ============================================================================
// MARKET SUGGESTIONS HANDLER
// Endpoints pour obtenir des suggestions de concurrents personnalisées
// ============================================================================

type MarketSuggestionsHandler struct {
	DB              *sql.DB
	MarketAnalyzer  *services.MarketAnalyzerService
}

func NewMarketSuggestionsHandler(db *sql.DB) *MarketSuggestionsHandler {
	aiService := services.NewClaudeAIService()
	marketAnalyzer := services.NewMarketAnalyzerService(db, aiService)
	
	return &MarketSuggestionsHandler{
		DB:             db,
		MarketAnalyzer: marketAnalyzer,
	}
}

// ============================================================================
// 1. ANALYSE D'UNE CHARGE SPÉCIFIQUE
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

	// Récupérer le pays de l'utilisateur
	userCountry, err := h.getUserCountry(c.Request.Context(), userID)
	if err != nil {
		log.Printf("Failed to get user country: %v", err)
		userCountry = "FR" // Default à France
	}

	log.Printf("[MarketSuggestions] Analyzing charge for user %s: %s - %.2f€ (%s)",
		userID, req.Category, req.CurrentAmount, userCountry)

	// Analyser et récupérer les suggestions
	suggestion, err := h.MarketAnalyzer.AnalyzeCharge(
		c.Request.Context(),
		req.Category,
		req.MerchantName,
		req.CurrentAmount,
		userCountry,
	)

	if err != nil {
		log.Printf("Market analysis failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to analyze charge",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, suggestion)
}

// ============================================================================
// 2. ANALYSE EN MASSE DE TOUTES LES CHARGES D'UN BUDGET
// POST /api/v1/budgets/:budget_id/suggestions/bulk-analyze
// ============================================================================

type BulkAnalyzeRequest struct {
	Charges []ChargeToAnalyze `json:"charges" binding:"required"`
}

type ChargeToAnalyze struct {
	ID            string  `json:"id"`
	Category      string  `json:"category"`
	Label         string  `json:"label"`
	Amount        float64 `json:"amount"`
	MerchantName  string  `json:"merchant_name,omitempty"`
}

type BulkAnalyzeResponse struct {
	Suggestions      []SuggestionWithCharge `json:"suggestions"`
	CacheHits        int                    `json:"cache_hits"`
	AICallsMade      int                    `json:"ai_calls_made"`
	TotalPotentialSavings float64           `json:"total_potential_savings"`
}

type SuggestionWithCharge struct {
	ChargeID    string                          `json:"charge_id"`
	ChargeLabel string                          `json:"charge_label"`
	Suggestion  *services.MarketSuggestion      `json:"suggestion"`
}

func (h *MarketSuggestionsHandler) BulkAnalyzeCharges(c *gin.Context) {
	userID := c.GetString("user_id")
	budgetID := c.Param("budget_id")

	// Vérifier que l'utilisateur a accès au budget
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

	// Récupérer le pays de l'utilisateur
	userCountry, err := h.getUserCountry(c.Request.Context(), userID)
	if err != nil {
		userCountry = "FR"
	}

	log.Printf("[MarketSuggestions] Bulk analyzing %d charges for budget %s", len(req.Charges), budgetID)

	var suggestions []SuggestionWithCharge
	cacheHits := 0
	aiCalls := 0
	totalSavings := 0.0

	// Analyser chaque charge
	for _, charge := range req.Charges {
		// Skip si pas de catégorie pertinente
		if !h.isSuggestionRelevant(charge.Category) {
			continue
		}

		suggestion, err := h.MarketAnalyzer.AnalyzeCharge(
			c.Request.Context(),
			charge.Category,
			charge.MerchantName,
			charge.Amount,
			userCountry,
		)

		if err != nil {
			log.Printf("Failed to analyze charge %s: %v", charge.ID, err)
			continue
		}

		// Compter cache hits vs AI calls (simple heuristique)
		if suggestion.LastUpdated.After(suggestion.LastUpdated.Add(-1 * time.Minute)) {
			aiCalls++
		} else {
			cacheHits++
		}

		// Calculer les économies totales
		for _, comp := range suggestion.Competitors {
			if comp.PotentialSavings > 0 {
				totalSavings += comp.PotentialSavings
				break // Prendre seulement la meilleure économie
			}
		}

		suggestions = append(suggestions, SuggestionWithCharge{
			ChargeID:    charge.ID,
			ChargeLabel: charge.Label,
			Suggestion:  suggestion,
		})
	}

	response := BulkAnalyzeResponse{
		Suggestions:           suggestions,
		CacheHits:             cacheHits,
		AICallsMade:           aiCalls,
		TotalPotentialSavings: totalSavings,
	}

	log.Printf("[MarketSuggestions] ✅ Bulk analysis complete: %d suggestions, %d cache hits, %d AI calls, %.2f€ potential savings",
		len(suggestions), cacheHits, aiCalls, totalSavings)

	c.JSON(http.StatusOK, response)
}

// ============================================================================
// 3. RÉCUPÉRER LES SUGGESTIONS EN CACHE POUR UNE CATÉGORIE
// GET /api/v1/suggestions/category/:category
// ============================================================================

func (h *MarketSuggestionsHandler) GetCategorySuggestions(c *gin.Context) {
	userID := c.GetString("user_id")
	category := c.Param("category")

	userCountry, err := h.getUserCountry(c.Request.Context(), userID)
	if err != nil {
		userCountry = "FR"
	}

	// Récupérer depuis le cache
	suggestion, err := h.MarketAnalyzer.GetCachedSuggestion(
		c.Request.Context(),
		category,
		userCountry,
		"", // Pas de merchant spécifique
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch suggestions"})
		return
	}

	if suggestion == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "No suggestions found in cache"})
		return
	}

	c.JSON(http.StatusOK, suggestion)
}

// ============================================================================
// 4. NETTOYER LE CACHE EXPIRÉ (Admin/Cron)
// POST /api/v1/admin/suggestions/clean-cache
// ============================================================================

func (h *MarketSuggestionsHandler) CleanExpiredCache(c *gin.Context) {
	err := h.MarketAnalyzer.CleanExpiredCache(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clean cache"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Cache cleaned successfully"})
}

// ============================================================================
// HELPERS
// ============================================================================

func (h *MarketSuggestionsHandler) getUserCountry(ctx context.Context, userID string) (string, error) {
	var country string
	err := h.DB.QueryRowContext(ctx,
		"SELECT COALESCE(country, 'FR') FROM users WHERE id = $1",
		userID,
	).Scan(&country)
	
	if err != nil {
		return "FR", err
	}
	
	return country, nil
}

func (h *MarketSuggestionsHandler) checkBudgetAccess(ctx context.Context, userID string, budgetID string) (bool, error) {
	var exists bool
	err := h.DB.QueryRowContext(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM budget_members 
			WHERE budget_id = $1 AND user_id = $2
		)`,
		budgetID, userID,
	).Scan(&exists)
	
	return exists, err
}

func (h *MarketSuggestionsHandler) isSuggestionRelevant(category string) bool {
	relevantCategories := map[string]bool{
		"ENERGY":    true,
		"INTERNET":  true,
		"MOBILE":    true,
		"INSURANCE": true,
		"LOAN":      true,
		"BANK":      true,
	}
	
	return relevantCategories[category]
}

// ============================================================================
// ENREGISTREMENT DES ROUTES
// ============================================================================

func (h *MarketSuggestionsHandler) RegisterRoutes(router *gin.RouterGroup) {
	suggestions := router.Group("/suggestions")
	suggestions.Use(middleware.AuthMiddleware())
	{
		suggestions.POST("/analyze", h.AnalyzeCharge)
		suggestions.GET("/category/:category", h.GetCategorySuggestions)
	}

	budgets := router.Group("/budgets")
	budgets.Use(middleware.AuthMiddleware())
	{
		budgets.POST("/:budget_id/suggestions/bulk-analyze", h.BulkAnalyzeCharges)
	}

	// Routes admin (ajouter middleware admin si nécessaire)
	admin := router.Group("/admin")
	admin.Use(middleware.AuthMiddleware())
	{
		admin.POST("/suggestions/clean-cache", h.CleanExpiredCache)
	}
}
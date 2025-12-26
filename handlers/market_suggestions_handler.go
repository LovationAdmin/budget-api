package handlers

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"strings"
	"time"

	"budget-api/models"
	"budget-api/services"

	"github.com/gin-gonic/gin"
)

// ============================================================================
// MARKET SUGGESTIONS HANDLER
// Endpoints pour obtenir des suggestions de concurrents personnalisées
// ============================================================================

type MarketSuggestionsHandler struct {
	DB             *sql.DB
	MarketAnalyzer *services.MarketAnalyzerService
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
			"error":   "Failed to analyze charge",
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

	var suggestions []models.ChargeSuggestion
	cacheHits := 0
	aiCalls := 0
	totalSavings := 0.0

	// Timestamp de début pour détecter les appels AI (nouvelle donnée < 5 secondes)
	startTime := time.Now()

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

		// Détecter si c'était un cache hit ou un AI call
		// Si la suggestion a été créée récemment (< 5 secondes), c'est un AI call
		if time.Since(suggestion.LastUpdated) < 5*time.Second {
			aiCalls++
		} else {
			cacheHits++
		}

		// Calculer les économies totales (prendre la meilleure offre)
		if len(suggestion.Competitors) > 0 {
			bestSavings := suggestion.Competitors[0].PotentialSavings
			totalSavings += bestSavings
		}

		suggestions = append(suggestions, models.ChargeSuggestion{
			ChargeID:    charge.ID,
			ChargeLabel: charge.Label,
			Suggestion:  suggestion,
		})
	}

	response := models.BulkAnalyzeResponse{
		Suggestions:           suggestions,
		CacheHits:             cacheHits,
		AICallsMade:           aiCalls,
		TotalPotentialSavings: totalSavings,
	}

	log.Printf("[MarketSuggestions] ✅ Bulk analysis complete: %d suggestions, %d cache hits, %d AI calls, %.2f€ potential savings (%.2fs)",
		len(suggestions), cacheHits, aiCalls, totalSavings, time.Since(startTime).Seconds())

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

	// Récupérer depuis le cache via le MarketAnalyzer
	suggestion, err := h.MarketAnalyzer.AnalyzeCharge(
		c.Request.Context(),
		category,
		"", // Pas de merchant spécifique
		0,  // Pas de montant
		userCountry,
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
	var country sql.NullString
	err := h.DB.QueryRowContext(ctx,
		"SELECT country FROM users WHERE id = $1",
		userID,
	).Scan(&country)

	if err != nil {
		return "FR", err
	}

	if !country.Valid || country.String == "" {
		return "FR", nil
	}

	return country.String, nil
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
	// Normaliser la catégorie
	category = strings.ToUpper(category)

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
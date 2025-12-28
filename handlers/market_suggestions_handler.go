package handlers

import (
	"budget-api/models"
	"budget-api/services"
	"context"
	"database/sql"
	"log"
	"net/http"
	"strings"
	"time"

	"[github.com/gin-gonic/gin](https://github.com/gin-gonic/gin)"
)

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
		1, 
	)

	if err != nil {
		log.Printf("Market analysis failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to analyze charge"})
		return
	}

	c.JSON(http.StatusOK, suggestion)
}

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

	// 3. Get Member Count (Household Size)
	var memberCount int
	err = h.DB.QueryRowContext(c.Request.Context(), 
		"SELECT COUNT(*) FROM budget_members WHERE budget_id = $1", budgetID).Scan(&memberCount)
	
	if err != nil || memberCount < 1 {
		memberCount = 1 // Fallback
	}

	log.Printf("[MarketSuggestions] Bulk analyzing %d charges for budget %s (Household: %d)", len(req.Charges), budgetID, memberCount)

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
			memberCount, 
		)

		if err != nil {
			log.Printf("Failed to analyze charge %s: %v", charge.ID, err)
			continue
		}

		if time.Since(suggestion.LastUpdated) < 5*time.Second {
			aiCalls++
		} else {
			cacheHits++
		}

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

	c.JSON(http.StatusOK, response)
}

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
		1, 
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

// THIS FUNCTION CAUSED THE ERROR - NOW IT WILL WORK
func (h *MarketSuggestionsHandler) CleanExpiredCache(c *gin.Context) {
	if err := h.MarketAnalyzer.CleanExpiredCache(c.Request.Context()); err != nil {
		log.Printf("Failed to clean cache: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clean cache"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Cache cleaned successfully"})
}

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

// Helpers
func determineCategory(label string) string {
	l := strings.ToUpper(strings.TrimSpace(label))

	keywords := map[string][]string{
		"MOBILE": {"MOBILE", "PORTABLE", "SOSH", "BOUYGUES", "FREE", "ORANGE", "SFR", "RED BY"},
		"INTERNET": {"BOX", "FIBRE", "ADSL", "INTERNET"},
		"ENERGY": {"EDF", "ENGIE", "TOTAL", "ENERGIE", "ELEC", "GAZ"},
		"INSURANCE": {"ASSURANCE", "AXA", "MAIF", "ALLIANZ", "MACIF"},
		"LOAN": {"PRET", "CREDIT", "ECHEANCE", "EMPRUNT"},
		"BANK": {"BANQUE", "CREDIT AGRICOLE", "SOCIETE GENERALE", "BNP"},
	}

	for cat, keys := range keywords {
		for _, k := range keys {
			if strings.Contains(l, k) {
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
		"ENERGY": true, "INTERNET": true, "MOBILE": true, "INSURANCE": true, "LOAN": true, "BANK": true,
	}
	return relevantCategories[strings.ToUpper(category)]
}
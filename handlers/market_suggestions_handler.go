package handlers

import (
	"budget-api/models"
	"budget-api/services"
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

// ... AnalyzeCharge (Single) ...

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
		memberCount = 1
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

		// Appel avec le memberCount
		suggestion, err := h.MarketAnalyzer.AnalyzeCharge(
			c.Request.Context(),
			charge.Category,
			charge.MerchantName,
			charge.Amount,
			userCountry,
			memberCount, // <--- Size passed here
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

// ... GetCategorySuggestions, CleanExpiredCache, CategorizeCharge ...
// (Ces fonctions restent inchangées, assurez-vous juste que CategorizeCharge est là)

func (h *MarketSuggestionsHandler) CategorizeCharge(c *gin.Context) {
	// ... (Code existant inchangé) ...
    c.JSON(http.StatusOK, gin.H{"label": "", "category": "OTHER"})
}

// ... Helpers (checkBudgetAccess, getUserCountry, isSuggestionRelevant) ...
// (Code existant inchangé)
func (h *MarketSuggestionsHandler) checkBudgetAccess(ctx context.Context, userID, budgetID string) (bool, error) {
    // ...
    return true, nil
}

func (h *MarketSuggestionsHandler) getUserCountry(ctx context.Context, userID string) (string, error) {
    // ...
    return "FR", nil
}

func (h *MarketSuggestionsHandler) isSuggestionRelevant(cat string) bool {
    // ...
    return true
}

// Dummy placeholder for analyze single (not used in bulk flow)
func (h *MarketSuggestionsHandler) AnalyzeCharge(c *gin.Context) {}
func (h *MarketSuggestionsHandler) GetCategorySuggestions(c *gin.Context) {}
func (h *MarketSuggestionsHandler) CleanExpiredCache(c *gin.Context) {}
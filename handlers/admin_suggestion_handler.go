package handlers

import (
	"budget-api/services"
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type AdminSuggestionHandler struct {
	DB             *sql.DB
	MarketAnalyzer *services.MarketAnalyzerService
}

func NewAdminSuggestionHandler(db *sql.DB) *AdminSuggestionHandler {
	// Re-use existing services
	aiService := services.NewClaudeAIService()
	marketAnalyzer := services.NewMarketAnalyzerService(db, aiService)

	return &AdminSuggestionHandler{
		DB:             db,
		MarketAnalyzer: marketAnalyzer,
	}
}

// RetroactiveAnalysis iterates over ALL budgets, fixes categories, and warms the cache
func (h *AdminSuggestionHandler) RetroactiveAnalysis(c *gin.Context) {
	// 1. Fetch all budget data
	rows, err := h.DB.QueryContext(c.Request.Context(), "SELECT budget_id, data FROM budget_data")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query budgets: " + err.Error()})
		return
	}
	defer rows.Close()

	stats := struct {
		BudgetsProcessed int `json:"budgets_processed"`
		BudgetsUpdated   int `json:"budgets_updated"`
		ChargesFixed     int `json:"charges_fixed"`
		CacheEntriesNew  int `json:"cache_entries_created"`
	}{}

	// Use a background context for AI calls so they don't timeout if the HTTP request drops
	bgCtx := context.Background()

	for rows.Next() {
		stats.BudgetsProcessed++
		var budgetID string
		var rawData []byte

		if err := rows.Scan(&budgetID, &rawData); err != nil {
			log.Printf("Scan error: %v", err)
			continue
		}

		// 2. Parse JSON Data
		var dataMap map[string]interface{}
		if err := json.Unmarshal(rawData, &dataMap); err != nil {
			log.Printf("JSON error for budget %s: %v", budgetID, err)
			continue
		}

		// 3. Locate "charges" array
		chargesRaw, ok := dataMap["charges"].([]interface{})
		if !ok {
			continue // No charges in this budget
		}

		budgetModified := false

		// 4. Iterate over charges
		for i, ch := range chargesRaw {
			chargeMap, ok := ch.(map[string]interface{})
			if !ok {
				continue
			}

			label, _ := chargeMap["label"].(string)
			currentCat, _ := chargeMap["category"].(string)
			amount, _ := chargeMap["amount"].(float64)

			// LOGIC: If category is missing/OTHER, OR if you want to force re-check all:
			if label != "" && (currentCat == "" || currentCat == "OTHER") {
				
				// A. Apply Smart Categorization
				// determineCategory is available since both files are in package handlers
				newCat := determineCategory(label) 

				if newCat != "OTHER" && newCat != currentCat {
					// Update the Charge in Memory
					chargeMap["category"] = newCat
					chargesRaw[i] = chargeMap
					budgetModified = true
					stats.ChargesFixed++
					currentCat = newCat // Update for step B
				}
			}

			// B. Warm Up Cache (Trigger Analysis)
			// Only for relevant categories
			if isCategoryRelevant(currentCat) {
				// We call AnalyzeCharge. This checks DB cache first.
				// If missing, it calls AI and saves result.
				// This ensures that when the user logs in, the data is INSTANT.
				
				// FIX: Added '1' as householdSize argument (default for bulk admin task)
				_, err := h.MarketAnalyzer.AnalyzeCharge(bgCtx, currentCat, "", amount, "FR", 1)
				
				if err == nil {
					// We don't easily know if it was a hit or miss here without changing service return,
					// but we know we ensured it exists.
					stats.CacheEntriesNew++ // Loosely tracking processed items
				}
				// Sleep briefly to avoid hitting rate limits if processing thousands of items
				time.Sleep(50 * time.Millisecond)
			}
		}

		// 5. Save Updated Budget JSON back to DB if needed
		if budgetModified {
			dataMap["charges"] = chargesRaw
			updatedJSON, _ := json.Marshal(dataMap)

			_, err := h.DB.ExecContext(c.Request.Context(),
				"UPDATE budget_data SET data = $1 WHERE budget_id = $2",
				updatedJSON, budgetID)

			if err == nil {
				stats.BudgetsUpdated++
				log.Printf("[Migration] Updated budget %s (fixed categories)", budgetID)
			} else {
				log.Printf("[Migration] Failed to save budget %s: %v", budgetID, err)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Retroactive analysis complete",
		"stats":   stats,
	})
}

// Helper function local to this file to avoid conflicts if needed,
// but effectively duplicates logic from market_suggestions_handler which is fine.
func isCategoryRelevant(cat string) bool {
	switch cat {
	case "MOBILE", "INTERNET", "ENERGY", "INSURANCE", "LOAN", "BANK":
		return true
	default:
		return false
	}
}
package handlers

import (
	"github.com/LovationAdmin/budget-api/services"
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
	aiService := services.NewClaudeAIService()
	marketAnalyzer := services.NewMarketAnalyzerService(db, aiService)

	return &AdminSuggestionHandler{
		DB:             db,
		MarketAnalyzer: marketAnalyzer,
	}
}

type MigrationStats struct {
	BudgetsProcessed int `json:"budgets_processed"`
	BudgetsUpdated   int `json:"budgets_updated"`
	ChargesFixed     int `json:"charges_fixed"`
	CacheEntriesNew  int `json:"cache_entries_new"`
}

func (h *AdminSuggestionHandler) RetroactiveAnalysis(c *gin.Context) {
	stats := MigrationStats{}

	rows, err := h.DB.QueryContext(c.Request.Context(), "SELECT budget_id, data FROM budget_data")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch budgets"})
		return
	}
	defer rows.Close()

	bgCtx := context.Background()

	for rows.Next() {
		var budgetID string
		var dataJSON []byte
		if err := rows.Scan(&budgetID, &dataJSON); err != nil {
			continue
		}

		stats.BudgetsProcessed++

		var dataMap map[string]interface{}
		if err := json.Unmarshal(dataJSON, &dataMap); err != nil {
			continue
		}

		chargesRaw, ok := dataMap["charges"].([]interface{})
		if !ok {
			continue
		}

		budgetModified := false

		for _, chargeRaw := range chargesRaw {
			charge, ok := chargeRaw.(map[string]interface{})
			if !ok {
				continue
			}

			label, _ := charge["label"].(string)
			currentCat, _ := charge["category"].(string)
			amount, _ := charge["amount"].(float64)

			if label == "" || amount == 0 {
				continue
			}

			detectedCat := determineCategory(label)

			if currentCat == "" || currentCat == "AUTRE" || currentCat != detectedCat {
				charge["category"] = detectedCat
				budgetModified = true
				stats.ChargesFixed++
			}

			if isCategoryRelevant(detectedCat) {
				_, err := h.MarketAnalyzer.AnalyzeCharge(bgCtx, detectedCat, "", amount, "FR", 1)
				if err == nil {
					stats.CacheEntriesNew++
				}
				time.Sleep(50 * time.Millisecond)
			}
		}

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

func isCategoryRelevant(cat string) bool {
	switch cat {
	case "MOBILE", "INTERNET", "ENERGY", "INSURANCE", "LOAN", "BANK":
		return true
	default:
		return false
	}
}
package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	
	"github.com/LovationAdmin/budget-api/services"
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

// RetroactiveAnalysis parcourt tous les budgets pour :
// 1. Corriger les catégories
// 2. Pré-générer les suggestions IA en cache (avec le bon pays/devise)
func (h *AdminSuggestionHandler) RetroactiveAnalysis(c *gin.Context) {
	stats := MigrationStats{}

	// ✅ RECUPERER AUSSI LOCATION ET CURRENCY DU BUDGET
	rows, err := h.DB.QueryContext(c.Request.Context(), 
		`SELECT bd.budget_id, bd.data, COALESCE(b.location, 'FR'), COALESCE(b.currency, 'EUR')
		 FROM budget_data bd
		 JOIN budgets b ON bd.budget_id = b.id`)
	
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch budgets"})
		return
	}
	defer rows.Close()

	bgCtx := context.Background()

	for rows.Next() {
		var budgetID string
		var dataJSON []byte
		var location, currency string // ✅ NOUVELLES VARIABLES

		if err := rows.Scan(&budgetID, &dataJSON, &location, &currency); err != nil {
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
			
			// Récupérer la description si elle existe
			description, _ := charge["description"].(string)

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
				// ✅ APPEL AVEC LOCALISATION ET DEVISE DYNAMIQUES
				_, err := h.MarketAnalyzer.AnalyzeCharge(
					bgCtx, 
					detectedCat, 
					"",           // Pas de merchant name spécifique pour l'analyse générique
					amount, 
					location,     // ✅ PAYS DU BUDGET
					currency,     // ✅ DEVISE DU BUDGET
					1,            // Household size par défaut
					description,  // ✅ DESCRIPTION
				)
				
				if err == nil {
					stats.CacheEntriesNew++
				}
				// Petit délai pour éviter de surcharger l'API IA
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
				log.Printf("[Migration] Updated budget %s (fixed categories, loc: %s)", budgetID, location)
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
	return relevantCategories[strings.ToUpper(cat)]
}
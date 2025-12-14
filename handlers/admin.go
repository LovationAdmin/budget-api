// handlers/admin.go
package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"budget-api/utils"

	"github.com/gin-gonic/gin"
)

type AdminHandler struct {
	DB *sql.DB
}

// ============================================
// Migration Logic (intégrée directement ici)
// ============================================

var MONTHS = []string{
	"Janvier", "Février", "Mars", "Avril", "Mai", "Juin",
	"Juillet", "Août", "Septembre", "Octobre", "Novembre", "Décembre",
}

var MONTH_NAME_VARIANTS = map[string]string{
	"Janvier": "Janvier", "janvier": "Janvier",
	"Février": "Février", "février": "Février", "Fevrier": "Février",
	"Mars": "Mars", "mars": "Mars",
	"Avril": "Avril", "avril": "Avril",
	"Mai": "Mai", "mai": "Mai",
	"Juin": "Juin", "juin": "Juin",
	"Juillet": "Juillet", "juillet": "Juillet",
	"Août": "Août", "août": "Août", "Aout": "Août",
	"Septembre": "Septembre", "septembre": "Septembre",
	"Octobre": "Octobre", "octobre": "Octobre",
	"Novembre": "Novembre", "novembre": "Novembre",
	"Décembre": "Décembre", "décembre": "Décembre", "Decembre": "Décembre",
	// Encodage UTF-8 mojibake
	"FÃ©vrier":  "Février",
	"AoÃ»t":     "Août",
	"DÃ©cembre": "Décembre",
}

type EncryptedDataWrapper struct {
	Encrypted string `json:"encrypted"`
}

func normalizeMonthName(monthKey string) (string, bool) {
	if normalized, ok := MONTH_NAME_VARIANTS[monthKey]; ok {
		return normalized, true
	}
	return "", false
}

func isMonthName(key string) bool {
	_, ok := normalizeMonthName(key)
	return ok
}

func getMonthIndex(monthName string) int {
	normalized, ok := normalizeMonthName(monthName)
	if !ok {
		return -1
	}
	for i, m := range MONTHS {
		if m == normalized {
			return i
		}
	}
	return -1
}

func isLegacyFormat(yearlyData map[string]interface{}) bool {
	for key := range yearlyData {
		if isMonthName(key) {
			return true
		}
	}
	return false
}

func isNewFormat(yearlyData map[string]interface{}) bool {
	for key, value := range yearlyData {
		if len(key) == 4 && key[0] >= '0' && key[0] <= '9' {
			if yearMap, ok := value.(map[string]interface{}); ok {
				if _, hasMonths := yearMap["months"]; hasMonths {
					return true
				}
			}
		}
	}
	return false
}

func migrateBudgetData(data map[string]interface{}) (map[string]interface{}, bool, error) {
	yearlyData, ok := data["yearlyData"].(map[string]interface{})
	if !ok || yearlyData == nil {
		return data, false, nil
	}

	if isNewFormat(yearlyData) {
		return data, false, nil
	}

	if !isLegacyFormat(yearlyData) {
		return data, false, nil
	}

	// Déterminer l'année cible depuis currentYear du budget
	targetYear := time.Now().Year()
	if cy, ok := data["currentYear"].(float64); ok {
		targetYear = int(cy)
	}
	yearKey := fmt.Sprintf("%d", targetYear)

	// Nouvelle structure
	newYearlyData := make(map[string]interface{})
	months := make([]map[string]interface{}, 12)
	expenses := make([]map[string]interface{}, 12)
	monthComments := make([]string, 12)
	expenseComments := make([]map[string]interface{}, 12)

	for i := 0; i < 12; i++ {
		months[i] = make(map[string]interface{})
		expenses[i] = make(map[string]interface{})
		monthComments[i] = ""
		expenseComments[i] = make(map[string]interface{})
	}

	// Migrer yearlyData (allocations projets)
	for monthKey, monthData := range yearlyData {
		idx := getMonthIndex(monthKey)
		if idx >= 0 && idx < 12 {
			if md, ok := monthData.(map[string]interface{}); ok {
				if _, hasMonths := md["months"]; !hasMonths {
					months[idx] = md
				}
			}
		}
	}

	// Migrer yearlyExpenses
	if ye, ok := data["yearlyExpenses"].(map[string]interface{}); ok {
		for monthKey, expenseData := range ye {
			idx := getMonthIndex(monthKey)
			if idx >= 0 && idx < 12 {
				if ed, ok := expenseData.(map[string]interface{}); ok {
					expenses[idx] = ed
				}
			}
		}
	}

	// Migrer monthComments
	if mc, ok := data["monthComments"].(map[string]interface{}); ok {
		for monthKey, comment := range mc {
			idx := getMonthIndex(monthKey)
			if idx >= 0 && idx < 12 {
				if c, ok := comment.(string); ok {
					monthComments[idx] = c
				}
			}
		}
	}

	// Migrer projectComments
	if pc, ok := data["projectComments"].(map[string]interface{}); ok {
		for monthKey, comments := range pc {
			idx := getMonthIndex(monthKey)
			if idx >= 0 && idx < 12 {
				if pcm, ok := comments.(map[string]interface{}); ok {
					expenseComments[idx] = pcm
				}
			}
		}
	}

	// Migrer oneTimeIncomes
	if oti, ok := data["oneTimeIncomes"].(map[string]interface{}); ok {
		newOneTimeIncomes := make(map[string]interface{})
		newYearIncomes := make([]map[string]interface{}, 12)
		for i := 0; i < 12; i++ {
			newYearIncomes[i] = map[string]interface{}{"amount": float64(0), "description": ""}
		}
		for monthKey, income := range oti {
			idx := getMonthIndex(monthKey)
			if idx >= 0 && idx < 12 {
				if amount, ok := income.(float64); ok {
					newYearIncomes[idx] = map[string]interface{}{"amount": amount, "description": ""}
				}
			}
		}
		newOneTimeIncomes[yearKey] = newYearIncomes
		data["oneTimeIncomes"] = newOneTimeIncomes
	}

	// Construire la nouvelle structure
	newYearlyData[yearKey] = map[string]interface{}{
		"months":          months,
		"expenses":        expenses,
		"monthComments":   monthComments,
		"expenseComments": expenseComments,
		"deletedMonths":   []int{},
	}

	data["yearlyData"] = newYearlyData
	data["version"] = "2.3-migrated"
	data["lastUpdated"] = time.Now().Format(time.RFC3339)

	// Nettoyer les anciens champs (optionnel, garde pour compatibilité)
	// delete(data, "yearlyExpenses")
	// delete(data, "monthComments")
	// delete(data, "projectComments")

	return data, true, nil
}

// ============================================
// HTTP Handlers
// ============================================

// MigrateAllBudgets migre tous les budgets vers le nouveau format
// POST /api/v1/admin/migrate-budgets
func (h *AdminHandler) MigrateAllBudgets(c *gin.Context) {
	// Vérification du secret admin
	adminSecret := c.GetHeader("X-Admin-Secret")
	expectedSecret := os.Getenv("ADMIN_SECRET")

	if expectedSecret == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ADMIN_SECRET not configured"})
		return
	}

	if adminSecret != expectedSecret {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid admin secret"})
		return
	}

	// Exécuter la migration
	result, err := h.runMigration(c.Request.Context(), "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// MigrateSingleBudget migre un seul budget
// POST /api/v1/admin/migrate-budget/:id
func (h *AdminHandler) MigrateSingleBudget(c *gin.Context) {
	// Vérification du secret admin
	adminSecret := c.GetHeader("X-Admin-Secret")
	expectedSecret := os.Getenv("ADMIN_SECRET")

	if expectedSecret == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ADMIN_SECRET not configured"})
		return
	}

	if adminSecret != expectedSecret {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid admin secret"})
		return
	}

	budgetID := c.Param("id")
	if budgetID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Budget ID required"})
		return
	}

	result, err := h.runMigration(c.Request.Context(), budgetID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// runMigration exécute la migration pour un ou tous les budgets
func (h *AdminHandler) runMigration(ctx context.Context, budgetID string) (gin.H, error) {
	var query string
	var rows *sql.Rows
	var err error

	if budgetID != "" {
		query = `
			SELECT bd.budget_id, bd.data, b.name
			FROM budget_data bd
			JOIN budgets b ON bd.budget_id = b.id
			WHERE bd.budget_id = $1
		`
		rows, err = h.DB.QueryContext(ctx, query, budgetID)
	} else {
		query = `
			SELECT bd.budget_id, bd.data, b.name
			FROM budget_data bd
			JOIN budgets b ON bd.budget_id = b.id
			ORDER BY bd.updated_at DESC
		`
		rows, err = h.DB.QueryContext(ctx, query)
	}

	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var migrated, skipped, errors int
	var details []gin.H

	for rows.Next() {
		var id string
		var rawJSON []byte
		var name string

		if err := rows.Scan(&id, &rawJSON, &name); err != nil {
			log.Printf("❌ Scan error: %v", err)
			errors++
			continue
		}

		log.Printf("📦 Processing: %s (%s)", name, id)

		// 1. Décrypter les données
		var data map[string]interface{}
		var wrapper EncryptedDataWrapper

		if err := json.Unmarshal(rawJSON, &wrapper); err == nil && wrapper.Encrypted != "" {
			// Données chiffrées
			decryptedBytes, err := utils.Decrypt(wrapper.Encrypted)
			if err != nil {
				log.Printf("  ❌ Decrypt error: %v", err)
				errors++
				details = append(details, gin.H{"id": id, "name": name, "status": "error", "reason": "decrypt failed"})
				continue
			}
			if err := json.Unmarshal(decryptedBytes, &data); err != nil {
				log.Printf("  ❌ Unmarshal error: %v", err)
				errors++
				details = append(details, gin.H{"id": id, "name": name, "status": "error", "reason": "unmarshal failed"})
				continue
			}
		} else {
			// Données non chiffrées (legacy)
			if err := json.Unmarshal(rawJSON, &data); err != nil {
				log.Printf("  ❌ Unmarshal error: %v", err)
				errors++
				details = append(details, gin.H{"id": id, "name": name, "status": "error", "reason": "unmarshal failed"})
				continue
			}
		}

		// 2. Migrer les données
		migratedData, wasMigrated, err := migrateBudgetData(data)
		if err != nil {
			log.Printf("  ❌ Migration error: %v", err)
			errors++
			details = append(details, gin.H{"id": id, "name": name, "status": "error", "reason": err.Error()})
			continue
		}

		if !wasMigrated {
			log.Printf("  ⏭️  Already migrated or no migration needed")
			skipped++
			details = append(details, gin.H{"id": id, "name": name, "status": "skipped", "reason": "already new format"})
			continue
		}

		// 3. Re-chiffrer et sauvegarder
		migratedJSON, err := json.Marshal(migratedData)
		if err != nil {
			log.Printf("  ❌ Marshal error: %v", err)
			errors++
			continue
		}

		encryptedString, err := utils.Encrypt(migratedJSON)
		if err != nil {
			log.Printf("  ❌ Encrypt error: %v", err)
			errors++
			continue
		}

		newWrapper := EncryptedDataWrapper{Encrypted: encryptedString}
		storageJSON, err := json.Marshal(newWrapper)
		if err != nil {
			log.Printf("  ❌ Wrapper marshal error: %v", err)
			errors++
			continue
		}

		// 4. Mettre à jour en base
		updateQuery := `
			UPDATE budget_data
			SET data = $1, version = version + 1, updated_at = $2
			WHERE budget_id = $3
		`
		if _, err := h.DB.ExecContext(ctx, updateQuery, storageJSON, time.Now(), id); err != nil {
			log.Printf("  ❌ Update error: %v", err)
			errors++
			details = append(details, gin.H{"id": id, "name": name, "status": "error", "reason": "db update failed"})
			continue
		}

		// Extraire l'année cible pour le log
		targetYear := "unknown"
		if yd, ok := migratedData["yearlyData"].(map[string]interface{}); ok {
			for k := range yd {
				targetYear = k
				break
			}
		}

		log.Printf("  ✅ Migrated to year %s", targetYear)
		migrated++
		details = append(details, gin.H{"id": id, "name": name, "status": "migrated", "targetYear": targetYear})
	}

	return gin.H{
		"migrated": migrated,
		"skipped":  skipped,
		"errors":   errors,
		"details":  details,
	}, nil
}
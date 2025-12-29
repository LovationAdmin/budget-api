// migration/migrate_yearly_data.go
// Script de migration pour convertir l'ancien format yearlyData (noms de mois)
// vers le nouveau format (années avec tableaux indexés)
//
// USAGE:
// 1. Ajouter ce fichier dans budget-api/migration/
// 2. Appeler MigrateAllBudgets() depuis main.go ou un endpoint admin
// 3. Ou exécuter comme commande CLI: go run migration/migrate_yearly_data.go

package migration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/LovationAdmin/budget-api/utils"
)

// Mois français dans l'ordre (index 0 = Janvier)
var MONTHS = []string{
	"Janvier", "Février", "Mars", "Avril", "Mai", "Juin",
	"Juillet", "Août", "Septembre", "Octobre", "Novembre", "Décembre",
}

// Map pour normaliser les variantes de noms de mois (encodage UTF-8 cassé, etc.)
var MONTH_NAME_VARIANTS = map[string]string{
	// Standard
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
	// Encodage UTF-8 mojibake (caractères mal décodés)
	"FÃ©vrier":  "Février",
	"AoÃ»t":     "Août",
	"DÃ©cembre": "Décembre",
}

// Structure pour les données chiffrées en DB
type EncryptedData struct {
	Encrypted string `json:"encrypted"`
}

// Structure pour une année de données (nouveau format)
type YearData struct {
	Months          []map[string]interface{} `json:"months"`
	Expenses        []map[string]interface{} `json:"expenses"`
	MonthComments   []string                 `json:"monthComments"`
	ExpenseComments []map[string]interface{} `json:"expenseComments"`
	DeletedMonths   []int                    `json:"deletedMonths"`
}

// Normalise un nom de mois (gère les variantes d'encodage)
func normalizeMonthName(monthKey string) (string, bool) {
	if normalized, ok := MONTH_NAME_VARIANTS[monthKey]; ok {
		return normalized, true
	}
	return "", false
}

// Vérifie si une clé est un nom de mois
func isMonthName(key string) bool {
	_, ok := normalizeMonthName(key)
	return ok
}

// Retourne l'index d'un mois (0-11)
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

// Vérifie si yearlyData est dans l'ancien format (noms de mois comme clés)
func isLegacyFormat(yearlyData map[string]interface{}) bool {
	for key := range yearlyData {
		if isMonthName(key) {
			return true
		}
	}
	return false
}

// Vérifie si yearlyData est déjà dans le nouveau format (années avec .months)
func isNewFormat(yearlyData map[string]interface{}) bool {
	for key, value := range yearlyData {
		// Vérifie si la clé est une année (4 chiffres)
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

// Crée une structure d'année vide
func createEmptyYearData() YearData {
	return YearData{
		Months:          make([]map[string]interface{}, 12),
		Expenses:        make([]map[string]interface{}, 12),
		MonthComments:   make([]string, 12),
		ExpenseComments: make([]map[string]interface{}, 12),
		DeletedMonths:   []int{},
	}
}

// Migre les données d'un budget de l'ancien vers le nouveau format
func MigrateBudgetData(data map[string]interface{}) (map[string]interface{}, bool, error) {
	yearlyData, ok := data["yearlyData"].(map[string]interface{})
	if !ok || yearlyData == nil {
		return data, false, nil // Pas de yearlyData, rien à migrer
	}

	// Vérifier si déjà au nouveau format
	if isNewFormat(yearlyData) {
		log.Println("  → Données déjà au nouveau format, skip")
		return data, false, nil
	}

	// Vérifier si c'est l'ancien format
	if !isLegacyFormat(yearlyData) {
		log.Println("  → Format non reconnu, skip")
		return data, false, nil
	}

	log.Println("  → Ancien format détecté, migration en cours...")

	// Déterminer l'année cible
	targetYear := time.Now().Year()
	if cy, ok := data["currentYear"].(float64); ok {
		targetYear = int(cy)
	}
	yearKey := fmt.Sprintf("%d", targetYear)

	// Créer la nouvelle structure
	newYearlyData := make(map[string]interface{})
	newYearData := createEmptyYearData()

	// Initialiser les tableaux
	for i := 0; i < 12; i++ {
		newYearData.Months[i] = make(map[string]interface{})
		newYearData.Expenses[i] = make(map[string]interface{})
		newYearData.MonthComments[i] = ""
		newYearData.ExpenseComments[i] = make(map[string]interface{})
	}

	// Migrer yearlyData (allocations projets)
	for monthKey, monthData := range yearlyData {
		idx := getMonthIndex(monthKey)
		if idx >= 0 && idx < 12 {
			if md, ok := monthData.(map[string]interface{}); ok {
				// Vérifier que ce n'est pas une structure year-based accidentellement
				if _, hasMonths := md["months"]; !hasMonths {
					newYearData.Months[idx] = md
				}
			}
		}
	}

	// Migrer yearlyExpenses si présent
	if yearlyExpenses, ok := data["yearlyExpenses"].(map[string]interface{}); ok {
		for monthKey, expenseData := range yearlyExpenses {
			idx := getMonthIndex(monthKey)
			if idx >= 0 && idx < 12 {
				if ed, ok := expenseData.(map[string]interface{}); ok {
					newYearData.Expenses[idx] = ed
				}
			}
		}
	}

	// Migrer monthComments si présent
	if monthComments, ok := data["monthComments"].(map[string]interface{}); ok {
		for monthKey, comment := range monthComments {
			idx := getMonthIndex(monthKey)
			if idx >= 0 && idx < 12 {
				if c, ok := comment.(string); ok {
					newYearData.MonthComments[idx] = c
				}
			}
		}
	}

	// Migrer projectComments si présent
	if projectComments, ok := data["projectComments"].(map[string]interface{}); ok {
		for monthKey, comments := range projectComments {
			idx := getMonthIndex(monthKey)
			if idx >= 0 && idx < 12 {
				if pc, ok := comments.(map[string]interface{}); ok {
					newYearData.ExpenseComments[idx] = pc
				}
			}
		}
	}

	// Migrer oneTimeIncomes si présent (ancien format: { "Janvier": 500, ... })
	if oneTimeIncomes, ok := data["oneTimeIncomes"].(map[string]interface{}); ok {
		newOneTimeIncomes := make(map[string]interface{})
		newYearIncomes := make([]map[string]interface{}, 12)
		
		for i := 0; i < 12; i++ {
			newYearIncomes[i] = map[string]interface{}{"amount": 0, "description": ""}
		}

		for monthKey, income := range oneTimeIncomes {
			idx := getMonthIndex(monthKey)
			if idx >= 0 && idx < 12 {
				if amount, ok := income.(float64); ok {
					newYearIncomes[idx] = map[string]interface{}{
						"amount":      amount,
						"description": "",
					}
				}
			}
		}

		newOneTimeIncomes[yearKey] = newYearIncomes
		data["oneTimeIncomes"] = newOneTimeIncomes
	}

	// Convertir YearData en map pour JSON
	yearDataMap := map[string]interface{}{
		"months":          newYearData.Months,
		"expenses":        newYearData.Expenses,
		"monthComments":   newYearData.MonthComments,
		"expenseComments": newYearData.ExpenseComments,
		"deletedMonths":   newYearData.DeletedMonths,
	}
	newYearlyData[yearKey] = yearDataMap

	// Mettre à jour les données
	data["yearlyData"] = newYearlyData
	data["version"] = "2.3-migrated"
	data["lastUpdated"] = time.Now().Format(time.RFC3339)

	// Supprimer les anciens champs obsolètes
	delete(data, "yearlyExpenses")
	delete(data, "monthComments")
	delete(data, "projectComments")

	return data, true, nil
}

// MigrateBudgetRecord migre un enregistrement de la table budget_data
func MigrateBudgetRecord(ctx context.Context, db *sql.DB, budgetID string, rawJSON []byte) error {
	// 1. Décrypter si nécessaire
	var data map[string]interface{}
	var wrapper EncryptedData

	if err := json.Unmarshal(rawJSON, &wrapper); err == nil && wrapper.Encrypted != "" {
		// Données chiffrées
		decryptedBytes, err := utils.Decrypt(wrapper.Encrypted)
		if err != nil {
			return fmt.Errorf("failed to decrypt: %w", err)
		}
		if err := json.Unmarshal(decryptedBytes, &data); err != nil {
			return fmt.Errorf("failed to unmarshal decrypted data: %w", err)
		}
	} else {
		// Données non chiffrées (legacy)
		if err := json.Unmarshal(rawJSON, &data); err != nil {
			return fmt.Errorf("failed to unmarshal data: %w", err)
		}
	}

	// 2. Migrer les données
	migratedData, wasMigrated, err := MigrateBudgetData(data)
	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	if !wasMigrated {
		return nil // Rien à faire
	}

	// 3. Re-chiffrer et sauvegarder
	migratedJSON, err := json.Marshal(migratedData)
	if err != nil {
		return fmt.Errorf("failed to marshal migrated data: %w", err)
	}

	encryptedString, err := utils.Encrypt(migratedJSON)
	if err != nil {
		return fmt.Errorf("failed to encrypt: %w", err)
	}

	newWrapper := EncryptedData{Encrypted: encryptedString}
	storageJSON, err := json.Marshal(newWrapper)
	if err != nil {
		return fmt.Errorf("failed to marshal wrapper: %w", err)
	}

	// 4. Mettre à jour en base
	updateQuery := `
		UPDATE budget_data
		SET data = $1, version = version + 1, updated_at = $2
		WHERE budget_id = $3
	`
	_, err = db.ExecContext(ctx, updateQuery, storageJSON, time.Now(), budgetID)
	if err != nil {
		return fmt.Errorf("failed to update DB: %w", err)
	}

	log.Printf("  ✅ Budget %s migré avec succès", budgetID)
	return nil
}

// MigrateAllBudgets migre tous les budgets de la base de données
func MigrateAllBudgets(db *sql.DB) error {
	ctx := context.Background()

	log.Println("🚀 Démarrage de la migration des données budget...")
	log.Println("========================================")

	// Récupérer tous les budgets avec leurs données
	query := `
		SELECT bd.budget_id, bd.data, b.name
		FROM budget_data bd
		JOIN budgets b ON bd.budget_id = b.id
		ORDER BY bd.updated_at DESC
	`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query budgets: %w", err)
	}
	defer rows.Close()

	var migrated, skipped, errors int

	for rows.Next() {
		var budgetID string
		var rawJSON []byte
		var budgetName string

		if err := rows.Scan(&budgetID, &rawJSON, &budgetName); err != nil {
			log.Printf("❌ Erreur scan: %v", err)
			errors++
			continue
		}

		log.Printf("\n📦 Budget: %s (%s)", budgetName, budgetID)

		if err := MigrateBudgetRecord(ctx, db, budgetID, rawJSON); err != nil {
			log.Printf("  ❌ Erreur: %v", err)
			errors++
		} else {
			migrated++
		}
	}

	log.Println("\n========================================")
	log.Printf("📊 Résultat: %d migrés, %d skippés, %d erreurs", migrated, skipped, errors)
	log.Println("✅ Migration terminée!")

	return nil
}

// MigrateSingleBudget migre un seul budget par son ID
func MigrateSingleBudget(db *sql.DB, budgetID string) error {
	ctx := context.Background()

	query := `SELECT data FROM budget_data WHERE budget_id = $1`
	var rawJSON []byte

	if err := db.QueryRowContext(ctx, query, budgetID).Scan(&rawJSON); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("budget %s not found", budgetID)
		}
		return err
	}

	return MigrateBudgetRecord(ctx, db, budgetID, rawJSON)
}

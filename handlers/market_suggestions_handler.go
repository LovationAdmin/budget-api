// handlers/market_suggestions_handler.go
// ============================================================================
// MARKET SUGGESTIONS HANDLER - Analyse IA des charges pour trouver des économies
// ============================================================================
// VERSION CORRIGÉE : Suppression du struct dupliqué "CategorizeRequest"
// ============================================================================

package handlers

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LovationAdmin/budget-api/models"
	"github.com/LovationAdmin/budget-api/services"
	"github.com/LovationAdmin/budget-api/utils"
	"github.com/gin-gonic/gin"
)

// clampHouseholdParam mirrors services.bucketHouseholdSize so the
// /suggestions/category endpoint matches the cache key used elsewhere.
func clampHouseholdParam(raw string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(raw))
	if n < 1 {
		return 1
	}
	if n > 4 {
		return 4
	}
	return n
}

// ============================================================================
// HANDLER STRUCT
// ============================================================================

type MarketSuggestionsHandler struct {
	DB             *sql.DB
	MarketAnalyzer *services.MarketAnalyzerService
	WS             *WSHandler
	bulk           *bulkRegistry
}

func NewMarketSuggestionsHandler(db *sql.DB, analyzer *services.MarketAnalyzerService, ws *WSHandler) *MarketSuggestionsHandler {
	return &MarketSuggestionsHandler{
		DB:             db,
		MarketAnalyzer: analyzer,
		WS:             ws,
		bulk:           &bulkRegistry{runs: map[string]*bulkRun{}},
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
		if strings.Contains(labelLower, "habitation") || strings.Contains(labelLower, "maison") ||
			strings.Contains(labelLower, "logement") {
			return "INSURANCE_HOME"
		}
		if strings.Contains(labelLower, "santé") || strings.Contains(labelLower, "sante") ||
			strings.Contains(labelLower, "mutuelle") {
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
// POST /api/v1/suggestions/analyze          (connecté)
// POST /api/v1/public/suggestions/analyze   (simulateur public, limité par IP)
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

// singleAnalysisTimeout bounds one synchronous analysis (AI call + retries).
const singleAnalysisTimeout = 75 * time.Second

// isISOCode reports whether s is an n-letter code (country / currency).
func isISOCode(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

// marketErrorResponse maps an analysis failure to a status, code and French
// message (the provider's raw error never reaches the client).
func marketErrorResponse(err error) (int, gin.H) {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, gin.H{
			"error": "L'analyse a pris trop de temps. Réessayez dans un instant.",
			"code":  "ai_timeout",
		}
	case services.IsTransientAIError(err):
		return http.StatusServiceUnavailable, gin.H{
			"error": "Le service IA est très sollicité en ce moment. Réessayez dans une minute.",
			"code":  "ai_busy",
		}
	default:
		return http.StatusServiceUnavailable, gin.H{
			"error": "Le service d'analyse est momentanément indisponible. Réessayez plus tard.",
			"code":  "ai_unavailable",
		}
	}
}

func (h *MarketSuggestionsHandler) AnalyzeCharge(c *gin.Context) {
	var req AnalyzeChargeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Requête invalide : catégorie et montant requis."})
		return
	}

	category := strings.ToUpper(strings.TrimSpace(req.Category))
	if !h.isSuggestionRelevant(category) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cette catégorie n'est pas encore prise en charge.", "code": "unsupported_category"})
		return
	}
	if math.IsNaN(req.Amount) || req.Amount <= 0 || req.Amount > 100000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Indiquez un montant mensuel valide.", "code": "invalid_amount"})
		return
	}

	// Defaults
	country := strings.ToUpper(strings.TrimSpace(req.Country))
	if !isISOCode(country, 2) {
		country = "FR"
	}
	currency := strings.ToUpper(strings.TrimSpace(req.Currency))
	if !isISOCode(currency, 3) {
		currency = "EUR"
	}
	householdSize := req.HouseholdSize
	if householdSize < 1 {
		householdSize = 1
	}
	if householdSize > 20 {
		householdSize = 20
	}

	// ✅ LOGGING SÉCURISÉ - Pas de montant ni données personnelles
	utils.LogAIAnalysis("SingleAnalyze", category, country, 1)

	ctx, cancel := context.WithTimeout(c.Request.Context(), singleAnalysisTimeout)
	defer cancel()

	suggestion, _, err := h.MarketAnalyzer.Analyze(ctx, services.ChargeAnalysis{
		Category:      category,
		MerchantName:  req.MerchantName,
		Amount:        req.Amount,
		Country:       country,
		Currency:      currency,
		HouseholdSize: householdSize,
		Description:   req.Description,
	})

	if err != nil {
		utils.SafeError("Single charge analysis failed: %v", err)
		status, body := marketErrorResponse(err)
		c.JSON(status, body)
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
	country := strings.ToUpper(c.DefaultQuery("country", "FR"))
	currency := strings.ToUpper(c.DefaultQuery("currency", "EUR"))
	householdSize := clampHouseholdParam(c.DefaultQuery("household_size", "1"))

	// ✅ LOGGING SÉCURISÉ
	utils.SafeInfo("Fetching cached suggestions for %s in %s/%s (hh=%d)", category, country, currency, householdSize)

	// Check cache via the analyzer so we share the exact cache key shape used
	// when writing entries (avoids two readers diverging on bucket logic).
	suggestion, err := h.MarketAnalyzer.GetCachedSuggestion(c.Request.Context(), category, country, currency, householdSize, "")

	if err == sql.ErrNoRows || suggestion == nil {
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

	// Cached offers carry no savings (they depend on the household's amount):
	// computed when ?amount= is given, otherwise cheapest first.
	if amount, err := strconv.ParseFloat(c.Query("amount"), 64); err == nil && amount > 0 {
		h.MarketAnalyzer.PriceForHousehold(suggestion, amount, householdSize)
	} else {
		sort.SliceStable(suggestion.Competitors, func(a, b int) bool {
			return suggestion.Competitors[a].TypicalPrice < suggestion.Competitors[b].TypicalPrice
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"suggestion": suggestion,
		"cached":     true,
	})
}

// ============================================================================
// 3. BULK ANALYZE ALL CHARGES IN A BUDGET (ASYNC)
// POST /api/v1/budgets/:id/suggestions/bulk-analyze
// ----------------------------------------------------------------------------
// Répond 202 tout de suite avec le nombre de charges à analyser, puis diffuse
// sur le WebSocket du budget :
//   - suggestions_progress {done, total, failed, item?} après chaque charge
//     (les résultats en cache arrivent en quelques ms) ;
//   - suggestions_ready {..., total, failed_count, status} à la fin.
// Les charges sont analysées en parallèle (MARKET_CONCURRENCY, 4 par défaut).
// Une analyse identique déjà en cours n'est pas relancée ; une analyse
// différente pour le même budget remplace la précédente.
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
	// Force restarts the analysis even if the same one is running (the
	// client's "Relancer" button, e.g. when a run seems stuck).
	Force bool `json:"force"`
}

// bulkRunTimeout bounds a whole bulk analysis.
const bulkRunTimeout = 4 * time.Minute

// bulkChargeTimeout bounds the analysis of one charge within a bulk run.
const bulkChargeTimeout = 75 * time.Second

func bulkConcurrency() int {
	if n, err := strconv.Atoi(os.Getenv("MARKET_CONCURRENCY")); err == nil && n > 0 {
		return n
	}
	return 4
}

// bulkRegistry tracks the running bulk analysis of each budget.
type bulkRegistry struct {
	mu   sync.Mutex
	runs map[string]*bulkRun
}

type bulkRun struct {
	signature string
	cancel    context.CancelFunc
}

// start registers a run for the budget. When the same analysis is already
// running (and force is false) it returns running=true and no context;
// otherwise any previous run of the budget is cancelled (its results would
// be stale or it is being restarted) and finish must be called when the new
// run ends.
func (r *bulkRegistry) start(budgetID, signature string, force bool) (ctx context.Context, finish func(), running bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if prev, ok := r.runs[budgetID]; ok {
		if prev.signature == signature && !force {
			return nil, nil, true
		}
		prev.cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), bulkRunTimeout)
	run := &bulkRun{signature: signature, cancel: cancel}
	r.runs[budgetID] = run
	return ctx, func() {
		cancel()
		r.mu.Lock()
		if r.runs[budgetID] == run {
			delete(r.runs, budgetID)
		}
		r.mu.Unlock()
	}, false
}

type bulkJob struct {
	Charge   ChargeToAnalyze
	Category string
}

func bulkSignature(jobs []bulkJob, householdSize int, country, currency string) string {
	data, _ := json.Marshal(struct {
		Jobs      []bulkJob
		Household int
		Country   string
		Currency  string
	}{jobs, householdSize, country, currency})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
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

	householdSize := req.HouseholdSize
	if householdSize < 1 {
		householdSize = 1
	}

	country, currency, err := h.getBudgetConfig(c.Request.Context(), budgetID)
	if err != nil {
		utils.SafeWarn("Could not fetch budget config, using defaults")
		country, currency = "FR", "EUR"
	}

	// 2. Eligible charges, known up front so the client can show "n / total".
	jobs := []bulkJob{}
	for _, charge := range req.Charges {
		// Vérifier/corriger la catégorie
		category := strings.ToUpper(charge.Category)
		if category == "LEISURE" || category == "OTHER" || category == "" {
			if refined := determineCategory(charge.Label); refined != "OTHER" && refined != "LEISURE" {
				category = refined
			}
		}
		if charge.Amount > 0 && h.isSuggestionRelevant(category) {
			jobs = append(jobs, bulkJob{Charge: charge, Category: category})
		}
	}

	// ✅ LOGGING SÉCURISÉ - Pas d'ID complet ni de montants
	utils.LogBudgetAction("BulkAnalyze-Start", budgetID, userID)
	utils.SafeInfo("Bulk analysis requested for %d charges (%d eligible)", len(req.Charges), len(jobs))

	ctx, finish, running := h.bulk.start(budgetID, bulkSignature(jobs, householdSize, country, currency), req.Force)
	if running {
		c.JSON(http.StatusAccepted, gin.H{
			"message": "Analysis already running",
			"status":  "already_running",
			"total":   len(jobs),
		})
		return
	}

	// 3. Respond IMMEDIATELY to prevent timeout (HTTP 202 Accepted)
	c.JSON(http.StatusAccepted, gin.H{
		"message": "Analysis started in background",
		"status":  "processing",
		"total":   len(jobs),
	})

	go func() {
		defer finish()
		h.runBulkAnalysis(ctx, budgetID, userID, jobs, householdSize, country, currency)
	}()
}

func (h *MarketSuggestionsHandler) runBulkAnalysis(ctx context.Context, budgetID, userID string, jobs []bulkJob, householdSize int, country, currency string) {
	// ✅ LOGGING SÉCURISÉ
	utils.LogAIAnalysis("BulkAnalyze-Process", "MULTIPLE", country, len(jobs))

	var (
		mu        sync.Mutex
		wg        sync.WaitGroup
		results   = make([]*models.ChargeSuggestion, len(jobs))
		done      int
		failed    int
		cacheHits int
		aiCalls   int
	)
	sem := make(chan struct{}, bulkConcurrency())

	for i, job := range jobs {
		wg.Add(1)
		go func(i int, job bulkJob) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			chargeCtx, cancel := context.WithTimeout(ctx, bulkChargeTimeout)
			suggestion, fromCache, err := h.MarketAnalyzer.Analyze(chargeCtx, services.ChargeAnalysis{
				Category:      job.Category,
				MerchantName:  job.Charge.MerchantName,
				Amount:        job.Charge.Amount,
				Country:       country,
				Currency:      currency,
				HouseholdSize: householdSize,
				Description:   job.Charge.Description,
			})
			cancel()

			// Progress is broadcast under the lock so clients see it in order.
			mu.Lock()
			defer mu.Unlock()
			if errors.Is(ctx.Err(), context.Canceled) {
				return // superseded by a newer analysis: its results are stale
			}
			done++
			var item *models.ChargeSuggestion
			switch {
			case err != nil:
				failed++
				utils.SafeWarn("Failed to analyze charge: %v", err)
			default:
				if fromCache {
					cacheHits++
				} else {
					aiCalls++
				}
				if len(suggestion.Competitors) > 0 {
					item = &models.ChargeSuggestion{
						ChargeID:    job.Charge.ID,
						ChargeLabel: job.Charge.Label,
						Suggestion:  suggestion,
					}
					results[i] = item
				}
			}
			h.broadcast(budgetID, "suggestions_progress", map[string]interface{}{
				"done":   done,
				"total":  len(jobs),
				"failed": failed,
				"item":   item,
			})
		}(i, job)
	}
	wg.Wait()

	if errors.Is(ctx.Err(), context.Canceled) {
		utils.SafeInfo("Bulk analysis superseded by a newer one")
		return
	}
	// Timed out: report what was found; charges never analyzed count as failed.
	failed += len(jobs) - done

	suggestions := []models.ChargeSuggestion{}
	totalSavings := 0.0
	for _, item := range results {
		if item != nil {
			suggestions = append(suggestions, *item)
			totalSavings += item.Suggestion.Competitors[0].PotentialSavings
		}
	}
	// Biggest savings first.
	sort.SliceStable(suggestions, func(a, b int) bool {
		return suggestions[a].Suggestion.Competitors[0].PotentialSavings > suggestions[b].Suggestion.Competitors[0].PotentialSavings
	})

	status := "ok"
	if failed > 0 && failed == len(jobs) {
		status = "failed"
	} else if failed > 0 {
		status = "partial"
	}

	// ✅ LOGGING SÉCURISÉ - Pas de montant total exact
	utils.SafeInfo("Bulk analysis complete: %d charges, %d suggestions, %d failed (%d cached, %d AI)",
		len(jobs), len(suggestions), failed, cacheHits, aiCalls)
	utils.LogBudgetAction("BulkAnalyze-Complete", budgetID, userID)

	// 4. Notify Frontend via WebSocket
	h.broadcast(budgetID, "suggestions_ready", map[string]interface{}{
		"suggestions":             suggestions,
		"total_potential_savings": math.Round(totalSavings*100) / 100,
		"household_size":          householdSize,
		"cache_hits":              cacheHits,
		"ai_calls_made":           aiCalls,
		"currency":                currency,
		"total":                   len(jobs),
		"failed_count":            failed,
		"status":                  status,
	})
}

func (h *MarketSuggestionsHandler) broadcast(budgetID, msgType string, data interface{}) {
	if h.WS == nil {
		return
	}
	h.WS.BroadcastJSON(budgetID, map[string]interface{}{"type": msgType, "data": data})
}

// ============================================================================
// 4. CATEGORIZE TRANSACTION LABEL
// POST /api/v1/categorize
// ============================================================================

// Struct supprimé ici pour éviter la redeclaration (déjà dans categorization.go)

func (h *MarketSuggestionsHandler) CategorizeLabel(c *gin.Context) {
	var req CategorizeRequest // Utilise le struct défini dans handlers/categorization.go
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

// ============================================================================
// 5. CLEAN CACHE (ADMIN)
// POST /api/v1/admin/suggestions/clean-cache
// ============================================================================

func (h *MarketSuggestionsHandler) CleanExpiredCache(c *gin.Context) {
	if err := h.MarketAnalyzer.CleanExpiredCache(c.Request.Context()); err != nil {
		utils.SafeWarn("Failed to clean cache: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clean cache"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Cache cleaned successfully"})
}

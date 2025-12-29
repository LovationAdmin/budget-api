package models

import "time"

// ============================================================================
// ANCIENNES STRUCTURES - GARDER POUR COMPATIBILITÉ
// ============================================================================

// Suggestion représente une suggestion d'économie simple (ancien système)
type Suggestion struct {
	ID               string  `json:"id"`
	ChargeID         string  `json:"charge_id"`
	Type             string  `json:"type"`
	Title            string  `json:"title"`
	Message          string  `json:"message"`
	PotentialSavings float64 `json:"potential_savings"`
	ActionLink       string  `json:"action_link"`
	CanBeContacted   bool    `json:"can_be_contacted"`
}

// ============================================================================
// NOUVELLES STRUCTURES - Système avancé avec recherche de concurrents
// ============================================================================

// Competitor représente un concurrent avec détails complets
type Competitor struct {
	Name             string   `json:"name"`
	TypicalPrice     float64  `json:"typical_price"`
	BestOffer        string   `json:"best_offer"`
	PotentialSavings float64  `json:"potential_savings"`
	AffiliateLink    string   `json:"affiliate_link,omitempty"`
	Pros             []string `json:"pros"`
	Cons             []string `json:"cons"`
	ContactAvailable bool     `json:"contact_available"`
	PhoneNumber      string   `json:"phone_number,omitempty"`
	ContactEmail     string   `json:"contact_email,omitempty"`
}

// MarketSuggestion représente une suggestion de marché avec liste de concurrents
type MarketSuggestion struct {
	ID           string       `json:"id"`
	Category     string       `json:"category"`
	Country      string       `json:"country"`
	MerchantName string       `json:"merchant_name,omitempty"`
	Competitors  []Competitor `json:"competitors"`
	LastUpdated  time.Time    `json:"last_updated"`
	ExpiresAt    time.Time    `json:"expires_at"`
}

// ChargeSuggestion associe une charge avec sa suggestion de marché
type ChargeSuggestion struct {
	ChargeID    string           `json:"charge_id"`
	ChargeLabel string           `json:"charge_label"`
	Suggestion  *MarketSuggestion `json:"suggestion"`
}

// BulkAnalyzeResponse retourne les résultats d'une analyse bulk
type BulkAnalyzeResponse struct {
	Suggestions           []ChargeSuggestion `json:"suggestions"`
	CacheHits             int                `json:"cache_hits"`
	AICallsMade           int                `json:"ai_calls_made"`
	TotalPotentialSavings float64            `json:"total_potential_savings"`
	HouseholdSize         int                `json:"household_size"` // NEW: Actual household size used for calculations
}
package models

import "time"

// ============================================================================
// ANCIENNES STRUCTURES - GARDER POUR COMPATIBILITÉ
// ============================================================================

// Suggestion représente une suggestion d'économie simple (ancien système)
type Suggestion struct {
	ID               string  `json:"id"`
	ChargeID         string  `json:"charge_id"`         // L'ID de la dépense concernée
	Type             string  `json:"type"`              // ex: "MOBILE_OFFER", "ENERGY_OFFER"
	Title            string  `json:"title"`             // ex: "Forfait mobile élevé"
	Message          string  `json:"message"`           // ex: "Vous payez 70€/mois. La moyenne est à 20€."
	PotentialSavings float64 `json:"potential_savings"` // ex: 600.00 (par an)
	ActionLink       string  `json:"action_link"`       // Lien d'affiliation direct (ex: Sosh)
	CanBeContacted   bool    `json:"can_be_contacted"`  // Si TRUE, affiche le bouton "Être rappelé"
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
	ChargeID    string            `json:"charge_id"`
	ChargeLabel string            `json:"charge_label"`
	Suggestion  *MarketSuggestion `json:"suggestion"`
}

// BulkAnalyzeResponse retourne les résultats d'une analyse bulk
type BulkAnalyzeResponse struct {
	Suggestions           []ChargeSuggestion `json:"suggestions"`
	CacheHits             int                `json:"cache_hits"`
	AICallsMade           int                `json:"ai_calls_made"`
	TotalPotentialSavings float64            `json:"total_potential_savings"`
}
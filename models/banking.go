package models

import (
	"time"
)

type BankConnection struct {
	ID                   string        `json:"id"`
	UserID               string        `json:"user_id"`
	InstitutionID        string        `json:"institution_id"`
	InstitutionName      string        `json:"institution_name"`
	ProviderConnectionID string        `json:"-"` // Internal use only
	Status               string        `json:"status"`
	ExpiresAt            time.Time     `json:"expires_at"`
	CreatedAt            time.Time     `json:"created_at"`
	UpdatedAt            time.Time     `json:"updated_at"`
	Accounts             []BankAccount `json:"accounts,omitempty"`
}

type BankAccount struct {
	ID                string    `json:"id"`
	ConnectionID      string    `json:"connection_id"`
	ExternalAccountID string    `json:"-"` // Internal use only
	Name              string    `json:"name"`
	Mask              string    `json:"mask"`
	Currency          string    `json:"currency"`
	Balance           float64   `json:"balance"`
	IsSavingsPool     bool      `json:"is_savings_pool"` // Critical for Reality Check
	LastSyncedAt      time.Time `json:"last_synced_at"`
}

// Request to toggle the pool status
type UpdateAccountPoolRequest struct {
	IsSavingsPool bool `json:"is_savings_pool"`
}

// Response for Reality Check
type RealityCheckSummary struct {
	TotalRealCash float64       `json:"total_real_cash"`
	Accounts      []BankAccount `json:"accounts"`
}
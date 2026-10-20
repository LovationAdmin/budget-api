// models/budget.go
// ✅ VERSION CORRIGÉE - Ajout Location et Currency

package models

import (
	"encoding/json"
	"time"
)

type Budget struct {
	ID        string    `json:"id"`
	Name      string    `json:"name" binding:"required"`
	OwnerID   string    `json:"owner_id"`
	Location  string    `json:"location"`   // ✅ NOUVEAU
	Currency  string    `json:"currency"`   // ✅ NOUVEAU
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	IsOwner   bool      `json:"is_owner"`
	OwnerName string    `json:"owner_name"`
	Members   []BudgetMember `json:"members"`
}

type BudgetMember struct {
	ID          string          `json:"id"`
	BudgetID    string          `json:"budget_id"`
	UserID      string          `json:"user_id"`
	User        *User           `json:"user,omitempty"`
	Role        string          `json:"role"`
	Permissions json.RawMessage `json:"permissions"`
	JoinedAt    time.Time       `json:"joined_at"`
	UserName    string          `json:"user_name"`
	UserEmail   string          `json:"user_email"`
}

type BudgetData struct {
	ID        string          `json:"id"`
	BudgetID  string          `json:"budget_id"`
	Data      json.RawMessage `json:"data"`
	Version   int             `json:"version"`
	UpdatedBy string          `json:"updated_by"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type CreateBudgetRequest struct {
	Name     string `json:"name" binding:"required"`
	Year     int    `json:"year"`
	Location string `json:"location"` // ✅ NOUVEAU
	Currency string `json:"currency"` // ✅ NOUVEAU
}

type UpdateBudgetDataRequest struct {
	Data json.RawMessage `json:"data" binding:"required"`
}
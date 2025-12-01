package models

import (
	"encoding/json"
	"time"
)

type Budget struct {
	ID        string    `json:"id"`
	Name      string    `json:"name" binding:"required"`
	OwnerID   string    `json:"owner_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type BudgetMember struct {
	ID          string          `json:"id"`
	BudgetID    string          `json:"budget_id"`
	UserID      string          `json:"user_id"`
	User        *User           `json:"user,omitempty"`
	Role        string          `json:"role"`
	Permissions json.RawMessage `json:"permissions"`
	JoinedAt    time.Time       `json:"joined_at"`
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
	Name string `json:"name" binding:"required"`
}

type UpdateBudgetDataRequest struct {
	Data json.RawMessage `json:"data" binding:"required"`
}
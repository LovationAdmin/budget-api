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
	IsOwner   bool          `json:"is_owner"`   // To store the CASE statement result
	OwnerName string        `json:"owner_name"` // To store the user.name from the JOIN
	Members   []BudgetMember `json:"members"`    // To hold the list of members
}

type BudgetMember struct {
	ID          string          `json:"id"`
	BudgetID    string          `json:"budget_id"`
	UserID      string          `json:"user_id"`
	User        *User           `json:"user,omitempty"`
	Role        string          `json:"role"`
	Permissions json.RawMessage `json:"permissions"`
	JoinedAt    time.Time       `json:"joined_at"`
	UserName  string `json:"user_name"`  // To store u.name from the JOIN (line 236 in service)
	UserEmail string `json:"user_email"` // To store u.email from the JOIN (line 237 in service)
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
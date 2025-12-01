package models

import (
	"time"
)

type Invitation struct {
	ID        string    `json:"id"`
	BudgetID  string    `json:"budget_id"`
	Email     string    `json:"email" binding:"required,email"`
	InvitedBy string    `json:"invited_by"`
	Token     string    `json:"token"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

type InvitationRequest struct {
	Email string `json:"email" binding:"required,email"`
}

type AcceptInvitationRequest struct {
	Token string `json:"token" binding:"required"`
}

type InvitationResponse struct {
	Invitation Invitation `json:"invitation"`
	Budget     Budget     `json:"budget"`
	InviterName string    `json:"inviter_name"`
}
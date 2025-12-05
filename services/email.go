package services

import (
	"budget-api/utils" // Import your utility package
)

// EmailService will hold any dependencies needed for email operations.
// It currently doesn't need any, but it's good practice to define it.
type EmailService struct {
	// No fields needed for now
}

// NewEmailService creates a new instance of EmailService.
func NewEmailService() *EmailService {
	return &EmailService{}
}

// SendInvitation is the method that the handler calls.
// It acts as a wrapper for the actual utility function in utils/email.go.
func (s *EmailService) SendInvitation(toEmail, inviterName, budgetName, invitationToken string) error {
	// Call the utility function that contains the actual logic
	return utils.SendInvitationEmail(toEmail, inviterName, budgetName, invitationToken)
}
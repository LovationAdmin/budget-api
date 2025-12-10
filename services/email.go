package services

import (
	"budget-api/utils"
)

// EmailService struct
type EmailService struct {
}

// NewEmailService constructor
func NewEmailService() *EmailService {
	return &EmailService{}
}

// SendInvitation wrapper qui appelle la fonction dans utils
func (s *EmailService) SendInvitation(toEmail, inviterName, budgetName, invitationToken string) error {
	return utils.SendInvitationEmail(toEmail, inviterName, budgetName, invitationToken)
}

// SendVerification wrapper qui appelle la fonction dans utils
func (s *EmailService) SendVerification(toEmail, userName, token string) error {
	return utils.SendVerificationEmail(toEmail, userName, token)
}
package services

import (
	"time"

	"github.com/LovationAdmin/budget-api/utils"
)

// EmailService struct
type EmailService struct {
}

// NewEmailService constructor
func NewEmailService() *EmailService {
	return &EmailService{}
}

// SendInvitation wrapper calling utils
func (s *EmailService) SendInvitation(toEmail, inviterName, budgetName, invitationToken, locale string) error {
	return utils.SendInvitationEmail(toEmail, inviterName, budgetName, invitationToken, locale)
}

// SendVerificationEmail wrapper calling utils
// Renamed from SendVerification to match handlers/auth.go
func (s *EmailService) SendVerificationEmail(toEmail, userName, token, locale string) error {
	return utils.SendVerificationEmail(toEmail, userName, token, locale)
}

// SendUnverifiedExpiryEmail wrapper calling utils
func (s *EmailService) SendUnverifiedExpiryEmail(toEmail, userName, token, locale string, deleteFrom time.Time) error {
	return utils.SendUnverifiedExpiryEmail(toEmail, userName, token, locale, deleteFrom)
}

// SendPasswordResetEmail wrapper calling utils
// Added this method as it was missing but called in handlers/auth.go
func (s *EmailService) SendPasswordResetEmail(toEmail, userName, resetToken, locale string) error {
	return utils.SendPasswordResetEmail(toEmail, userName, resetToken, locale)
}

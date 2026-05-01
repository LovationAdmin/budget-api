// services/email_campaign.go
// ============================================================================
// CAMPAIGN EMAIL SERVICE METHOD
// ============================================================================
// Extends the existing EmailService with re-engagement campaign support.
// Lives in a separate file from email.go to keep the existing contract
// (SendInvitation / SendVerificationEmail / SendPasswordResetEmail) intact.
// ============================================================================

package services

import (
	"github.com/LovationAdmin/budget-api/utils"
)

// SendReengagementEmail renders + sends one re-engagement email via Resend.
// Returns the Resend message ID on success (may be empty if Resend's response
// body couldn't be decoded; the email is still sent in that case).
//
// variant must be one of:
//   - utils.CampaignReengagementVerified   (founder note for verified users)
//   - utils.CampaignReengagementUnverified (relance for unverified users)
func (s *EmailService) SendReengagementEmail(toEmail, userName, campaignID string, variant utils.CampaignVariant) (string, error) {
	subject, html, err := utils.RenderCampaignEmail(variant, userName, campaignID)
	if err != nil {
		return "", err
	}
	return utils.SendCampaignEmail(toEmail, subject, html)
}

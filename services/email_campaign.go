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
//
// The email carries a one-click unsubscribe from the product news of userID.
func (s *EmailService) SendReengagementEmail(toEmail, userID, userName, campaignID string, variant utils.CampaignVariant) (string, error) {
	subject, html, err := utils.RenderCampaignEmail(variant, userName, campaignID)
	if err != nil {
		return "", err
	}
	links := utils.UnsubscribeLinksFor(userID, utils.EmailListNews)
	return utils.SendCampaign(utils.CampaignMessage{
		To: toEmail, Subject: subject, HTML: html,
		Unsubscribe: &links, Ref: campaignID + ":" + userID,
	})
}

// SendMonthlyRecapEmail renders + sends the monthly recap for a single user.
// `recap` is the *RecapData built by MonthlyRecapService; we pass it as
// interface{} into utils.RenderMonthlyRecapEmail to avoid a circular import.
// `msg` carries the recipient's unsubscribe links and the message reference.
func (s *EmailService) SendMonthlyRecapEmail(toEmail string, locale, monthLabel, budgetName string, recap interface{}, msg utils.CampaignMessage) (string, error) {
	subject, html, err := utils.RenderMonthlyRecapEmail(locale, monthLabel, budgetName, recap)
	if err != nil {
		return "", err
	}
	msg.To, msg.Subject, msg.HTML = toEmail, subject, html
	return utils.SendCampaign(msg)
}

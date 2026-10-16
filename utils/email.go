package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
)

// ============================================================================
// STRUCTS & TYPES
// ============================================================================

type EmailRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
}

// ============================================================================
// EXISTING FUNCTIONS (Preserved - No Regression)
// ============================================================================

// The words of the account emails in each language (utils/locale.go). The
// French ones are the historical texts.
type accountEmailText struct {
	InviteTitle, InviteHeading, InviteText, InviteButton string
	InviteSubject                                        func(inviter string) string

	VerifyTitle, VerifyHeading, VerifyText, VerifyButton, VerifySubject string

	ResetTitle, ResetHello, ResetIntro, ResetAsk, ResetButton, ResetValidity, ResetValidityTime,
	ResetCopy, ResetNotYou, ResetIgnore, ResetFooter, ResetAuto, ResetSubject string
}

var accountEmails = map[string]accountEmailText{
	LocaleFR: {
		InviteTitle:   "Invitation Budget",
		InviteHeading: "Invitation à collaborer",
		InviteText:    "vous invite à rejoindre le budget",
		InviteButton:  "Accepter l'invitation",
		InviteSubject: func(inviter string) string { return fmt.Sprintf("%s vous invite à collaborer", inviter) },

		VerifyTitle:   "Vérification Email",
		VerifyHeading: "Bienvenue %s ! 👋",
		VerifyText:    "Veuillez vérifier votre email pour activer votre compte Budget Famille.",
		VerifyButton:  "Vérifier mon email",
		VerifySubject: "Vérifiez votre compte Budget Famille",

		ResetTitle:        "Réinitialisation de mot de passe",
		ResetHello:        "Bonjour",
		ResetIntro:        "Nous avons reçu une demande de réinitialisation de mot de passe pour votre compte Budget Famille.",
		ResetAsk:          "Si vous êtes à l'origine de cette demande, cliquez sur le bouton ci-dessous pour définir un nouveau mot de passe :",
		ResetButton:       "Réinitialiser mon mot de passe",
		ResetValidity:     "Ce lien est valide pendant",
		ResetValidityTime: "1 heure",
		ResetCopy:         "Si le bouton ne fonctionne pas, copiez et collez ce lien dans votre navigateur :",
		ResetNotYou:       "⚠️ Vous n'avez pas demandé cette réinitialisation ?",
		ResetIgnore:       "Ignorez simplement cet email. Votre mot de passe actuel reste inchangé.",
		ResetFooter:       "Budget Famille - Gestion budgétaire familiale sécurisée",
		ResetAuto:         "Cet email a été envoyé automatiquement, merci de ne pas y répondre.",
		ResetSubject:      "Réinitialisation de votre mot de passe Budget Famille",
	},
	LocaleEN: {
		InviteTitle:   "Budget invitation",
		InviteHeading: "You’re invited to a shared budget",
		InviteText:    "invites you to join the budget",
		InviteButton:  "Accept the invitation",
		InviteSubject: func(inviter string) string { return fmt.Sprintf("%s invites you to their budget", inviter) },

		VerifyTitle:   "Confirm your email",
		VerifyHeading: "Welcome, %s! 👋",
		VerifyText:    "Please confirm your email address to activate your Budget Famille account.",
		VerifyButton:  "Confirm my email",
		VerifySubject: "Confirm your Budget Famille account",

		ResetTitle:        "Password reset",
		ResetHello:        "Hello",
		ResetIntro:        "We received a request to reset the password of your Budget Famille account.",
		ResetAsk:          "If you made this request, click the button below to set a new password:",
		ResetButton:       "Reset my password",
		ResetValidity:     "This link is valid for",
		ResetValidityTime: "1 hour",
		ResetCopy:         "If the button doesn’t work, copy and paste this link into your browser:",
		ResetNotYou:       "⚠️ Didn’t ask for a password reset?",
		ResetIgnore:       "Just ignore this email. Your current password stays the same.",
		ResetFooter:       "Budget Famille - Secure budgeting for the whole household",
		ResetAuto:         "This email was sent automatically, please don’t reply.",
		ResetSubject:      "Reset your Budget Famille password",
	},
}

func accountEmailIn(locale string) accountEmailText {
	if t, ok := accountEmails[NormalizeLocale(locale)]; ok {
		return t
	}
	return accountEmails[LocaleFR]
}

func renderEmail(name, tmpl string, data interface{}) (string, error) {
	t, err := template.New(name).Parse(tmpl)
	if err != nil {
		return "", err
	}
	var body bytes.Buffer
	if err := t.Execute(&body, data); err != nil {
		return "", err
	}
	return body.String(), nil
}

const invitationEmailTemplate = `
<!DOCTYPE html>
<html lang="{{.Lang}}">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.T.InviteTitle}}</title>
</head>
<body style="margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background-color: #f3f4f6;">
    <table role="presentation" style="width: 100%; border-collapse: collapse;">
        <tr>
            <td style="padding: 40px 0; text-align: center; background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);">
                <h1 style="margin: 0; color: #ffffff; font-size: 28px; font-weight: bold;">
                    💰 Budget Famille
                </h1>
            </td>
        </tr>
        <tr>
            <td style="padding: 40px 20px;">
                <table role="presentation" style="max-width: 600px; margin: 0 auto; background-color: #ffffff; border-radius: 12px; box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);">
                    <tr>
                        <td style="padding: 40px;">
                            <h2 style="margin: 0 0 20px 0; color: #1f2937; font-size: 24px;">{{.T.InviteHeading}}</h2>
                            <p style="margin: 0 0 20px 0; color: #4b5563; font-size: 16px; line-height: 1.6;">
                                <strong>{{.Inviter}}</strong> {{.T.InviteText}} <strong>"{{.Budget}}"</strong>.
                            </p>
                            <table role="presentation" style="margin: 20px 0;">
                                <tr>
                                    <td style="border-radius: 8px; background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);">
                                        <a href="{{.Link}}" style="display: inline-block; padding: 16px 32px; color: #ffffff; text-decoration: none; font-size: 16px; font-weight: 600;">
                                            {{.T.InviteButton}}
                                        </a>
                                    </td>
                                </tr>
                            </table>
                        </td>
                    </tr>
                </table>
            </td>
        </tr>
    </table>
</body>
</html>
    `

// SendInvitationEmail envoie l'email d'invitation, dans la langue donnée
// (celle de la personne qui invite).
func SendInvitationEmail(toEmail, inviterName, budgetName, invitationToken, locale string) error {
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:3000"
	}

	t := accountEmailIn(locale)
	htmlBody, err := renderEmail("invitation", invitationEmailTemplate, map[string]interface{}{
		"Lang":    PickLocale(locale),
		"T":       t,
		"Inviter": inviterName,
		"Budget":  budgetName,
		"Link":    fmt.Sprintf("%s/invitation/accept?token=%s", frontendURL, invitationToken),
	})
	if err != nil {
		log.Printf("❌ Error rendering invitation email: %v", err)
		return err
	}

	return sendEmail(toEmail, t.InviteSubject(inviterName), htmlBody)
}

const verificationEmailTemplate = `
<!DOCTYPE html>
<html lang="{{.Lang}}">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.T.VerifyTitle}}</title>
</head>
<body style="margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background-color: #f3f4f6;">
    <table role="presentation" style="width: 100%; border-collapse: collapse;">
        <tr>
            <td style="padding: 40px 0; text-align: center; background: linear-gradient(135deg, #10b981 0%, #059669 100%);">
                <h1 style="margin: 0; color: #ffffff; font-size: 28px; font-weight: bold;">
                    💰 Budget Famille
                </h1>
            </td>
        </tr>
        <tr>
            <td style="padding: 40px 20px;">
                <table role="presentation" style="max-width: 600px; margin: 0 auto; background-color: #ffffff; border-radius: 12px; box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);">
                    <tr>
                        <td style="padding: 40px;">
                            <h2 style="margin: 0 0 20px 0; color: #1f2937; font-size: 24px;">{{.Heading}}</h2>
                            <p style="margin: 0 0 20px 0; color: #4b5563; font-size: 16px; line-height: 1.6;">
                                {{.T.VerifyText}}
                            </p>
                            <table role="presentation" style="margin: 20px 0;">
                                <tr>
                                    <td style="border-radius: 8px; background: linear-gradient(135deg, #10b981 0%, #059669 100%);">
                                        <a href="{{.Link}}" style="display: inline-block; padding: 16px 32px; color: #ffffff; text-decoration: none; font-size: 16px; font-weight: 600;">
                                            {{.T.VerifyButton}}
                                        </a>
                                    </td>
                                </tr>
                            </table>
                        </td>
                    </tr>
                </table>
            </td>
        </tr>
    </table>
</body>
</html>
    `

// SendVerificationEmail envoie l'email de vérification, dans la langue du compte.
func SendVerificationEmail(toEmail, userName, token, locale string) error {
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:3000"
	}

	t := accountEmailIn(locale)
	htmlBody, err := renderEmail("verification", verificationEmailTemplate, map[string]interface{}{
		"Lang":    PickLocale(locale),
		"T":       t,
		"Heading": fmt.Sprintf(t.VerifyHeading, userName),
		"Link":    fmt.Sprintf("%s/verify-email?token=%s", frontendURL, token),
	})
	if err != nil {
		log.Printf("❌ Error rendering verification email: %v", err)
		return err
	}

	return sendEmail(toEmail, t.VerifySubject, htmlBody)
}

// ============================================================================
// PASSWORD RESET (Resend API)
// ============================================================================

const passwordResetEmailTemplate = `
<!DOCTYPE html>
<html lang="{{.Lang}}">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.T.ResetTitle}}</title>
</head>
<body style="margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif; background-color: #f3f4f6;">
    <table role="presentation" style="width: 100%; border-collapse: collapse;">
        <tr>
            <td style="padding: 40px 0; text-align: center; background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);">
                <h1 style="margin: 0; color: #ffffff; font-size: 28px; font-weight: bold;">
                    💰 Budget Famille
                </h1>
            </td>
        </tr>
        <tr>
            <td style="padding: 40px 20px;">
                <table role="presentation" style="max-width: 600px; margin: 0 auto; background-color: #ffffff; border-radius: 12px; box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);">
                    <tr>
                        <td style="padding: 40px;">
                            <h2 style="margin: 0 0 20px 0; color: #1f2937; font-size: 24px; font-weight: bold;">
                                {{.T.ResetHello}} {{.Name}} 👋
                            </h2>
                            <p style="margin: 0 0 20px 0; color: #4b5563; font-size: 16px; line-height: 1.6;">
                                {{.T.ResetIntro}}
                            </p>
                            <p style="margin: 0 0 30px 0; color: #4b5563; font-size: 16px; line-height: 1.6;">
                                {{.T.ResetAsk}}
                            </p>
                            <table role="presentation" style="margin: 0 0 30px 0;">
                                <tr>
                                    <td style="border-radius: 8px; background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);">
                                        <a href="{{.ResetLink}}" style="display: inline-block; padding: 16px 32px; color: #ffffff; text-decoration: none; font-size: 16px; font-weight: 600;">
                                            {{.T.ResetButton}}
                                        </a>
                                    </td>
                                </tr>
                            </table>
                            <p style="margin: 0 0 20px 0; color: #6b7280; font-size: 14px; line-height: 1.6;">
                                {{.T.ResetValidity}} <strong>{{.T.ResetValidityTime}}</strong>.
                            </p>
                            <p style="margin: 0 0 20px 0; color: #6b7280; font-size: 14px; line-height: 1.6;">
                                {{.T.ResetCopy}}
                            </p>
                            <p style="margin: 0 0 30px 0; padding: 12px; background-color: #f3f4f6; border-radius: 6px; word-break: break-all; font-size: 13px; color: #4b5563;">
                                {{.ResetLink}}
                            </p>
                            <div style="border-top: 2px solid #e5e7eb; padding-top: 20px; margin-top: 30px;">
                                <p style="margin: 0 0 10px 0; color: #ef4444; font-size: 14px; font-weight: 600;">
                                    {{.T.ResetNotYou}}
                                </p>
                                <p style="margin: 0; color: #6b7280; font-size: 14px; line-height: 1.6;">
                                    {{.T.ResetIgnore}}
                                </p>
                            </div>
                        </td>
                    </tr>
                </table>
            </td>
        </tr>
        <tr>
            <td style="padding: 20px; text-align: center;">
                <p style="margin: 0 0 10px 0; color: #6b7280; font-size: 14px;">
                    {{.T.ResetFooter}}
                </p>
                <p style="margin: 0; color: #9ca3af; font-size: 12px;">
                    {{.T.ResetAuto}}
                </p>
            </td>
        </tr>
    </table>
</body>
</html>
`

// SendPasswordResetEmail envoie un email de réinitialisation via Resend API,
// dans la langue du compte.
func SendPasswordResetEmail(toEmail, userName, resetToken, locale string) error {
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:3000"
	}

	t := accountEmailIn(locale)
	body, err := renderEmail("passwordReset", passwordResetEmailTemplate, map[string]interface{}{
		"Lang":      PickLocale(locale),
		"T":         t,
		"Name":      userName,
		"ResetLink": fmt.Sprintf("%s/reset-password?token=%s", frontendURL, resetToken),
	})
	if err != nil {
		log.Printf("❌ Error rendering password reset email: %v", err)
		return err
	}

	// REUSE the existing sendEmail function (Resend API)
	return sendEmail(toEmail, t.ResetSubject, body)
}

// ============================================================================
// SHARED PRIVATE HELPER (Resend API)
// ============================================================================

func sendEmail(to, subject, htmlBody string) error {
	apiKey := os.Getenv("RESEND_API_KEY")
	if apiKey == "" {
		log.Println("⚠️ RESEND_API_KEY not set, email not sent")
		return fmt.Errorf("RESEND_API_KEY not set")
	}

	fromEmail := os.Getenv("FROM_EMAIL")
	if fromEmail == "" {
		fromEmail = "Budget Famille <noreply@budgetfamille.com>"
	}

	emailReq := EmailRequest{
		From:    fromEmail,
		To:      []string{to},
		Subject: subject,
		HTML:    htmlBody,
	}

	jsonData, err := json.Marshal(emailReq)
	if err != nil {
		log.Printf("❌ Error marshaling email request: %v", err)
		return err
	}

	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("❌ Error creating HTTP request: %v", err)
		return err
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("❌ Error sending email via Resend: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		log.Printf("❌ Resend API error: status %d", resp.StatusCode)
		return fmt.Errorf("email API returned status: %d", resp.StatusCode)
	}

	log.Printf("✅ Email sent successfully to %s", to)
	return nil
}
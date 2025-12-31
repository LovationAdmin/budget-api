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

// SendInvitationEmail envoie l'email d'invitation
func SendInvitationEmail(toEmail, inviterName, budgetName, invitationToken string) error {
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:3000"
	}

	invitationLink := fmt.Sprintf("%s/invitation/accept?token=%s", frontendURL, invitationToken)

	htmlBody := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Invitation Budget</title>
</head>
<body style="margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background-color: #f3f4f6;">
    <table role="presentation" style="width: 100%%; border-collapse: collapse;">
        <tr>
            <td style="padding: 40px 0; text-align: center; background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%);">
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
                            <h2 style="margin: 0 0 20px 0; color: #1f2937; font-size: 24px;">Invitation à collaborer</h2>
                            <p style="margin: 0 0 20px 0; color: #4b5563; font-size: 16px; line-height: 1.6;">
                                <strong>%s</strong> vous invite à rejoindre le budget <strong>"%s"</strong>.
                            </p>
                            <table role="presentation" style="margin: 20px 0;">
                                <tr>
                                    <td style="border-radius: 8px; background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%);">
                                        <a href="%s" style="display: inline-block; padding: 16px 32px; color: #ffffff; text-decoration: none; font-size: 16px; font-weight: 600;">
                                            Accepter l'invitation
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
    `, inviterName, budgetName, invitationLink)

	return sendEmail(toEmail, fmt.Sprintf("%s vous invite à collaborer", inviterName), htmlBody)
}

// SendVerificationEmail envoie l'email de vérification
func SendVerificationEmail(toEmail, userName, token string) error {
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:3000"
	}

	verifyLink := fmt.Sprintf("%s/verify-email?token=%s", frontendURL, token)

	htmlBody := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Vérification Email</title>
</head>
<body style="margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background-color: #f3f4f6;">
    <table role="presentation" style="width: 100%%; border-collapse: collapse;">
        <tr>
            <td style="padding: 40px 0; text-align: center; background: linear-gradient(135deg, #10b981 0%%, #059669 100%%);">
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
                            <h2 style="margin: 0 0 20px 0; color: #1f2937; font-size: 24px;">Bienvenue %s ! 👋</h2>
                            <p style="margin: 0 0 20px 0; color: #4b5563; font-size: 16px; line-height: 1.6;">
                                Veuillez vérifier votre email pour activer votre compte Budget Famille.
                            </p>
                            <table role="presentation" style="margin: 20px 0;">
                                <tr>
                                    <td style="border-radius: 8px; background: linear-gradient(135deg, #10b981 0%%, #059669 100%%);">
                                        <a href="%s" style="display: inline-block; padding: 16px 32px; color: #ffffff; text-decoration: none; font-size: 16px; font-weight: 600;">
                                            Vérifier mon email
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
    `, userName, verifyLink)

	return sendEmail(toEmail, "Vérifiez votre compte Budget Famille", htmlBody)
}

// ============================================================================
// NEW FEATURE: PASSWORD RESET (Using Resend API - No Regression)
// ============================================================================

const passwordResetEmailTemplate = `
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Réinitialisation de mot de passe</title>
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
                                Bonjour {{.Name}} 👋
                            </h2>
                            <p style="margin: 0 0 20px 0; color: #4b5563; font-size: 16px; line-height: 1.6;">
                                Nous avons reçu une demande de réinitialisation de mot de passe pour votre compte Budget Famille.
                            </p>
                            <p style="margin: 0 0 30px 0; color: #4b5563; font-size: 16px; line-height: 1.6;">
                                Si vous êtes à l'origine de cette demande, cliquez sur le bouton ci-dessous pour définir un nouveau mot de passe :
                            </p>
                            <table role="presentation" style="margin: 0 0 30px 0;">
                                <tr>
                                    <td style="border-radius: 8px; background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);">
                                        <a href="{{.ResetLink}}" style="display: inline-block; padding: 16px 32px; color: #ffffff; text-decoration: none; font-size: 16px; font-weight: 600;">
                                            Réinitialiser mon mot de passe
                                        </a>
                                    </td>
                                </tr>
                            </table>
                            <p style="margin: 0 0 20px 0; color: #6b7280; font-size: 14px; line-height: 1.6;">
                                Ce lien est valide pendant <strong>1 heure</strong>.
                            </p>
                            <p style="margin: 0 0 20px 0; color: #6b7280; font-size: 14px; line-height: 1.6;">
                                Si le bouton ne fonctionne pas, copiez et collez ce lien dans votre navigateur :
                            </p>
                            <p style="margin: 0 0 30px 0; padding: 12px; background-color: #f3f4f6; border-radius: 6px; word-break: break-all; font-size: 13px; color: #4b5563;">
                                {{.ResetLink}}
                            </p>
                            <div style="border-top: 2px solid #e5e7eb; padding-top: 20px; margin-top: 30px;">
                                <p style="margin: 0 0 10px 0; color: #ef4444; font-size: 14px; font-weight: 600;">
                                    ⚠️ Vous n'avez pas demandé cette réinitialisation ?
                                </p>
                                <p style="margin: 0; color: #6b7280; font-size: 14px; line-height: 1.6;">
                                    Ignorez simplement cet email. Votre mot de passe actuel reste inchangé.
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
                    Budget Famille - Gestion budgétaire familiale sécurisée
                </p>
                <p style="margin: 0; color: #9ca3af; font-size: 12px;">
                    Cet email a été envoyé automatiquement, merci de ne pas y répondre.
                </p>
            </td>
        </tr>
    </table>
</body>
</html>
`

// SendPasswordResetEmail envoie un email de réinitialisation via Resend API
func SendPasswordResetEmail(toEmail, userName, resetToken string) error {
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:3000"
	}

	resetLink := fmt.Sprintf("%s/reset-password?token=%s", frontendURL, resetToken)

	data := struct {
		Name      string
		ResetLink string
	}{
		Name:      userName,
		ResetLink: resetLink,
	}

	// Use template engine to generate the HTML body
	tmpl, err := template.New("passwordReset").Parse(passwordResetEmailTemplate)
	if err != nil {
		log.Printf("❌ Error parsing password reset template: %v", err)
		return err
	}

	var body bytes.Buffer
	if err := tmpl.Execute(&body, data); err != nil {
		log.Printf("❌ Error executing password reset template: %v", err)
		return err
	}

	// REUSE the existing sendEmail function (Resend API)
	return sendEmail(toEmail, "Réinitialisation de votre mot de passe Budget Famille", body.String())
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
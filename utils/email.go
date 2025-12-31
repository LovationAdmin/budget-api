package utils

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"net/smtp"
	"os"
)

// ============================================================================
// EMAIL TEMPLATES
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
                                    Ignorez simplement cet email. Votre mot de passe actuel reste inchangé. Si vous recevez plusieurs emails de ce type, contactez-nous immédiatement.
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

// ============================================================================
// PASSWORD RESET EMAIL FUNCTION
// ============================================================================

// SendPasswordResetEmail envoie un email de réinitialisation de mot de passe
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

	// Configuration SMTP
	smtpHost := os.Getenv("SMTP_HOST")
	smtpPort := os.Getenv("SMTP_PORT")
	smtpUser := os.Getenv("SMTP_USER")
	smtpPass := os.Getenv("SMTP_PASS")
	fromEmail := os.Getenv("FROM_EMAIL")

	if smtpHost == "" || smtpPort == "" || smtpUser == "" || smtpPass == "" {
		log.Println("⚠️ SMTP configuration incomplete, email not sent")
		return fmt.Errorf("SMTP configuration incomplete")
	}

	if fromEmail == "" {
		fromEmail = "noreply@budgetfamille.com"
	}

	// Construction du message
	subject := "Réinitialisation de votre mot de passe Budget Famille"
	msg := []byte(fmt.Sprintf(
		"From: Budget Famille <%s>\r\n"+
			"To: %s\r\n"+
			"Subject: %s\r\n"+
			"MIME-Version: 1.0\r\n"+
			"Content-Type: text/html; charset=UTF-8\r\n"+
			"\r\n"+
			"%s",
		fromEmail, toEmail, subject, body.String(),
	))

	// Authentification et envoi
	auth := smtp.PlainAuth("", smtpUser, smtpPass, smtpHost)
	addr := fmt.Sprintf("%s:%s", smtpHost, smtpPort)

	err = smtp.SendMail(addr, auth, fromEmail, []string{toEmail}, msg)
	if err != nil {
		log.Printf("❌ Error sending password reset email to %s: %v", toEmail, err)
		return err
	}

	log.Printf("✅ Password reset email sent to %s", toEmail)
	return nil
}
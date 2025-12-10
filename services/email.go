package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

type EmailRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
}

// SendInvitationEmail envoie l'email d'invitation à un budget
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
    <style>
        body { font-family: sans-serif; }
        .container { max-width: 600px; margin: 0 auto; padding: 20px; }
        .header { background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%); color: white; padding: 30px; border-radius: 10px 10px 0 0; }
        .content { background: #f8f9fa; padding: 30px; }
        .button { display: inline-block; background: #667eea; color: white; padding: 15px 30px; text-decoration: none; border-radius: 8px; margin: 20px 0; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>💰 Invitation à un Budget</h1>
        </div>
        <div class="content">
            <p>Bonjour,</p>
            <p><strong>%s</strong> vous invite à collaborer sur le budget <strong>"%s"</strong>.</p>
            <a href="%s" class="button">Accepter l'invitation</a>
            <p style="color: #e74c3c; margin-top: 30px;">⚠️ Ce lien expire dans 7 jours.</p>
        </div>
    </div>
</body>
</html>
	`, inviterName, budgetName, invitationLink)

	return sendEmail(toEmail, fmt.Sprintf("%s vous invite à collaborer", inviterName), htmlBody)
}

// SendVerificationEmail envoie l'email de confirmation de création de compte
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
    <style>
        body { font-family: sans-serif; color: #333; }
        .container { max-width: 600px; margin: 0 auto; padding: 20px; border: 1px solid #eee; border-radius: 10px; }
        .button { display: inline-block; background: #10b981; color: white; padding: 12px 24px; text-decoration: none; border-radius: 6px; margin: 20px 0; font-weight: bold; }
    </style>
</head>
<body>
    <div class="container">
        <h2>Bienvenue sur Budget Famille ! 👋</h2>
        <p>Bonjour %s,</p>
        <p>Merci de vérifier votre adresse email pour activer votre compte.</p>
        <a href="%s" class="button">Vérifier mon email</a>
        <p>Si vous n'avez pas créé de compte, ignorez cet email.</p>
    </div>
</body>
</html>
	`, userName, verifyLink)

	return sendEmail(toEmail, "Vérifiez votre compte Budget Famille", htmlBody)
}

// sendEmail est la fonction privée interne qui appelle l'API Resend
func sendEmail(to, subject, htmlBody string) error {
	apiKey := os.Getenv("RESEND_API_KEY")
	if apiKey == "" {
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
		return fmt.Errorf("failed to marshal email request: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("email API returned status %d", resp.StatusCode)
	}

	return nil
}
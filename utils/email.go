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
    <style>
        body { font-family: sans-serif; }
        .container { max-width: 600px; margin: 0 auto; padding: 20px; }
        .button { display: inline-block; background: #667eea; color: white; padding: 15px 30px; text-decoration: none; border-radius: 8px; margin: 20px 0; }
    </style>
</head>
<body>
    <div class="container">
        <h1>Invitation Budget</h1>
        <p><strong>%s</strong> vous invite sur <strong>"%s"</strong>.</p>
        <a href="%s" class="button">Accepter l'invitation</a>
    </div>
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
    <style>
        body { font-family: sans-serif; }
        .container { max-width: 600px; margin: 0 auto; padding: 20px; }
        .button { display: inline-block; background: #10b981; color: white; padding: 12px 24px; text-decoration: none; border-radius: 6px; margin: 20px 0; }
    </style>
</head>
<body>
    <div class="container">
        <h2>Bienvenue %s !</h2>
        <p>Veuillez vérifier votre email pour activer votre compte.</p>
        <a href="%s" class="button">Vérifier mon email</a>
    </div>
</body>
</html>
	`, userName, verifyLink)

	return sendEmail(toEmail, "Vérifiez votre compte", htmlBody)
}

// sendEmail (fonction privée)
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
		return err
	}

	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("email API status: %d", resp.StatusCode)
	}

	return nil
}
package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

type EmailService struct {
	apiKey      string
	fromEmail   string
	frontendURL string
}

func NewEmailService(apiKey, fromEmail, frontendURL string) *EmailService {
	return &EmailService{
		apiKey:      apiKey,
		fromEmail:   fromEmail,
		frontendURL: frontendURL,
	}
}

func (s *EmailService) SendInvitation(to, inviterName, budgetName, token string) error {
	if s.apiKey == "" {
		return fmt.Errorf("RESEND_API_KEY not configured")
	}

	invitationURL := fmt.Sprintf("%s/invitation/accept?token=%s", s.frontendURL, token)

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
	`, inviterName, budgetName, invitationURL)

	payload := map[string]interface{}{
		"from":    fmt.Sprintf("Budget Famille <%s>", s.fromEmail),
		"to":      []string{to},
		"subject": fmt.Sprintf("%s vous invite à collaborer", inviterName),
		"html":    htmlBody,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.apiKey))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to send email: status %d", resp.StatusCode)
	}

	return nil
}
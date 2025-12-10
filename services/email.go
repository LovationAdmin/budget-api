package services

import (
	"budget-api/utils" // Import your utility package
)

// EmailService will hold any dependencies needed for email operations.
// It currently doesn't need any, but it's good practice to define it.
type EmailService struct {
	// No fields needed for now
}

// NewEmailService creates a new instance of EmailService.
func NewEmailService() *EmailService {
	return &EmailService{}
}

// SendInvitation is the method that the handler calls.
// It acts as a wrapper for the actual utility function in utils/email.go.
func (s *EmailService) SendInvitation(toEmail, inviterName, budgetName, invitationToken string) error {
	// Call the utility function that contains the actual logic
	return utils.SendInvitationEmail(toEmail, inviterName, budgetName, invitationToken)
}

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


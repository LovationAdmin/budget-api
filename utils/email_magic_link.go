// utils/email_magic_link.go
// ============================================================================
// MAGIC LINK EMAIL
// ============================================================================
// Sends the passwordless sign-in email via Resend (same backend as the
// other transactional emails). Uses the package-private sendEmail helper
// already defined in utils/email.go.
// ============================================================================

package utils

import "fmt"

// SendMagicLinkEmail emails a sign-in link to the user.
// `link` is the full URL: https://budgetfamille.com/m/magic-link?token=...
// On Android, the intent filter (configured in budget-mobile/app.json)
// opens the BudgetFamille app directly when the user taps the link.
func SendMagicLinkEmail(toEmail, userName, link string) error {
	subject := "Votre lien de connexion BudgetFamille"
	body := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Connexion BudgetFamille</title>
</head>
<body style="margin:0; padding:0; font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif; background-color:#FFEDD5;">
  <table role="presentation" style="width:100%%; border-collapse:collapse;">
    <tr>
      <td style="padding:40px 0; text-align:center; background:linear-gradient(135deg,#FB923C 0%%,#F97316 100%%);">
        <h1 style="margin:0; color:#FFFFFF; font-size:28px; font-weight:bold;">
          BudgetFamille
        </h1>
      </td>
    </tr>
    <tr>
      <td style="padding:40px 20px;">
        <table role="presentation" style="max-width:600px; margin:0 auto; background-color:#FFFFFF; border-radius:16px; box-shadow:0 4px 12px rgba(0,0,0,0.08);">
          <tr>
            <td style="padding:40px;">
              <h2 style="margin:0 0 16px 0; color:#0F172A; font-size:22px;">Bonjour %s,</h2>
              <p style="margin:0 0 20px 0; color:#475569; font-size:16px; line-height:1.6;">
                Voici votre lien de connexion. Il est valable 15 minutes et ne peut être utilisé qu'une seule fois.
              </p>
              <table role="presentation" style="margin:24px 0;">
                <tr>
                  <td style="border-radius:12px; background:linear-gradient(135deg,#FB923C 0%%,#F97316 100%%);">
                    <a href="%s" style="display:inline-block; padding:16px 32px; color:#FFFFFF; text-decoration:none; font-size:16px; font-weight:600;">
                      Me connecter
                    </a>
                  </td>
                </tr>
              </table>
              <p style="margin:24px 0 0 0; color:#94A3B8; font-size:13px; line-height:1.5;">
                Si vous n'avez pas demandé ce lien, vous pouvez ignorer cet email — votre compte reste sécurisé.
              </p>
            </td>
          </tr>
        </table>
      </td>
    </tr>
  </table>
</body>
</html>`, userName, link)

	return sendEmail(toEmail, subject, body)
}

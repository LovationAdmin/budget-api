// utils/email_campaign.go
// ============================================================================
// CAMPAIGN EMAIL HELPER — re-engagement campaigns via Resend with Reply-To
// ============================================================================
// This file is INDEPENDENT from the existing sendEmail() helper in email.go,
// to avoid regression on the verification / password-reset / invitation flows.
// It uses its own JSON request struct to support the Reply-To header that
// the existing EmailRequest struct does not expose.
// ============================================================================

package utils

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strings"
	"time"
)

// ============================================================================
// EMBEDDED TEMPLATES
// ============================================================================

//go:embed campaign_templates/*.html
var campaignTemplates embed.FS

// CampaignVariant identifies which template + subject line to use.
type CampaignVariant string

const (
	CampaignReengagementVerified   CampaignVariant = "reengagement_verified"
	CampaignReengagementUnverified CampaignVariant = "reengagement_unverified"
)

var campaignTemplateFiles = map[CampaignVariant]string{
	CampaignReengagementVerified:   "campaign_templates/reengagement_founder.html",
	CampaignReengagementUnverified: "campaign_templates/reengagement_unverified.html",
}

var campaignSubjects = map[CampaignVariant]string{
	CampaignReengagementVerified:   "Budget Famille : navigation repensée 👋",
	CampaignReengagementUnverified: "Votre compte Budget Famille est encore en attente",
}

// ============================================================================
// RENDERING
// ============================================================================

type campaignTemplateData struct {
	Name       string
	AppURL     string
	LoginURL   string
	CampaignID string
}

// RenderCampaignEmail renders the HTML body + subject line for the variant.
func RenderCampaignEmail(variant CampaignVariant, userName, campaignID string) (subject string, html string, err error) {
	file, ok := campaignTemplateFiles[variant]
	if !ok {
		return "", "", fmt.Errorf("unknown campaign variant: %q", variant)
	}

	raw, err := campaignTemplates.ReadFile(file)
	if err != nil {
		return "", "", fmt.Errorf("read template %s: %w", file, err)
	}

	tmpl, err := template.New(string(variant)).Parse(string(raw))
	if err != nil {
		return "", "", fmt.Errorf("parse template %s: %w", file, err)
	}

	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:3000"
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, campaignTemplateData{
		Name:       userName,
		AppURL:     frontendURL,
		LoginURL:   frontendURL + "/login",
		CampaignID: campaignID,
	}); err != nil {
		return "", "", fmt.Errorf("execute template %s: %w", file, err)
	}

	return campaignSubjects[variant], buf.String(), nil
}

// ============================================================================
// SENDING (independent from existing sendEmail to avoid regression)
// ============================================================================

// campaignEmailRequest mirrors EmailRequest but adds reply_to + text fallback.
type campaignEmailRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
	Text    string   `json:"text,omitempty"`
	ReplyTo string   `json:"reply_to,omitempty"`
}

type campaignEmailResponse struct {
	ID string `json:"id"`
}

// SendCampaignEmail dispatches a single campaign email via Resend.
//
// FROM and Reply-To are read from env vars dedicated to campaigns so they
// don't collide with the existing FROM_EMAIL used for transactional flows:
//
//	CAMPAIGN_FROM_EMAIL  default: "Libasse — Budget Famille <libasse@budgetfamille.com>"
//	CAMPAIGN_REPLY_TO    default: "lovation.pro@gmail.com"
func SendCampaignEmail(toEmail, subject, htmlBody string) (msgID string, err error) {
	apiKey := os.Getenv("RESEND_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("RESEND_API_KEY not set")
	}

	from := os.Getenv("CAMPAIGN_FROM_EMAIL")
	if from == "" {
		from = "Libasse — Budget Famille <libasse@budgetfamille.com>"
	}
	replyTo := os.Getenv("CAMPAIGN_REPLY_TO")
	if replyTo == "" {
		replyTo = "lovation.pro@gmail.com"
	}

	body, err := json.Marshal(campaignEmailRequest{
		From:    from,
		To:      []string{toEmail},
		Subject: subject,
		HTML:    htmlBody,
		Text:    htmlToPlainText(htmlBody),
		ReplyTo: replyTo,
	})
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost,
		"https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("resend status %d", resp.StatusCode)
	}

	var parsed campaignEmailResponse
	_ = json.NewDecoder(resp.Body).Decode(&parsed)
	return parsed.ID, nil
}

// ============================================================================
// PLAINTEXT FALLBACK
// ============================================================================

// htmlToPlainText produces a basic text/plain alternative from the HTML body.
// Helps spam scores on emails sent without an explicit text part.
func htmlToPlainText(html string) string {
	repl := strings.NewReplacer(
		"<br>", "\n", "<br/>", "\n", "<br />", "\n",
		"</p>", "\n\n", "</div>", "\n", "</tr>", "\n",
		"</li>", "\n", "</h1>", "\n\n", "</h2>", "\n\n", "</h3>", "\n\n",
	)
	s := repl.Replace(html)

	// Strip remaining tags.
	var out strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
		case !in:
			out.WriteRune(r)
		}
	}

	decoded := strings.NewReplacer(
		"&nbsp;", " ",
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&#39;", "'",
		"&hellip;", "…",
	).Replace(out.String())

	// Collapse multi-blank lines.
	lines := strings.Split(decoded, "\n")
	cleaned := make([]string, 0, len(lines))
	blanks := 0
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			blanks++
			if blanks <= 1 {
				cleaned = append(cleaned, "")
			}
			continue
		}
		blanks = 0
		cleaned = append(cleaned, l)
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

// ============================================================================
// SAFE LOGGING HELPER
// ============================================================================

// MaskEmailForLog returns a redacted form of an email for safe logging.
//
//	alice@gmail.com  -> al***@gmail.com
//	bo@gmail.com     -> ***@gmail.com
//
// Named distinctly to avoid collision with existing utils helpers.
func MaskEmailForLog(e string) string {
	at := strings.IndexByte(e, '@')
	if at < 0 {
		return "***"
	}
	if at <= 2 {
		return "***" + e[at:]
	}
	return e[:2] + strings.Repeat("*", at-2) + e[at:]
}

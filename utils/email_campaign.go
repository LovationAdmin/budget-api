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
	"html"
	"html/template"
	"math"
	"net/http"
	"os"
	"regexp"
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
	// What's new + apology after the 2026 outage (one-off announcement).
	CampaignWhatsNew202610 CampaignVariant = "whatsnew_2026_10"
)

var campaignTemplateFiles = map[CampaignVariant]string{
	CampaignReengagementVerified:   "campaign_templates/reengagement_founder.html",
	CampaignReengagementUnverified: "campaign_templates/reengagement_unverified.html",
	CampaignWhatsNew202610:         "campaign_templates/whatsnew_2026_10.html",
}

var campaignSubjects = map[CampaignVariant]string{
	CampaignReengagementVerified:   "Budget Famille : navigation repensée 👋",
	CampaignReengagementUnverified: "Votre compte Budget Famille est encore en attente",
	CampaignWhatsNew202610:         "On vous doit des excuses (et quelques belles surprises)",
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

// formatMoney renders an amount with the given currency symbol. We display
// the integer part with a thin no-break-space group separator (1 234) which
// reads well in both French and English HTML rendering.
func formatMoney(amount float64, symbol string) string {
	negative := amount < 0
	if negative {
		amount = -amount
	}
	integer := int64(amount + 0.5)
	rendered := groupedThousands(integer)
	if negative && integer > 0 {
		return "−" + rendered + " " + symbol
	}
	return rendered + " " + symbol
}

func groupedThousands(n int64) string {
	if n == 0 {
		return "0"
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	// Insert NBSP every 3 digits from the right.
	first := len(s) % 3
	var parts []string
	if first > 0 {
		parts = append(parts, s[:first])
	}
	for i := first; i < len(s); i += 3 {
		parts = append(parts, s[i:i+3])
	}
	return strings.Join(parts, " ")
}

// monthlyRecapFuncs are the template helpers exposed inside the recap
// templates. Keep this list small — every entry expands the surface area we
// have to keep in sync between the two locales.
func monthlyRecapFuncs() template.FuncMap {
	return template.FuncMap{
		"formatMoney": formatMoney,
		"lower":       strings.ToLower,
		"deMonth":     deMonth,
		"abs":         math.Abs,
		"add":         func(a, b float64) float64 { return a + b },
		"pctOf":       pctOf,
	}
}

// deMonth returns "de septembre 2026" / "d’avril 2026" (French elision
// before a vowel), lower-cased as French month names are.
func deMonth(label string) string {
	l := strings.ToLower(strings.TrimSpace(label))
	if l == "" {
		return l
	}
	switch []rune(l)[0] {
	case 'a', 'e', 'i', 'o', 'u', 'é', 'è', 'h':
		return "d’" + l
	}
	return "de " + l
}

// pctOf returns part as a whole percentage (0..100) of the larger of the two
// totals — used to draw the month bar (charges, savings, rest) in the recap.
func pctOf(part, totalA, totalB float64) int {
	total := totalA
	if totalB > total {
		total = totalB
	}
	if total <= 0 || part <= 0 {
		return 0
	}
	v := int(part/total*100 + 0.5)
	if v > 100 {
		return 100
	}
	return v
}

// monthlyRecapTemplate is the recap of every language (words: recap_text.go).
const monthlyRecapTemplate = "campaign_templates/monthly_recap.html"

// RenderMonthlyRecapEmail renders the monthly recap in `locale` ("fr", or
// English for anything else). `data` should be a *services.RecapData (or any
// struct exposing the same exported fields the template references). We
// accept `interface{}` to avoid importing the services package from utils.
// `monthLabel` is the month the recap prepares ("Novembre"), for the subject.
func RenderMonthlyRecapEmail(locale, monthLabel, budgetName string, data interface{}) (subject string, html string, err error) {
	if locale != LocaleFR {
		locale = LocaleEN
	}
	raw, err := campaignTemplates.ReadFile(monthlyRecapTemplate)
	if err != nil {
		return "", "", fmt.Errorf("read template %s: %w", monthlyRecapTemplate, err)
	}
	funcs := recapFuncs(locale)
	tmpl, err := template.New("monthly_recap").Funcs(funcs).Parse(string(raw))
	if err != nil {
		return "", "", fmt.Errorf("parse template %s: %w", monthlyRecapTemplate, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", "", fmt.Errorf("execute template %s: %w", monthlyRecapTemplate, err)
	}
	subject, err = recapT(locale, "subject", funcs["deM"].(func(string) string)(monthLabel), budgetName)
	return subject, buf.String(), err
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
	// List-Unsubscribe lets mail clients show a one-tap unsubscribe and
	// improves deliverability of campaign emails.
	Headers map[string]string `json:"headers,omitempty"`
}

type campaignEmailResponse struct {
	ID string `json:"id"`
}

// CampaignMessage is one campaign email.
type CampaignMessage struct {
	To, Subject, HTML string
	// Unsubscribe, when set, gives mail clients the one-click unsubscribe
	// Gmail and Yahoo expect from bulk mail (RFC 8058); without it, replying
	// "STOP" is the only way out.
	Unsubscribe *UnsubscribeLinks
	// Ref is unique per email: Gmail then never folds monthly emails with the
	// same subject into one conversation (X-Entity-Ref-ID).
	Ref string
}

// SendCampaignEmail dispatches a single campaign email via Resend.
func SendCampaignEmail(toEmail, subject, htmlBody string) (msgID string, err error) {
	return SendCampaign(CampaignMessage{To: toEmail, Subject: subject, HTML: htmlBody})
}

// SendCampaign dispatches a campaign email via Resend, with its text part and
// list headers.
//
// FROM and Reply-To are read from env vars dedicated to campaigns so they
// don't collide with the existing FROM_EMAIL used for transactional flows:
//
//	CAMPAIGN_FROM_EMAIL  default: "Libasse — Budget Famille <libasse@budgetfamille.com>"
//	CAMPAIGN_REPLY_TO    default: "lovation.pro@gmail.com"
func SendCampaign(msg CampaignMessage) (msgID string, err error) {
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
		To:      []string{msg.To},
		Subject: msg.Subject,
		HTML:    msg.HTML,
		Text:    htmlToPlainText(msg.HTML),
		ReplyTo: replyTo,
		Headers: campaignHeaders(msg, mailboxAddress(replyTo)),
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

// campaignHeaders: the unsubscribe headers (one-click https first, the reply
// mailbox as a fallback) and the message reference.
func campaignHeaders(msg CampaignMessage, mailbox string) map[string]string {
	mailto := "<mailto:" + mailbox + "?subject=STOP>"
	h := map[string]string{"List-Unsubscribe": mailto}
	if msg.Unsubscribe != nil && msg.Unsubscribe.OneClick != "" {
		h["List-Unsubscribe"] = "<" + msg.Unsubscribe.OneClick + ">, " + mailto
		h["List-Unsubscribe-Post"] = "List-Unsubscribe=One-Click"
	}
	if msg.Ref != "" {
		h["X-Entity-Ref-ID"] = msg.Ref
	}
	return h
}

// mailboxAddress extracts "a@b.c" from "Name <a@b.c>" (or returns it as is).
func mailboxAddress(s string) string {
	if i := strings.LastIndex(s, "<"); i >= 0 {
		if j := strings.LastIndex(s, ">"); j > i {
			return strings.TrimSpace(s[i+1 : j])
		}
	}
	return strings.TrimSpace(s)
}

// ============================================================================
// PLAINTEXT FALLBACK
// ============================================================================

var (
	reDropBlocks = regexp.MustCompile(`(?is)<!--.*?-->|<head\b.*?</head>|<style\b.*?</style>|<script\b.*?</script>|<title\b.*?</title>`)
	// The inbox preview line: shown by the inbox, not part of the message.
	reHidden   = regexp.MustCompile(`(?is)<div[^>]*display:\s*none[^>]*>.*?</div>`)
	reLink     = regexp.MustCompile(`(?is)<a\b[^>]*\bhref="([^"]*)"[^>]*>(.*?)</a>`)
	reBreak    = regexp.MustCompile(`(?i)<br\s*/?>`)
	reBlockEnd = regexp.MustCompile(`(?i)</(p|div|tr|li|h[1-6]|table)>`)
	reCellEnd  = regexp.MustCompile(`(?i)</td>`)
	reTag      = regexp.MustCompile(`(?s)<[^>]*>`)
	reSpaces   = regexp.MustCompile(`[ \t\x{00a0}\x{202f}]+`)
	invisibles = strings.NewReplacer("\u034f", "", "\u200b", "", "\u200c", "", "\u200d", "", "\ufeff", "", "\u00ad", "")
)

// htmlToPlainText produces the text/plain alternative of an HTML email: the
// words in reading order, links spelled out, no styles nor hidden preview.
// A clean text part matching the HTML is part of what spam filters weigh.
func htmlToPlainText(htmlBody string) string {
	s := reDropBlocks.ReplaceAllString(htmlBody, "")
	s = reHidden.ReplaceAllString(s, "")
	s = reLink.ReplaceAllStringFunc(s, func(m string) string {
		parts := reLink.FindStringSubmatch(m)
		href := html.UnescapeString(parts[1])
		label := strings.TrimSpace(reSpaces.ReplaceAllString(html.UnescapeString(reTag.ReplaceAllString(parts[2], "")), " "))
		if !strings.HasPrefix(href, "http") || label == "" || label == href {
			return label + " " + href
		}
		return label + " : " + href
	})
	s = reBreak.ReplaceAllString(s, "\n")
	s = reBlockEnd.ReplaceAllString(s, "\n")
	s = reCellEnd.ReplaceAllString(s, " ")
	s = reTag.ReplaceAllString(s, "")
	s = invisibles.Replace(html.UnescapeString(s))

	// One space between words, at most one blank line between blocks.
	lines := strings.Split(s, "\n")
	cleaned := make([]string, 0, len(lines))
	blanks := 0
	for _, l := range lines {
		l = strings.TrimSpace(reSpaces.ReplaceAllString(l, " "))
		if l == "" {
			blanks++
			if blanks == 1 && len(cleaned) > 0 {
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

// PlainTextOf is the text/plain part sent with an HTML email.
func PlainTextOf(htmlBody string) string { return htmlToPlainText(htmlBody) }

package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatMoney(t *testing.T) {
	const nbsp = " "
	cases := []struct {
		in     float64
		symbol string
		want   string
	}{
		{0, "€", "0 €"},
		{12, "€", "12 €"},
		{1234, "€", "1" + nbsp + "234 €"},
		{1234567, "€", "1" + nbsp + "234" + nbsp + "567 €"},
		{-50, "€", "−50 €"},
		{-0.49, "€", "0 €"},
		{100, "$", "100 $"},
	}
	for _, c := range cases {
		got := formatMoney(c.in, c.symbol)
		if got != c.want {
			t.Errorf("formatMoney(%v, %s) = %q, want %q", c.in, c.symbol, got, c.want)
		}
	}
}

func TestRenderWhatsNew202610(t *testing.T) {
	t.Setenv("FRONTEND_URL", "https://budgetfamille.com")
	subject, html, err := RenderCampaignEmail(CampaignWhatsNew202610, "Alice", "whatsnew_2026_10")
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if subject == "" {
		t.Error("missing subject")
	}
	for _, w := range []string{
		"Bonjour Alice",
		"sincèrement désolé",
		"vos données sont intactes",
		"argent de poche",
		"Budget IA",
		"mode sombre",
		"https://www.budgetfamille.com/email/2026-10/month-light.jpg",
		"https://budgetfamille.com/dashboard?utm_source=email&amp;utm_medium=campaign&amp;utm_campaign=whatsnew_2026_10",
		"répondez «&nbsp;STOP&nbsp;»",
	} {
		if !strings.Contains(html, w) {
			t.Errorf("html missing %q", w)
		}
	}
	// No name: a friendly fallback instead of "Bonjour ,".
	_, html, _ = RenderCampaignEmail(CampaignWhatsNew202610, "", "whatsnew_2026_10")
	if !strings.Contains(html, "Bonjour à vous") {
		t.Error("empty name should fall back to « Bonjour à vous »")
	}
	if dir := os.Getenv("EMAIL_PREVIEW_DIR"); dir != "" {
		_, html, _ = RenderCampaignEmail(CampaignWhatsNew202610, "Camille", "whatsnew_2026_10")
		_ = os.WriteFile(filepath.Join(dir, "whatsnew.html"), []byte(html), 0o644)
	}
}

func TestMailboxAddress(t *testing.T) {
	cases := map[string]string{
		"lovation.pro@gmail.com":                    "lovation.pro@gmail.com",
		"Libasse — Budget Famille <libasse@bf.com>": "libasse@bf.com",
		" <a@b.c> ": "a@b.c",
	}
	for in, want := range cases {
		if got := mailboxAddress(in); got != want {
			t.Errorf("mailboxAddress(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeMonth(t *testing.T) {
	cases := map[string]string{
		"Septembre 2026": "de septembre 2026",
		"Avril 2026":     "d’avril 2026",
		"Août":           "d’août",
		"octobre":        "d’octobre",
		"Mai":            "de mai",
	}
	for in, want := range cases {
		if got := deMonth(in); got != want {
			t.Errorf("deMonth(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUnsubscribeToken(t *testing.T) {
	t.Setenv("EMAIL_UNSUBSCRIBE_SECRET", "s3cret")
	token := UnsubscribeToken("7b1c-uuid", EmailListMonthlyRecap)
	user, list, ok := ParseUnsubscribeToken(token)
	if !ok || user != "7b1c-uuid" || list != EmailListMonthlyRecap {
		t.Fatalf("round trip: %v %q %q", ok, user, list)
	}
	other := UnsubscribeToken("someone-else", EmailListMonthlyRecap)
	forged := strings.SplitN(other, ".", 2)[0] + "." + strings.SplitN(token, ".", 2)[1]
	for _, bad := range []string{"", "abc", token + "x", forged, UnsubscribeToken("u", "spam")} {
		if _, _, ok := ParseUnsubscribeToken(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
	t.Setenv("EMAIL_UNSUBSCRIBE_SECRET", "rotated")
	if _, _, ok := ParseUnsubscribeToken(token); ok {
		t.Error("a token signed with another secret is refused")
	}
}

func TestCampaignHeaders_OneClick(t *testing.T) {
	t.Setenv("API_PUBLIC_URL", "https://api.example.com")
	t.Setenv("FRONTEND_URL", "https://budgetfamille.com")
	links := UnsubscribeLinksFor("u1", EmailListMonthlyRecap)
	if !strings.HasPrefix(links.Page, "https://budgetfamille.com/emails/desabonnement?token=") ||
		!strings.HasPrefix(links.OneClick, "https://api.example.com/api/v1/email/unsubscribe?token=") {
		t.Errorf("links %+v", links)
	}
	h := campaignHeaders(CampaignMessage{Unsubscribe: &links, Ref: "monthly_recap_2026_11:u1"}, "hello@bf.com")
	if h["List-Unsubscribe"] != "<"+links.OneClick+">, <mailto:hello@bf.com?subject=STOP>" ||
		h["List-Unsubscribe-Post"] != "List-Unsubscribe=One-Click" || h["X-Entity-Ref-ID"] != "monthly_recap_2026_11:u1" {
		t.Errorf("headers %v", h)
	}
	if h := campaignHeaders(CampaignMessage{}, "hello@bf.com"); h["List-Unsubscribe-Post"] != "" || h["List-Unsubscribe"] != "<mailto:hello@bf.com?subject=STOP>" {
		t.Errorf("no links: mailto only, got %v", h)
	}
}

func TestHTMLToPlainText(t *testing.T) {
	html := `<!DOCTYPE html><html><head><title>T</title><style>.a{color:red}</style></head><body>
<!--[if mso]><p>outlook</p><![endif]-->
<div style="display:none; max-height:0;">Preview line &#847; &#847;</div>
<h1>Novembre est prêt.</h1><p>Reste&nbsp;prévu : <strong>450 €</strong></p>
<table><tr><td>Loyer</td><td>1 150 €</td></tr></table>
<a href="https://budgetfamille.com/x?a=1&amp;b=2">Ouvrir l’app →</a></body></html>`
	got := htmlToPlainText(html)
	want := "Novembre est prêt.\nReste prévu : 450 €\n\nLoyer 1 150 €\n\nOuvrir l’app → : https://budgetfamille.com/x?a=1&b=2"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestWithUTM(t *testing.T) {
	if got := withUTM("https://a.b/c?m=2026-11", "x", "cta"); got != "https://a.b/c?m=2026-11&utm_source=email&utm_medium=monthly_recap&utm_campaign=x&utm_content=cta" {
		t.Error(got)
	}
	if got := withUTM("https://a.b/c#split", "x", "s"); got != "https://a.b/c?utm_source=email&utm_medium=monthly_recap&utm_campaign=x&utm_content=s#split" {
		t.Error(got)
	}
}

// Every key of the recap exists in both languages.
func TestRecapTextBothLanguages(t *testing.T) {
	for key := range recapText[LocaleFR] {
		if _, ok := recapText[LocaleEN][key]; !ok {
			t.Errorf("%q missing in English", key)
		}
	}
	for key := range recapText[LocaleEN] {
		if _, ok := recapText[LocaleFR][key]; !ok {
			t.Errorf("%q missing in French", key)
		}
	}
}

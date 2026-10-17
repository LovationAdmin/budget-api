package utils

import (
	"strings"
	"testing"
	"time"
)

// The account emails render in both languages, French by default, and escape
// the names people type.
func TestAccountEmailsByLocale(t *testing.T) {
	fr := accountEmailIn("")
	en := accountEmailIn("en-GB")
	if fr.VerifySubject != "Vérifiez votre compte Budget Famille" || en.VerifySubject != "Confirm your Budget Famille account" {
		t.Fatalf("subjects: %q / %q", fr.VerifySubject, en.VerifySubject)
	}

	html, err := renderEmail("invitation", invitationEmailTemplate, map[string]interface{}{
		"Lang": "en", "T": en, "Inviter": "<b>Sam</b>", "Budget": "Home", "Link": "https://example.com/x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, `lang="en"`) || !strings.Contains(html, "invites you to join the budget") || !strings.Contains(html, "Accept the invitation") {
		t.Errorf("English invitation not rendered:\n%s", html)
	}
	if strings.Contains(html, "<b>Sam</b>") || !strings.Contains(html, "&lt;b&gt;Sam&lt;/b&gt;") {
		t.Errorf("inviter name not escaped")
	}

	html, err = renderEmail("verification", verificationEmailTemplate, map[string]interface{}{
		"Lang": "fr", "T": fr, "Heading": "Bienvenue Mariama ! 👋", "Link": "https://example.com/v",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Bienvenue Mariama ! 👋", "Veuillez vérifier votre email pour activer votre compte Budget Famille.", "Vérifier mon email", `href="https://example.com/v"`} {
		if !strings.Contains(html, want) {
			t.Errorf("French verification email lacks %q", want)
		}
	}

	html, err = renderEmail("passwordReset", passwordResetEmailTemplate, map[string]interface{}{
		"Lang": "en", "T": en, "Name": "Alex", "ResetLink": "https://example.com/r",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Hello Alex 👋", "Reset my password", "This link is valid for <strong>1 hour</strong>."} {
		if !strings.Contains(html, want) {
			t.Errorf("English reset email lacks %q", want)
		}
	}
}

// The warning before an unconfirmed account is deleted: its date and link, in
// the account's language.
func TestUnverifiedExpiryEmail(t *testing.T) {
	d := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	if got := FormatLongDate(d, "fr"); got != "24 octobre 2026" {
		t.Errorf("French date = %q", got)
	}
	if got := FormatLongDate(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), "en-GB"); got != "1 August 2026" {
		t.Errorf("English date = %q", got)
	}
	if got := accountEmailIn("fr").ExpireSubject("24 octobre 2026"); got != "Votre compte Budget Famille sera supprimé à partir du 24 octobre 2026" {
		t.Errorf("French subject = %q", got)
	}

	en := accountEmailIn("en")
	html, err := renderEmail("unverifiedExpiry", unverifiedExpiryEmailTemplate, map[string]interface{}{
		"Lang": "en", "T": en, "Heading": "Hello <i>Sam</i>,", "Date": "24 October 2026", "Link": "https://example.com/verify-email?token=t",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`lang="en"`, "will be deleted from <strong>24 October 2026</strong>.", "Confirm my email", `href="https://example.com/verify-email?token=t"`, "&lt;i&gt;Sam&lt;/i&gt;"} {
		if !strings.Contains(html, want) {
			t.Errorf("English expiry email lacks %q", want)
		}
	}
}

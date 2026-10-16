package utils

import (
	"strings"
	"testing"
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

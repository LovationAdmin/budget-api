package handlers

import (
	"errors"
	"strings"
	"testing"

	"github.com/LovationAdmin/budget-api/utils"
)

// Every password rule the server enforces must reach the user in French.
func TestPasswordErrorFR_CoversValidator(t *testing.T) {
	cases := map[string]string{
		"Court1!":                  "password_too_short",
		strings.Repeat("Aa1!", 20): "password_too_long",
		"toutenminuscules":         "password_too_simple",
		"Password123":              "password_common",
		"Xmariama.diallo2026!":     "password_contains_email",
		"Mariama2026!X":            "password_contains_name",
	}
	for pw, want := range cases {
		err := utils.ValidatePassword(pw, "mariama.diallo@mail.fr", "Mariama")
		if err == nil {
			t.Errorf("%q: expected a validation error", pw)
			continue
		}
		code, msg := passwordErrorFR(err)
		if code != want {
			t.Errorf("%q: code = %s (%v), want %s", pw, code, err, want)
		}
		if strings.Contains(msg, "password") || msg == "" {
			t.Errorf("%q: message not translated: %q", pw, msg)
		}
	}
}

func TestSignupBindErrorFR(t *testing.T) {
	code, _ := signupBindErrorFR(errors.New("Key: 'SignupRequest.Email' Error:Field validation for 'Email' failed on the 'email' tag"))
	if code != "email_invalid" {
		t.Errorf("email: %s", code)
	}
	code, _ = signupBindErrorFR(errors.New("Key: 'SignupRequest.Name' Error:Field validation for 'Name' failed on the 'min' tag"))
	if code != "name_invalid" {
		t.Errorf("name: %s", code)
	}
}

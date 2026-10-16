package handlers

import (
	"errors"
	"strings"
	"testing"

	"github.com/LovationAdmin/budget-api/utils"
)

// Every password rule the server enforces must reach the user in their language.
func TestPasswordError_CoversValidator(t *testing.T) {
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
		code, msg := passwordError(err, utils.LocaleFR)
		if code != want {
			t.Errorf("%q: code = %s (%v), want %s", pw, code, err, want)
		}
		if strings.Contains(msg, "password") || msg == "" {
			t.Errorf("%q: message not translated: %q", pw, msg)
		}
		codeEN, msgEN := passwordError(err, utils.LocaleEN)
		if codeEN != code || msgEN == msg || !strings.Contains(strings.ToLower(msgEN), "password") {
			t.Errorf("%q: English message = %q (%s)", pw, msgEN, codeEN)
		}
	}
}

func TestSignupBindError(t *testing.T) {
	code, _ := signupBindError(errors.New("Key: 'SignupRequest.Email' Error:Field validation for 'Email' failed on the 'email' tag"), utils.LocaleFR)
	if code != "email_invalid" {
		t.Errorf("email: %s", code)
	}
	code, _ = signupBindError(errors.New("Key: 'SignupRequest.Name' Error:Field validation for 'Name' failed on the 'min' tag"), utils.LocaleEN)
	if code != "name_invalid" {
		t.Errorf("name: %s", code)
	}
}

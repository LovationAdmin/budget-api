// handlers/auth_messages.go
// ============================================================================
// User-facing auth messages, in the language of the request (French or
// English, see locale.go), with a stable `code` the app can use to offer the
// right next step (log in instead, resend the email…).
// ============================================================================

package handlers

import (
	"strings"

	"github.com/LovationAdmin/budget-api/utils"
	"github.com/gin-gonic/gin"
)

// authError is the JSON body of an auth error.
func authError(code, message string) gin.H {
	return gin.H{"error": message, "code": code}
}

// say returns the French or the English message.
func say(locale, fr, en string) string {
	if locale == utils.LocaleEN {
		return en
	}
	return fr
}

// passwordError translates a utils.ValidatePassword error.
func passwordError(err error, locale string) (code, message string) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "too short"):
		return "password_too_short", say(locale, "Le mot de passe doit contenir au moins 10 caractères.", "The password must be at least 10 characters.")
	case strings.Contains(msg, "too long"):
		return "password_too_long", say(locale, "Le mot de passe ne doit pas dépasser 72 caractères.", "The password must be at most 72 characters.")
	case strings.Contains(msg, "at least 3 of"):
		return "password_too_simple", say(locale, "Le mot de passe doit mélanger au moins 3 types de caractères : minuscules, majuscules, chiffres, symboles.", "The password must mix at least 3 kinds of characters: lowercase, uppercase, digits, symbols.")
	case strings.Contains(msg, "too common"):
		return "password_common", say(locale, "Ce mot de passe est trop courant. Choisissez-en un plus personnel.", "This password is too common. Choose a more personal one.")
	case strings.Contains(msg, "email"):
		return "password_contains_email", say(locale, "Le mot de passe ne doit pas contenir votre adresse e-mail.", "The password must not contain your email address.")
	case strings.Contains(msg, "name"):
		return "password_contains_name", say(locale, "Le mot de passe ne doit pas contenir votre prénom.", "The password must not contain your first name.")
	}
	return "password_invalid", say(locale, "Ce mot de passe n'est pas accepté. Choisissez-en un autre.", "This password isn’t accepted. Choose another one.")
}

// signupBindError explains which field of the signup form is invalid.
func signupBindError(err error, locale string) (code, message string) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "'Email'"):
		return "email_invalid", say(locale, "Cette adresse e-mail n'est pas valide.", "This email address isn’t valid.")
	case strings.Contains(msg, "'Name'"):
		return "name_invalid", say(locale, "Indiquez un prénom d'au moins 2 caractères.", "Enter a first name of at least 2 characters.")
	case strings.Contains(msg, "'Password'"):
		return "password_too_long", say(locale, "Le mot de passe ne doit pas dépasser 72 caractères.", "The password must be at most 72 characters.")
	}
	return "invalid_request", say(locale, "Certains champs sont incomplets. Vérifiez le formulaire.", "Some fields are incomplete. Check the form.")
}

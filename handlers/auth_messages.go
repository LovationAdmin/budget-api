// handlers/auth_messages.go
// ============================================================================
// User-facing auth messages, in French, with a stable `code` the app can use
// to offer the right next step (log in instead, resend the email…).
// ============================================================================

package handlers

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// authError is the JSON body of an auth error.
func authError(code, message string) gin.H {
	return gin.H{"error": message, "code": code}
}

// passwordErrorFR translates a utils.ValidatePassword error.
func passwordErrorFR(err error) (code, message string) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "too short"):
		return "password_too_short", "Le mot de passe doit contenir au moins 10 caractères."
	case strings.Contains(msg, "too long"):
		return "password_too_long", "Le mot de passe ne doit pas dépasser 72 caractères."
	case strings.Contains(msg, "at least 3 of"):
		return "password_too_simple", "Le mot de passe doit mélanger au moins 3 types de caractères : minuscules, majuscules, chiffres, symboles."
	case strings.Contains(msg, "too common"):
		return "password_common", "Ce mot de passe est trop courant. Choisissez-en un plus personnel."
	case strings.Contains(msg, "email"):
		return "password_contains_email", "Le mot de passe ne doit pas contenir votre adresse e-mail."
	case strings.Contains(msg, "name"):
		return "password_contains_name", "Le mot de passe ne doit pas contenir votre prénom."
	}
	return "password_invalid", "Ce mot de passe n'est pas accepté. Choisissez-en un autre."
}

// signupBindErrorFR explains which field of the signup form is invalid.
func signupBindErrorFR(err error) (code, message string) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "'Email'"):
		return "email_invalid", "Cette adresse e-mail n'est pas valide."
	case strings.Contains(msg, "'Name'"):
		return "name_invalid", "Indiquez un prénom d'au moins 2 caractères."
	case strings.Contains(msg, "'Password'"):
		return "password_too_long", "Le mot de passe ne doit pas dépasser 72 caractères."
	}
	return "invalid_request", "Certains champs sont incomplets. Vérifiez le formulaire."
}

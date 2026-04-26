// utils/password_validator.go
// ============================================================================
// PASSWORD VALIDATOR
// ============================================================================
// Politique de mot de passe robuste :
//   - 10 caractères minimum (NIST SP 800-63B recommande 8+, on prend 10 pour
//     se laisser une marge confortable)
//   - 72 caractères maximum (limite bcrypt)
//   - Au moins 3 classes de caractères parmi : majuscule, minuscule, chiffre,
//     caractère spécial
//   - Refus d'une liste de mots de passe communs
//   - Ne doit pas contenir l'email ou le nom de l'utilisateur
//
// Cette validation est appelée APRÈS la validation Gin "binding:min=10" qui
// fournit une première barrière rapide. Le validateur ici fait la vérification
// sémantique profonde.
// ============================================================================

package utils

import (
	"errors"
	"strings"
	"unicode"
)

// PasswordPolicy définit les règles d'un mot de passe acceptable.
type PasswordPolicy struct {
	MinLength      int
	MaxLength      int
	RequireClasses int // nombre minimum de classes de caractères (sur 4)
}

// DefaultPasswordPolicy retourne la politique recommandée pour Budget Famille.
func DefaultPasswordPolicy() PasswordPolicy {
	return PasswordPolicy{
		MinLength:      10,
		MaxLength:      72, // limite imposée par bcrypt
		RequireClasses: 3,
	}
}

// commonPasswords est une petite liste de mots de passe trop fréquents.
// Source : top NIST + RockYou + spécifiques au contexte produit (FR + nom du produit).
// Volontairement courte pour éviter de gonfler le binaire ; pour une couverture
// complète, brancher sur un fichier embedded ou une API externe (Have I Been Pwned).
var commonPasswords = map[string]struct{}{
	// Top mondial
	"password": {}, "password1": {}, "password123": {}, "password!": {},
	"123456": {}, "12345678": {}, "123456789": {}, "1234567890": {},
	"qwerty": {}, "qwerty123": {}, "qwertyuiop": {},
	"admin": {}, "admin123": {}, "letmein": {}, "welcome": {}, "welcome1": {},
	"monkey": {}, "iloveyou": {}, "abc123": {}, "abcdef": {}, "abcdefgh": {},
	"trustno1": {}, "dragon": {}, "master": {}, "shadow": {},
	// Spécifique français
	"azerty": {}, "azerty123": {}, "azertyuiop": {},
	"motdepasse": {}, "motdepasse1": {}, "motdepasse123": {},
	"bonjour": {}, "soleil": {}, "doudou": {},
	// Spécifique au produit (à éviter explicitement)
	"budget": {}, "budgetfamille": {}, "famille": {}, "lovation": {},
}

// ValidatePassword vérifie un mot de passe contre la politique par défaut.
//
// Paramètres :
//   - password : le mot de passe à valider
//   - email    : email de l'utilisateur (peut être vide ; sert à empêcher le
//                mot de passe de contenir la partie locale de l'email)
//   - name     : nom de l'utilisateur (peut être vide ; sert à empêcher le
//                mot de passe de contenir un token du nom)
//
// Retourne nil si le mot de passe est acceptable, sinon une erreur décrivant
// la première règle violée (en anglais, pour rester cohérent avec les autres
// messages d'erreur de l'API ; le frontend traduit).
func ValidatePassword(password, email, name string) error {
	return ValidatePasswordWithPolicy(password, email, name, DefaultPasswordPolicy())
}

// ValidatePasswordWithPolicy est identique à ValidatePassword mais avec une
// politique personnalisable. Utile pour les tests.
func ValidatePasswordWithPolicy(password, email, name string, policy PasswordPolicy) error {
	// 1. Longueur
	if len(password) < policy.MinLength {
		return errors.New("password is too short (minimum 10 characters)")
	}
	if len(password) > policy.MaxLength {
		return errors.New("password is too long (maximum 72 characters)")
	}

	// 2. Diversité des classes de caractères
	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPunct(r), unicode.IsSymbol(r), unicode.IsSpace(r):
			hasSpecial = true
		}
	}

	classes := 0
	if hasUpper {
		classes++
	}
	if hasLower {
		classes++
	}
	if hasDigit {
		classes++
	}
	if hasSpecial {
		classes++
	}

	if classes < policy.RequireClasses {
		return errors.New("password must contain at least 3 of: uppercase letter, lowercase letter, digit, special character")
	}

	// 3. Mots de passe communs (insensible à la casse)
	lowered := strings.ToLower(password)
	if _, found := commonPasswords[lowered]; found {
		return errors.New("password is too common and easily guessable")
	}

	// 4. Doit pas contenir la partie locale de l'email
	if email != "" {
		localPart := strings.ToLower(email)
		if at := strings.Index(localPart, "@"); at > 0 {
			localPart = localPart[:at]
		}
		// Seuil de 4 caractères : on ne bloque pas pour des emails très courts
		// (ex: "ed@x.fr" : "ed" est trop court pour être discriminant).
		if len(localPart) >= 4 && strings.Contains(lowered, localPart) {
			return errors.New("password must not contain your email address")
		}
	}

	// 5. Doit pas contenir un token du nom (≥4 caractères)
	if name != "" {
		for _, token := range strings.Fields(strings.ToLower(name)) {
			if len(token) >= 4 && strings.Contains(lowered, token) {
				return errors.New("password must not contain your name")
			}
		}
	}

	return nil
}

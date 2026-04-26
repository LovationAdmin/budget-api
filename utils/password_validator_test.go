// utils/password_validator_test.go
// ============================================================================
// TESTS — Password validator
// ============================================================================
// Lancer : go test ./utils -run TestValidatePassword -v
// ============================================================================

package utils

import (
	"strings"
	"testing"
)

func TestValidatePassword_Valid(t *testing.T) {
	cases := []struct {
		name     string
		password string
		email    string
		userName string
	}{
		{
			name:     "passphrase avec espaces",
			password: "Correct horse Battery Staple 9",
			email:    "alice@example.com",
			userName: "Alice Martin",
		},
		{
			name:     "mot de passe complexe court",
			password: "MyP@ssw0rd!",
			email:    "bob@example.com",
			userName: "Bob Dupont",
		},
		{
			name:     "11 caracteres 4 classes",
			password: "Tr0ub4dor&3",
			email:    "test@example.com",
			userName: "John Doe",
		},
		{
			name:     "sans email ni nom",
			password: "Strong#Pass1234",
			email:    "",
			userName: "",
		},
		{
			name:     "exactement 10 caracteres avec 3 classes",
			password: "Abcdef1234", // upper, lower, digit = 3 classes
			email:    "",
			userName: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidatePassword(tc.password, tc.email, tc.userName); err != nil {
				t.Errorf("expected %q to be valid, got: %v", tc.password, err)
			}
		})
	}
}

func TestValidatePassword_Invalid(t *testing.T) {
	cases := []struct {
		name        string
		password    string
		email       string
		userName    string
		errContains string
	}{
		{
			name:        "trop court",
			password:    "Sh0rt!",
			errContains: "too short",
		},
		{
			name:        "trop long (>72)",
			password:    strings.Repeat("a", 73),
			errContains: "too long",
		},
		{
			name:        "uniquement minuscules + chiffres = 2 classes",
			password:    "alllowercase123",
			errContains: "at least 3",
		},
		{
			name:        "uniquement majuscules + minuscules = 2 classes",
			password:    "OnlyLettersHere",
			errContains: "at least 3",
		},
		{
			name:        "mot de passe commun (azerty)",
			password:    "azerty",
			errContains: "too short", // bloqué d'abord sur la longueur
		},
		{
			name:        "mot de passe commun rallonge",
			password:    "MotDePasse",
			errContains: "at least 3", // 2 classes : upper + lower
		},
		{
			name:        "mot de passe exactement dans la liste commune",
			password:    "Welcome1",
			errContains: "too short", // 8 chars < 10
		},
		{
			name:        "mot de passe commun avec longueur OK",
			password:    "motdepasse123", // dans la liste, 13 chars, mais 2 classes seulement
			errContains: "at least 3",    // bloqué d'abord sur la diversité
		},
		{
			name:        "contient le nom d'utilisateur",
			password:    "MartinDubois99!",
			email:       "alice@example.com",
			userName:    "Martin Dubois",
			errContains: "your name",
		},
		{
			name:        "contient la partie locale de l'email",
			password:    "alicebrown99!Z",
			email:       "alicebrown@example.com",
			userName:    "Bob Smith",
			errContains: "email",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePassword(tc.password, tc.email, tc.userName)
			if err == nil {
				t.Errorf("expected %q to be invalid, got nil", tc.password)
				return
			}
			if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
				t.Errorf("expected error to contain %q, got %q", tc.errContains, err.Error())
			}
		})
	}
}

func TestValidatePassword_EdgeCases(t *testing.T) {
	t.Run("nom court (<4 chars) ne bloque pas", func(t *testing.T) {
		// "Bob" fait 3 caractères, donc ne doit PAS être utilisé pour bloquer.
		// Le mot de passe contient "bob" mais reste valide.
		err := ValidatePassword("StrongPa$$bob1", "alice@example.com", "Bob")
		if err != nil {
			t.Errorf("expected valid (short name shouldn't block), got: %v", err)
		}
	})

	t.Run("email court (<4 chars local part) ne bloque pas", func(t *testing.T) {
		err := ValidatePassword("StrongPa$$ed12", "ed@example.com", "Some User")
		if err != nil {
			t.Errorf("expected valid (short email local shouldn't block), got: %v", err)
		}
	})

	t.Run("policy custom appliquee", func(t *testing.T) {
		strict := PasswordPolicy{MinLength: 16, MaxLength: 72, RequireClasses: 4}
		err := ValidatePasswordWithPolicy("Short#Pass1!", "", "", strict)
		if err == nil {
			t.Error("expected strict policy to reject 12-char password")
		}
	})
}

func TestDefaultPasswordPolicy(t *testing.T) {
	p := DefaultPasswordPolicy()
	if p.MinLength != 10 {
		t.Errorf("expected MinLength=10, got %d", p.MinLength)
	}
	if p.MaxLength != 72 {
		t.Errorf("expected MaxLength=72, got %d", p.MaxLength)
	}
	if p.RequireClasses != 3 {
		t.Errorf("expected RequireClasses=3, got %d", p.RequireClasses)
	}
}

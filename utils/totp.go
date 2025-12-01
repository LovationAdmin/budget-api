package utils

import (
	"github.com/pquerna/otp/totp"
)

func GenerateTOTPSecret(email string) (string, string, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Budget Famille",
		AccountName: email,
	})
	if err != nil {
		return "", "", err
	}

	return key.Secret(), key.URL(), nil
}

func VerifyTOTP(secret, code string) (bool, error) {
	valid := totp.Validate(code, secret)
	return valid, nil
}
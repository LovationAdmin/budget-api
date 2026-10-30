// utils/email_unsubscribe.go
// ============================================================================
// EMAIL LISTS — signed unsubscribe links
// ============================================================================
// Gmail and Yahoo expect bulk mail to carry a one-click unsubscribe
// (RFC 8058): a List-Unsubscribe https URL answering a POST, plus
// List-Unsubscribe-Post. Each recipient gets a token naming them and the list,
// signed with HMAC so nobody can unsubscribe someone else. Tokens do not
// expire: an old email's link must keep working.
// ============================================================================

package utils

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"strings"
)

// Email lists a user can leave.
const (
	EmailListMonthlyRecap = "monthly_recap"
	EmailListNews         = "news"
)

// IsEmailList reports whether `list` is one of the lists above.
func IsEmailList(list string) bool {
	return list == EmailListMonthlyRecap || list == EmailListNews
}

func unsubscribeSecret() []byte {
	if s := os.Getenv("EMAIL_UNSUBSCRIBE_SECRET"); s != "" {
		return []byte(s)
	}
	if s := os.Getenv("JWT_SECRET"); s != "" {
		return []byte("unsubscribe:" + s)
	}
	return []byte("budget-famille-dev-unsubscribe")
}

func unsubscribeSignature(payload string) string {
	mac := hmac.New(sha256.New, unsubscribeSecret())
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))[:32]
}

// UnsubscribeToken returns the signed token of `userID` on `list`.
func UnsubscribeToken(userID, list string) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(userID + ":" + list))
	return payload + "." + unsubscribeSignature(payload)
}

// ParseUnsubscribeToken returns the user and list of a valid token.
func ParseUnsubscribeToken(token string) (userID, list string, ok bool) {
	payload, sig, found := strings.Cut(strings.TrimSpace(token), ".")
	if !found || !hmac.Equal([]byte(sig), []byte(unsubscribeSignature(payload))) {
		return "", "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", "", false
	}
	userID, list, found = strings.Cut(string(raw), ":")
	if !found || userID == "" || !IsEmailList(list) {
		return "", "", false
	}
	return userID, list, true
}

// APIPublicURL is the API's own address, for links that must reach it
// directly (the one-click unsubscribe). Render sets RENDER_EXTERNAL_URL.
func APIPublicURL() string {
	for _, k := range []string{"API_PUBLIC_URL", "RENDER_EXTERNAL_URL"} {
		if v := strings.TrimRight(os.Getenv(k), "/"); v != "" {
			return v
		}
	}
	return "https://budget-api-778i.onrender.com"
}

func frontendURL() string {
	if v := strings.TrimRight(os.Getenv("FRONTEND_URL"), "/"); v != "" {
		return v
	}
	return "http://localhost:3000"
}

// UnsubscribeLinks are the two ways out of a list for one recipient.
type UnsubscribeLinks struct {
	// Page is the app page that unsubscribes, confirms and lets the user
	// change their mind (the footer link).
	Page string
	// OneClick is the RFC 8058 endpoint mail clients POST to.
	OneClick string
}

// UnsubscribeLinksFor returns the links of `userID` on `list`.
func UnsubscribeLinksFor(userID, list string) UnsubscribeLinks {
	token := UnsubscribeToken(userID, list)
	return UnsubscribeLinks{
		Page:     frontendURL() + "/emails/desabonnement?token=" + token,
		OneClick: APIPublicURL() + "/api/v1/email/unsubscribe?token=" + token,
	}
}

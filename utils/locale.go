// utils/locale.go
// ============================================================================
// Languages the API answers in: French (the default) and English. The app
// sends the language of the screen in Accept-Language; an account keeps its
// own (users.locale) for the emails sent when no request is there (monthly
// recap) and for those sent to it.
// ============================================================================

package utils

import "strings"

const (
	LocaleFR = "fr"
	LocaleEN = "en"
)

// NormalizeLocale returns "fr" or "en" for a language tag ("en-GB" → "en"),
// "" when it is neither.
func NormalizeLocale(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	switch {
	case tag == LocaleFR || strings.HasPrefix(tag, "fr-") || strings.HasPrefix(tag, "fr_"):
		return LocaleFR
	case tag == LocaleEN || strings.HasPrefix(tag, "en-") || strings.HasPrefix(tag, "en_"):
		return LocaleEN
	}
	return ""
}

// LocaleFromAcceptLanguage returns the first of our languages listed in an
// Accept-Language header, French when there is none.
func LocaleFromAcceptLanguage(header string) string {
	for _, part := range strings.Split(header, ",") {
		tag := strings.SplitN(strings.TrimSpace(part), ";", 2)[0]
		if l := NormalizeLocale(tag); l != "" {
			return l
		}
	}
	return LocaleFR
}

// PickLocale returns the first known language among candidates, French otherwise.
func PickLocale(candidates ...string) string {
	for _, c := range candidates {
		if l := NormalizeLocale(c); l != "" {
			return l
		}
	}
	return LocaleFR
}

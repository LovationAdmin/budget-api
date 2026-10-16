package utils

import "testing"

func TestLocaleFromAcceptLanguage(t *testing.T) {
	cases := map[string]string{
		"":                        "fr",
		"en":                      "en",
		"en-US,en;q=0.9":          "en",
		"fr-FR,fr;q=0.9,en;q=0.8": "fr",
		"de-DE,de;q=0.9,en;q=0.8": "en",
		"es-ES,es;q=0.9":          "fr",
		" EN-gb ; q=1":            "en",
	}
	for header, want := range cases {
		if got := LocaleFromAcceptLanguage(header); got != want {
			t.Errorf("LocaleFromAcceptLanguage(%q) = %q, want %q", header, got, want)
		}
	}
}

func TestPickLocale(t *testing.T) {
	if got := PickLocale("", "xx", "en-GB", "fr"); got != "en" {
		t.Errorf("PickLocale = %q, want en", got)
	}
	if got := PickLocale(); got != "fr" {
		t.Errorf("PickLocale() = %q, want fr", got)
	}
}

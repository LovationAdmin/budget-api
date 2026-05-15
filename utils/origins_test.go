package utils

import (
	"os"
	"testing"
)

func TestIsAllowedOrigin(t *testing.T) {
	old := os.Getenv("FRONTEND_URL")
	defer os.Setenv("FRONTEND_URL", old)
	os.Setenv("FRONTEND_URL", "https://my-frontend.example.com")

	cases := []struct {
		origin string
		want   bool
		name   string
	}{
		{"", false, "empty"},
		{"https://my-frontend.example.com", true, "configured frontend"},
		{"https://budgetfamille.com", true, "prod domain"},
		{"https://www.budgetfamille.com", true, "www prod domain"},
		{"http://localhost:3000", true, "local dev"},
		{"http://localhost:5173", true, "vite dev"},
		{"https://budget-ui.vercel.app", true, "vercel production preview"},
		{"https://budget-ui-two.vercel.app", true, "vercel named preview"},
		{"https://budget-ui-git-feat-foo-lovationadmin.vercel.app", true, "vercel branch preview"},
		{"https://budget-abc123-lovationadmin.vercel.app", true, "vercel deploy preview"},
		{"https://evil.com", false, "unknown host"},
		{"https://budget-ui.vercel.app.evil.com", false, "subdomain trick"},
		{"http://budget-ui.vercel.app", false, "http on vercel"},
		{"https://OTHER.vercel.app", false, "other vercel app"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsAllowedOrigin(c.origin); got != c.want {
				t.Errorf("IsAllowedOrigin(%q) = %v, want %v", c.origin, got, c.want)
			}
		})
	}
}

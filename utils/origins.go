// utils/origins.go
// ============================================================================
// ORIGIN ALLOWLIST — shared by CORS (HTTP API) and the WebSocket upgrader.
// ============================================================================
//
// Previously the two layers maintained separate hard-coded lists, and the WS
// path silently rejected Vercel preview URLs that CORS happily accepted.
// Routing both through this helper keeps them in lockstep.
// ============================================================================

package utils

import (
	"os"
	"regexp"
)

// vercelPreviewPattern matches the auto-generated Vercel preview hosts:
// "budget-ui.vercel.app", "budget-ui-two.vercel.app",
// "budget-ui-git-feature-orgname.vercel.app",
// "budget-ui-abc123-orgname.vercel.app", etc.
var vercelPreviewPattern = regexp.MustCompile(`^https://budget-[a-z0-9\-]+\.vercel\.app$`)

// fixedAllowedOrigins lists the production + local-dev hosts that should be
// accepted regardless of FRONTEND_URL. The configured FRONTEND_URL is
// appended at runtime so deploys with a custom domain still work.
var fixedAllowedOrigins = []string{
	"https://budgetfamille.com",
	"https://www.budgetfamille.com",
	"http://localhost:3000",
	"http://localhost:5173",
}

// IsAllowedOrigin returns true when `origin` may access the API — both for
// CORS preflight (HTTP routes) and the WebSocket Upgrader's CheckOrigin.
// An empty origin returns false: requests from same-origin / non-browser
// clients shouldn't be passing through the browser-origin allowlist anyway.
func IsAllowedOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	if frontendURL := os.Getenv("FRONTEND_URL"); frontendURL != "" && origin == frontendURL {
		return true
	}
	for _, o := range fixedAllowedOrigins {
		if origin == o {
			return true
		}
	}
	if vercelPreviewPattern.MatchString(origin) {
		return true
	}
	return false
}

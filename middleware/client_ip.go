// middleware/client_ip.go
// ============================================================================
// ADRESSE IP DU VISITEUR
// ============================================================================
// Sur Render, les requêtes arrivent des proxys internes (10.x) avec
// « X-Forwarded-For: <client>, <edge Cloudflare> » : l'entrée la plus à gauche
// est fournie par l'appelant, donc falsifiable. Cloudflare, devant tous les
// services Render, renseigne CF-Connecting-IP avec l'IP réelle du visiteur et
// écrase toute valeur envoyée par le client : c'est l'en-tête que Render
// recommande. X-Forwarded-For n'est jamais pris en compte.
//
// Sans cette configuration, c.ClientIP() renvoie l'adresse du proxy : toutes
// les limites « par IP » (inscription, réinitialisation du mot de passe,
// simulateur public) étaient partagées par l'ensemble des visiteurs.
// ============================================================================

package middleware

import (
	"os"

	"github.com/gin-gonic/gin"
)

// ConfigureClientIP makes c.ClientIP() return the visitor's address, read
// from the header set by the edge: CLIENT_IP_HEADER if set, otherwise
// CF-Connecting-IP when running on Render (RENDER is set by the platform).
// Elsewhere (local dev, no edge) the header could be forged, so the TCP peer
// address is used. Returns the header in use ("" if none).
func ConfigureClientIP(engine *gin.Engine) string {
	header := os.Getenv("CLIENT_IP_HEADER")
	if header == "" && os.Getenv("RENDER") != "" {
		header = gin.PlatformCloudflare
	}
	engine.TrustedPlatform = header
	return header
}

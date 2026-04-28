// middleware/sentry.go
// ============================================================================
// SENTRY MIDDLEWARE
// ============================================================================
// Trois rôles :
//   1. Recovery : capture les panics avec stack trace, retourne 500 propre
//   2. Status capture : envoie à Sentry les responses 5xx (les bugs serveur)
//   3. User tagging : attache user_id depuis le context Gin (après auth)
//
// Doit être appliqué APRÈS le middleware CORS et AVANT les routes business.
// L'ordre dans main.go importe :
//   router.Use(cors.New(...))     // CORS first
//   router.Use(SentryMiddleware()) // ← ici
//   router.Use(loggingMiddleware)  // logs
//   router.Use(rateLimiter)        // rate limit
// ============================================================================

package middleware

import (
	"fmt"
	"runtime/debug"

	"github.com/getsentry/sentry-go"
	"github.com/gin-gonic/gin"

	"github.com/LovationAdmin/budget-api/utils"
)

// SentryMiddleware est le middleware principal Sentry pour Gin.
// Sans-effet si Sentry n'est pas initialisé (utils.InitSentry pas appelé).
func SentryMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Cloner le hub pour que ce request ait son propre scope
		// (sinon les tags fuiraient entre requêtes concurrentes)
		hub := sentry.CurrentHub().Clone()
		c.Set("sentry_hub", hub)

		// Tagger le scope avec les infos de la requête
		hub.Scope().SetTag("http.method", c.Request.Method)
		hub.Scope().SetTag("http.path", c.FullPath()) // pattern, pas URL réelle
		hub.Scope().SetRequest(c.Request)

		// Recovery : si la handler panique, on capture et on répond 500
		defer func() {
			if r := recover(); r != nil {
				// Capture avec stack trace
				err := fmt.Errorf("panic: %v\n%s", r, debug.Stack())
				utils.SafeError("PANIC recovered: %v", r)

				if utils.IsSentryEnabled() {
					hub.RecoverWithContext(c.Request.Context(), r)
					hub.Flush(0) // best-effort flush avant la réponse
				}

				// Réponse 500 propre, sans leak d'info
				c.AbortWithStatusJSON(500, gin.H{
					"error": "Internal server error",
				})
				_ = err // évite l'unused warning
			}
		}()

		c.Next()

		// Après la handler : si le statut est 5xx, capture
		// (les 4xx sont des erreurs métier normales, pas des bugs)
		status := c.Writer.Status()
		if status >= 500 && utils.IsSentryEnabled() {
			hub.Scope().SetTag("http.status_code", fmt.Sprintf("%d", status))
			hub.CaptureMessage(fmt.Sprintf(
				"5xx response: %s %s -> %d",
				c.Request.Method,
				c.FullPath(),
				status,
			))
		}
	}
}

// SentryUserTagger attache l'user ID au scope Sentry après authentification.
// À appliquer APRÈS le AuthMiddleware (qui set "user_id" dans le context).
//
// Note : on tag le hub cloné par SentryMiddleware ; si SentryMiddleware n'a
// pas été appliqué avant, ce tag finit dans le hub global (toujours OK,
// juste un peu moins propre).
func SentryUserTagger() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := GetUserID(c)
		if userID != "" && utils.IsSentryEnabled() {
			if hub, ok := c.Get("sentry_hub"); ok {
				if h, ok := hub.(*sentry.Hub); ok {
					h.Scope().SetUser(sentry.User{
						ID: utils.MaskID(userID),
					})
				}
			} else {
				// Fallback : hub global
				utils.SetUserContext(userID)
			}
		}
		c.Next()
	}
}

// utils/sentry.go
// ============================================================================
// SENTRY INTEGRATION
// ============================================================================
// Init Sentry au boot, helpers de capture, et wrappers contextuels.
//
// Design :
//   - Si SENTRY_DSN n'est pas défini, tout devient no-op (zero impact en dev)
//   - Capture* fonctions safe à appeler partout : si Sentry n'est pas init,
//     elles ne font rien (pas de panic, pas d'erreur)
//   - Les données sensibles sont déjà masquées AVANT que SafeError ne pousse
//     à Sentry (passage par MaskString) → Sentry ne reçoit jamais d'IBAN,
//     d'email en clair, etc.
//
// Variables d'environnement :
//   - SENTRY_DSN          : DSN du projet Sentry (obligatoire pour activer)
//   - SENTRY_ENVIRONMENT  : "production", "staging", "development" (auto-set
//                           depuis ENVIRONMENT si non précisé)
//   - SENTRY_RELEASE      : tag de release pour les sourcemaps (optionnel)
//   - SENTRY_TRACES_RATE  : sampling rate des traces, 0.0 à 1.0 (default 0.0)
// ============================================================================

package utils

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/getsentry/sentry-go"
)

// sentryEnabled est vrai uniquement si SENTRY_DSN est défini ET que l'init
// a réussi. Toutes les fonctions de capture vérifient ce flag.
var sentryEnabled bool

// InitSentry initialise Sentry. À appeler une seule fois au démarrage.
// Si SENTRY_DSN n'est pas défini, Sentry reste désactivé (no-op).
//
// Retourne une fonction de flush à appeler dans un defer pour s'assurer
// que les events en attente sont envoyés avant l'arrêt du processus.
func InitSentry(appName, version string) func() {
	dsn := os.Getenv("SENTRY_DSN")
	if dsn == "" {
		SafeInfo("Sentry: disabled (SENTRY_DSN not set)")
		return func() {} // no-op flush
	}

	environment := os.Getenv("SENTRY_ENVIRONMENT")
	if environment == "" {
		environment = GetEnvMode() // "production" ou "development"
	}

	tracesRate := 0.0
	if rateStr := os.Getenv("SENTRY_TRACES_RATE"); rateStr != "" {
		if r, err := strconv.ParseFloat(rateStr, 64); err == nil {
			tracesRate = r
		}
	}

	release := os.Getenv("SENTRY_RELEASE")
	if release == "" {
		release = fmt.Sprintf("%s@%s", appName, version)
	}

	err := sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		Environment:      environment,
		Release:          release,
		TracesSampleRate: tracesRate,
		// AttachStacktrace : ajoute une stack trace même quand on capture un
		// message simple (pas qu'une erreur). Très utile pour le debug.
		AttachStacktrace: true,
		// BeforeSend : dernière chance de filtrer/masquer avant envoi.
		// On masque les données sensibles dans le message au cas où
		// quelqu'un aurait passé une string non-Safe*.
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			if event.Message != "" {
				event.Message = MaskString(event.Message)
			}
			for i, ex := range event.Exception {
				event.Exception[i].Value = MaskString(ex.Value)
			}
			return event
		},
	})

	if err != nil {
		SafeError("Sentry: init failed: %v", err)
		return func() {}
	}

	sentryEnabled = true
	SafeInfo("Sentry: enabled (env=%s, release=%s, traces=%.2f)", environment, release, tracesRate)

	// Flush à appeler avant l'arrêt
	return func() {
		sentry.Flush(2 * time.Second)
	}
}

// IsSentryEnabled retourne si Sentry est actif. Utile pour conditionner
// du code coûteux qu'on ne veut pas exécuter si Sentry est off.
func IsSentryEnabled() bool {
	return sentryEnabled
}

// ============================================================================
// CAPTURE FUNCTIONS
// ============================================================================

// CaptureError envoie une erreur à Sentry. No-op si Sentry n'est pas init.
// Le message est masqué (PII) avant envoi.
func CaptureError(err error) {
	if !sentryEnabled || err == nil {
		return
	}
	// Wrapper l'erreur pour qu'elle passe par BeforeSend qui masquera
	// le message si nécessaire.
	sentry.CaptureException(err)
}

// CaptureMessage envoie un message à Sentry avec un level (info, warning, error).
// Useful pour les events business (ex: "Refresh token reuse detected").
func CaptureMessage(message string, level sentry.Level) {
	if !sentryEnabled {
		return
	}
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetLevel(level)
		sentry.CaptureMessage(MaskString(message))
	})
}

// CaptureWarning est un raccourci pour CaptureMessage avec level=warning.
func CaptureWarning(format string, args ...interface{}) {
	CaptureMessage(fmt.Sprintf(format, args...), sentry.LevelWarning)
}

// CaptureSecurityEvent envoie un event de sécurité (refresh token reuse,
// brute-force détecté, etc.). Tag spécial pour faciliter le filtrage Sentry.
func CaptureSecurityEvent(eventType, message string, tags map[string]string) {
	if !sentryEnabled {
		return
	}
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetLevel(sentry.LevelWarning)
		scope.SetTag("security_event", eventType)
		scope.SetTag("category", "security")
		for k, v := range tags {
			scope.SetTag(k, v)
		}
		sentry.CaptureMessage(MaskString(message))
	})
}

// SetUserContext attache un user_id au scope Sentry courant (par requête).
// Appelé typiquement par le middleware d'auth après validation du JWT.
func SetUserContext(userID string) {
	if !sentryEnabled || userID == "" {
		return
	}
	hub := sentry.CurrentHub()
	if hub == nil {
		return
	}
	hub.Scope().SetUser(sentry.User{
		ID: MaskID(userID), // masquer en prod
	})
}

// CaptureMessageAsError envoie un message comme une erreur à Sentry.
// Différence avec CaptureMessage : utilise le level "error" et apparaît
// dans le dashboard Sentry comme une vraie erreur (avec stack trace).
//
// Utilisé en interne par SafeError pour ne pas avoir à wrapper en error{}.
func CaptureMessageAsError(message string) {
	if !sentryEnabled {
		return
	}
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetLevel(sentry.LevelError)
		sentry.CaptureMessage(message) // déjà masqué par l'appelant
	})
}

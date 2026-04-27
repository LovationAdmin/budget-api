// middleware/auth_ratelimit.go
// ============================================================================
// AUTH RATE LIMITERS — Pass 3
// ============================================================================
// Configurations prêtes à l'emploi pour chaque endpoint sensible.
//
// Choix de design :
//
// LOGIN — par EMAIL, pas par IP
//   Raison : un attaquant qui veut casser un compte cible un email donné.
//   Une limite "5 échecs / 15 min par email" l'empêche de tester plus de
//   480 mots de passe par jour, ce qui rend le brute-force impraticable.
//   Limiter par IP serait inutile (attaquants utilisent VPN/Tor) et briserait
//   les utilisateurs derrière NAT (familles, entreprises).
//   SkipOnSuccess → un user qui se logge correctement ne grille pas son quota.
//
// SIGNUP — par IP
//   Raison : empêcher la création massive de comptes spam depuis une même
//   source. 3 / heure laisse les usages familiaux légitimes (1-2 comptes
//   par foyer) tout en bloquant les bots.
//
// FORGOT-PASSWORD — par EMAIL
//   Raison : empêcher le "mail bombing" (envoi répété de mails de reset
//   pour harceler quelqu'un). 3 / heure suffit largement pour un usage
//   légitime (ces mails arrivent vite, pas besoin d'en demander plein).
//
// REFRESH — par cookie
//   Raison : un user qui ouvre 10 onglets fait potentiellement 10 refresh
//   simultanés. Mais un attaquant qui aurait volé un refresh token essaiera
//   de le rafraîchir en boucle. 60 / heure laisse passer les usages normaux
//   et flague les boucles.
//
// RESET-PASSWORD — par IP
//   Raison : protéger contre le brute-force du token de reset (qui est un
//   UUID, donc 122 bits d'entropie — déjà incassable, mais ceinture+bretelles).
// ============================================================================

package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
)

// LoginRateLimit limite les tentatives de login : 5 échecs / 15 min par email.
// Les logins réussis ne consomment PAS de tentative.
func LoginRateLimit() gin.HandlerFunc {
	return NewLimiter(LimiterConfig{
		Name:          "login",
		Limit:         5,
		Window:        15 * time.Minute,
		KeyFunc:       KeyByEmailFromBody,
		SkipOnSuccess: true,
	})
}

// SignupRateLimit limite la création de comptes : 3 / heure par IP.
func SignupRateLimit() gin.HandlerFunc {
	return NewLimiter(LimiterConfig{
		Name:    "signup",
		Limit:   3,
		Window:  1 * time.Hour,
		KeyFunc: KeyByIP,
	})
}

// ForgotPasswordRateLimit : 3 demandes de reset / heure par email.
func ForgotPasswordRateLimit() gin.HandlerFunc {
	return NewLimiter(LimiterConfig{
		Name:    "forgot_password",
		Limit:   3,
		Window:  1 * time.Hour,
		KeyFunc: KeyByEmailFromBody,
	})
}

// ResetPasswordRateLimit : 5 tentatives / 15 min par IP (protection token).
func ResetPasswordRateLimit() gin.HandlerFunc {
	return NewLimiter(LimiterConfig{
		Name:    "reset_password",
		Limit:   5,
		Window:  15 * time.Minute,
		KeyFunc: KeyByIP,
	})
}

// RefreshRateLimit : 60 / heure par cookie (ou IP si pas de cookie).
// SkipOnSuccess car un refresh réussi est une opération légitime fréquente
// (potentiellement toutes les 15 min selon JWT_EXPIRY).
func RefreshRateLimit() gin.HandlerFunc {
	return NewLimiter(LimiterConfig{
		Name:          "refresh",
		Limit:         60,
		Window:        1 * time.Hour,
		KeyFunc:       KeyByRefreshCookie,
		SkipOnSuccess: true,
	})
}

// VerifyResendRateLimit : 3 renvois de mail de vérification / heure par email.
func VerifyResendRateLimit() gin.HandlerFunc {
	return NewLimiter(LimiterConfig{
		Name:    "verify_resend",
		Limit:   3,
		Window:  1 * time.Hour,
		KeyFunc: KeyByEmailFromBody,
	})
}

// middleware/ratelimit.go
// ============================================================================
// RATE LIMITER — Pass 3
// ============================================================================
// Réécriture complète du rate limiter :
//   - Fixed-window in-memory avec sync.Map (concurrent-safe sans verrou global)
//   - Clé extraite via une fonction injectable (par IP, par email, par cookie...)
//   - Headers HTTP standards : X-RateLimit-Limit, X-RateLimit-Remaining,
//     X-RateLimit-Reset, Retry-After
//   - Mode "skipOnSuccess" : pour le login, on ne consomme une tentative
//     que si elle ÉCHOUE — un user qui se connecte normalement ne grille pas
//     ses 5 tentatives en se connectant 5 fois de suite.
//
// L'ancien `RateLimiter()` est conservé en bas du fichier pour ne rien
// casser, mais on ne l'utilise plus depuis main.go (voir patch main.go).
//
// Migration vers Redis : remplacer le `store` par une implémentation Redis
// avec INCR + EXPIRE atomic. L'API publique du middleware ne change pas.
// ============================================================================

package middleware

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

// ============================================================================
// CONFIG D'UN LIMITER
// ============================================================================

// LimiterConfig décrit une politique de limitation pour un endpoint.
type LimiterConfig struct {
	// Limit est le nombre maximum de requêtes autorisées dans la fenêtre.
	Limit int

	// Window est la durée de la fenêtre fixe.
	Window time.Duration

	// KeyFunc retourne la clé de comptage (IP, email, cookie, etc.).
	// Si elle retourne "" + skip=true, la requête passe sans être comptée
	// (utile pour les requêtes mal formées qu'on laisse glisser pour
	// retomber sur la validation Gin habituelle).
	KeyFunc func(c *gin.Context) (key string, skip bool)

	// Name est un identifiant lisible utilisé dans les logs et messages
	// d'erreur (ex: "login", "signup", "forgot_password").
	Name string

	// SkipOnSuccess : si true, la tentative n'est COMPTÉE qu'après la réponse,
	// et seulement si le statut HTTP indique un échec (>= 400).
	// Indispensable pour le login : sinon 5 connexions normales d'un user
	// consomment ses 5 tentatives autorisées.
	SkipOnSuccess bool
}

// ============================================================================
// STORE
// ============================================================================

// counter représente un compteur dans une fenêtre.
type counter struct {
	count     int64
	resetTime time.Time
}

// store est un map clé→compteur, concurrent-safe.
type store struct {
	data sync.Map // map[string]*counter
}

// hit incrémente le compteur pour `key` et retourne :
//   - allowed : false si la limite est atteinte
//   - remaining : nombre de requêtes restantes
//   - resetAt : quand la fenêtre se réinitialise
//   - count : nombre actuel après incrémentation
func (s *store) hit(key string, limit int, window time.Duration) (allowed bool, remaining int, resetAt time.Time, count int64) {
	now := time.Now()
	resetTime := now.Add(window)

	value, _ := s.data.LoadOrStore(key, &counter{count: 0, resetTime: resetTime})
	c := value.(*counter)

	// Si la fenêtre actuelle a expiré, on la réinitialise atomiquement
	if now.After(c.resetTime) {
		// Race possible ici si plusieurs requêtes arrivent simultanément, mais
		// l'effet est bénin (on laisse passer 1-2 requêtes en plus au pire).
		// Pour un rate limit strict, passer en CAS-loop ou en Redis.
		atomic.StoreInt64(&c.count, 0)
		c.resetTime = resetTime
	}

	newCount := atomic.AddInt64(&c.count, 1)
	rem := limit - int(newCount)
	if rem < 0 {
		rem = 0
	}

	return newCount <= int64(limit), rem, c.resetTime, newCount
}

// decrement retire 1 au compteur — utilisé par SkipOnSuccess pour annuler
// le hit si la requête a finalement réussi.
func (s *store) decrement(key string) {
	if value, ok := s.data.Load(key); ok {
		c := value.(*counter)
		// Min 0 : on ne descend pas en négatif
		for {
			cur := atomic.LoadInt64(&c.count)
			if cur <= 0 {
				return
			}
			if atomic.CompareAndSwapInt64(&c.count, cur, cur-1) {
				return
			}
		}
	}
}

// cleanup supprime les entrées dont la fenêtre est expirée depuis plus
// de `grace`. Appelé périodiquement par une goroutine.
func (s *store) cleanup(grace time.Duration) {
	cutoff := time.Now().Add(-grace)
	s.data.Range(func(key, value interface{}) bool {
		c := value.(*counter)
		if c.resetTime.Before(cutoff) {
			s.data.Delete(key)
		}
		return true
	})
}

// Store global partagé par tous les limiters
var globalStore = &store{}

// init lance le cleanup périodique
func init() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			// On garde les entrées 1h après expiration pour éviter de
			// re-créer un compteur en boucle pour un même attaquant
			globalStore.cleanup(1 * time.Hour)
		}
	}()
}

// ============================================================================
// MIDDLEWARE GENERIC
// ============================================================================

// NewLimiter crée un middleware Gin pour la config donnée.
func NewLimiter(cfg LimiterConfig) gin.HandlerFunc {
	if cfg.KeyFunc == nil {
		cfg.KeyFunc = KeyByIP
	}
	if cfg.Name == "" {
		cfg.Name = "rate_limit"
	}

	return func(c *gin.Context) {
		key, skip := cfg.KeyFunc(c)
		if skip || key == "" {
			c.Next()
			return
		}

		// Préfixer la clé avec le nom du limiter pour éviter les collisions
		// entre limiters distincts qui partagent le même store global.
		fullKey := cfg.Name + ":" + key

		allowed, remaining, resetAt, _ := globalStore.hit(fullKey, cfg.Limit, cfg.Window)

		// Headers informatifs (toujours, même si limite atteinte)
		c.Header("X-RateLimit-Limit", strconv.Itoa(cfg.Limit))
		c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))
		c.Header("X-RateLimit-Reset", strconv.FormatInt(resetAt.Unix(), 10))

		if !allowed {
			retryAfter := int(time.Until(resetAt).Seconds())
			if retryAfter < 1 {
				retryAfter = 1
			}
			c.Header("Retry-After", strconv.Itoa(retryAfter))
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":       fmt.Sprintf("Too many requests, please try again in %d seconds", retryAfter),
				"retry_after": retryAfter,
				"limit":       cfg.Limit,
				"window_seconds": int(cfg.Window.Seconds()),
			})
			c.Abort()
			return
		}

		// Si SkipOnSuccess, on annule le hit après coup si la requête a réussi
		if cfg.SkipOnSuccess {
			c.Next()
			// Si statut < 400 (success), on rembourse la tentative
			if c.Writer.Status() < 400 {
				globalStore.decrement(fullKey)
			}
			return
		}

		c.Next()
	}
}

// ============================================================================
// KEY EXTRACTORS RÉUTILISABLES
// ============================================================================

// KeyByIP utilise l'adresse IP du client. Présuppose que les trusted proxies
// sont configurés dans Gin (sinon retourne l'IP du proxy).
func KeyByIP(c *gin.Context) (string, bool) {
	ip := c.ClientIP()
	if ip == "" {
		return "", true // skip si IP indisponible
	}
	return "ip:" + ip, false
}

// KeyByEmailFromBody lit le champ "email" du body JSON. Utilise une copie
// du body pour ne pas le consommer (Gin a déjà bufferisé via ShouldBindJSON
// dans le handler — mais ici on est avant le handler, donc on lit-rewind).
//
// Pour les routes login/signup/forgot-password où le body contient "email".
func KeyByEmailFromBody(c *gin.Context) (string, bool) {
	// On ne peut pas re-lire c.Request.Body après que le handler l'ait consommé.
	// Stratégie : on lit le body, on le copie, on le restaure pour le handler.
	body, err := readBody(c)
	if err != nil || len(body) == 0 {
		return "", true
	}

	email := extractEmailField(body)
	if email == "" {
		return "", true // pas d'email dans le body → on laisse passer (Gin renverra 400)
	}
	// Normaliser : casse + espaces
	email = normalizeEmail(email)
	return "email:" + email, false
}

// KeyByRefreshCookie utilise le cookie "rt" pour limiter les refresh.
// Si pas de cookie, on retombe sur l'IP.
func KeyByRefreshCookie(c *gin.Context) (string, bool) {
	if cookie, err := c.Cookie("rt"); err == nil && cookie != "" {
		// On hash légèrement pour ne pas exposer le token dans les logs
		return "rt:" + shortHash(cookie), false
	}
	return KeyByIP(c)
}

// ============================================================================
// HELPERS
// ============================================================================

// readBody lit le body et le restaure pour les middlewares/handlers suivants.
func readBody(c *gin.Context) ([]byte, error) {
	if c.Request.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, err
	}
	// Restaurer le body pour le handler
	c.Request.Body = io.NopCloser(bytes.NewBuffer(body))
	return body, nil
}

// extractEmailField fait un parse JSON minimal pour récupérer "email".
// On évite json.Unmarshal complet dans le hot path : un simple scan suffit.
func extractEmailField(body []byte) string {
	var probe struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return ""
	}
	return probe.Email
}

func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// shortHash retourne un hash court (8 hex chars) — anti-leak en logs.
func shortHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:4])
}

// ============================================================================
// COMPAT : ancien RateLimiter() — DEPRECATED, conservé pour ne rien casser
// ============================================================================

// RateLimiter retourne un middleware compatible avec l'ancienne signature.
// DEPRECATED : utiliser NewLimiter(LimiterConfig{...}) à la place.
// Cette fonction est gardée pour ne pas casser les imports existants si
// quelqu'un l'utilise encore quelque part — main.go ne l'utilise plus
// après le patch Pass 3.
func RateLimiter() gin.HandlerFunc {
	return NewLimiter(LimiterConfig{
		Name:    "global_legacy",
		Limit:   1000, // valeur volontairement très élevée — ne sert plus en pratique
		Window:  time.Minute,
		KeyFunc: KeyByIP,
	})
}

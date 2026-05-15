// main.go
// ============================================================================
// BUDGET FAMILLE - API BACKEND
// ============================================================================
// VERSION 2.4.1 — CORS robuste (AllowOriginFunc + Vercel previews + X-Admin-Secret)
// ============================================================================

package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/LovationAdmin/budget-api/config"
	"github.com/LovationAdmin/budget-api/handlers"
	"github.com/LovationAdmin/budget-api/middleware"
	"github.com/LovationAdmin/budget-api/routes"
	"github.com/LovationAdmin/budget-api/services"
	"github.com/LovationAdmin/budget-api/utils"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

const (
	AppName    = "Budget Famille API"
	AppVersion = "2.4.1"
)

// CORS allowlist lives in utils.IsAllowedOrigin so the WebSocket upgrader can
// share the same matcher — see handlers/ws.go.

func main() {
	// Charger les variables d'environnement
	if err := godotenv.Load(); err != nil {
		utils.SafeInfo("No .env file found, using environment variables")
	}

	// Récupérer le port
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// ✅ LOGGING DE DÉMARRAGE SÉCURISÉ
	utils.LogStartup(AppName, AppVersion, port)

	// Connexion à la base de données
	db, err := config.InitDB()
	if err != nil {
		utils.SafeError("Failed to connect to database: %v", err)
		os.Exit(1)
	}
	defer db.Close()

	utils.SafeInfo("Database connected successfully")

	// Migrations
	if err := config.RunMigrations(db); err != nil {
		utils.SafeError("Failed to run migrations: %v", err)
		os.Exit(1)
	}

	// Init Sentry (no-op si SENTRY_DSN n'est pas défini)
	flushSentry := utils.InitSentry(AppName, AppVersion)
	defer flushSentry()

	// Démarrer le nettoyage automatique du cache
	go scheduleCacheCleaning(db)

	// Démarrer l'envoi automatique du récap mensuel (1er du mois, 09:00 Paris)
	go scheduleMonthlyRecap(db)

	// Initialiser le handler WebSocket
	wsHandler := handlers.NewWSHandler()

	// Créer le routeur Gin
	router := gin.Default()

	// ============================================================================
	// TRUSTED PROXIES
	// ============================================================================
	// En prod, on est derrière le proxy Render → c.ClientIP() retournerait
	// l'IP du proxy si on ne configure pas la confiance. On accepte les headers
	// X-Forwarded-For en prod ; un attaquant pourrait spoofer son IP, mais
	// notre rate limiting est principalement par EMAIL (pas par IP) pour les
	// endpoints sensibles, donc l'impact est limité.
	if isProd := os.Getenv("ENVIRONMENT") == "production"; isProd {
		// nil = trust all (X-Forwarded-For pris tel quel, comme c'est Render qui le set)
		_ = router.SetTrustedProxies(nil)
		utils.SafeInfo("Trusted proxies: ALL (production mode)")
	} else {
		// En dev, pas de proxy → trust uniquement loopback
		_ = router.SetTrustedProxies([]string{"127.0.0.1", "::1"})
	}

	// ============================================================================
	// CONFIGURATION CORS — v2 (robuste, supporte Vercel previews + X-Admin-Secret)
	// ============================================================================
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:3000"
	}

	utils.SafeInfo("CORS configured via utils.IsAllowedOrigin (frontend=%s)", frontendURL)

	corsConfig := cors.Config{
		// AllowOriginFunc prend le pas sur AllowOrigins → décision dynamique.
		// La même fonction gouverne le upgrade WebSocket (handlers/ws.go) pour
		// éviter les divergences entre les deux couches.
		AllowOriginFunc: func(origin string) bool {
			if utils.IsAllowedOrigin(origin) {
				return true
			}
			utils.SafeWarn("CORS rejected origin: %s", origin)
			return false
		},
		AllowMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowHeaders: []string{
			"Origin",
			"Content-Type",
			"Authorization",
			"X-Admin-Secret", // ← requis pour /admin/stats
			"X-Requested-With",
			"Accept",
			"Upgrade",
			"Connection",
			"Sec-WebSocket-Key",
			"Sec-WebSocket-Version",
			"Sec-WebSocket-Extensions",
		},
		ExposeHeaders: []string{
			"Content-Length",
			"X-RateLimit-Limit",
			"X-RateLimit-Remaining",
			"X-RateLimit-Reset",
			"Retry-After",
		},
		AllowCredentials: true,
		MaxAge:           86400, // 24h cache du préflight
	}

	// Appliquer CORS en premier
	router.Use(cors.New(corsConfig))

	// Sentry middleware : capture des panics + 5xx (no-op si Sentry pas init)
	router.Use(middleware.SentryMiddleware())

	// ============================================================================
	// WEBSOCKET ROUTE (avant le middleware de logging pour éviter les blocages)
	// ============================================================================
	router.GET("/api/v1/ws/budgets/:id", wsHandler.HandleWS)

	// ============================================================================
	// MIDDLEWARE DE LOGGING SÉCURISÉ
	// ============================================================================
	router.Use(func(c *gin.Context) {
		// Skip les health checks pour réduire le bruit
		if c.Request.URL.Path == "/health" {
			c.Next()
			return
		}

		// Skip le logging détaillé pour WebSocket
		if c.Request.URL.Path == "/api/v1/ws/budgets/:id" {
			c.Next()
			return
		}

		start := time.Now()

		// Récupérer l'ID utilisateur si disponible
		userID := c.GetString("user_id")
		if userID == "" {
			userID = "anonymous"
		}

		c.Next()

		duration := time.Since(start)

		// ✅ LOGGING SÉCURISÉ - Utilise LogAPIRequest
		utils.LogAPIRequest(
			c.Request.Method,
			c.Request.URL.Path,
			userID,
			c.Writer.Status(),
			duration.String(),
		)
	})

	// Rate Limiter : on ne met PLUS de limit global ici.
	// Les limits sont appliqués par endpoint via les middlewares dédiés
	// (voir routes/routes.go). Voir middleware/auth_ratelimit.go pour la liste.

	// ============================================================================
	// HEALTH CHECK
	// ============================================================================
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "healthy",
			"version": AppVersion,
			"mode":    utils.GetEnvMode(),
		})
	})

	// ============================================================================
	// ROUTES API v1
	// ============================================================================
	v1 := router.Group("/api/v1")
	{
		// Initialiser le service refresh tokens
		refreshLifetime := handlers.ParseRefreshLifetime()
		refreshService := services.NewRefreshTokenService(db, refreshLifetime)
		utils.SafeInfo("Refresh tokens service initialized (lifetime=%s)", refreshLifetime)

		// Cleanup périodique des tokens expirés
		go scheduleRefreshTokenCleanup(refreshService)

		// 1. Routes Publiques (Auth, Admin)
		routes.SetupAuthRoutes(v1, db, refreshService)
		routes.SetupAdminRoutes(v1, db)

		// FIXED: SetupAdminSuggestionsRoutes ne prend que 2 arguments
		routes.SetupAdminSuggestionsRoutes(v1, db)

		// 2. Routes Protégées (Nécessitent une authentification)
		// On crée un groupe protégé qui applique le middleware d'auth
		protected := v1.Group("/")
		protected.Use(middleware.AuthMiddleware())
		protected.Use(middleware.SentryUserTagger())
		{
			// FIXED: Appel explicite des routes au lieu de "SetupProtectedRoutes"
			routes.SetupBudgetRoutes(protected, db, wsHandler)
			routes.SetupUserRoutes(protected, db, refreshService)
			routes.SetupInvitationRoutes(protected, db)
			routes.SetupEnableBankingRoutes(protected, db)
			routes.SetupMarketSuggestionsRoutes(protected, db, wsHandler)
		}
	}

	// ============================================================================
	// DÉMARRAGE DU SERVEUR
	// ============================================================================
	utils.SafeInfo("Server starting on port %s", port)

	if err := router.Run(":" + port); err != nil {
		utils.SafeError("Server failed to start: %v", err)
		os.Exit(1)
	}
}

// ============================================================================
// NETTOYAGE AUTOMATIQUE DU CACHE
// ============================================================================

func scheduleCacheCleaning(db *sql.DB) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	// Premier nettoyage au démarrage (après 1 minute)
	time.Sleep(1 * time.Minute)
	cleanExpiredCache(db)

	for range ticker.C {
		cleanExpiredCache(db)
	}
}

func cleanExpiredCache(db *sql.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Nettoyer les suggestions expirées
	result, err := db.ExecContext(ctx, `
		DELETE FROM market_suggestions 
		WHERE expires_at < NOW()
	`)

	if err != nil {
		utils.SafeWarn("Failed to clean expired suggestions: %v", err)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		utils.SafeInfo("Cleaned %d expired cache entries", rowsAffected)
	}

	// Nettoyer les tokens de vérification email expirés
	result, err = db.ExecContext(ctx, `
		DELETE FROM email_verification_tokens 
		WHERE expires_at < NOW()
	`)

	if err != nil {
		utils.SafeWarn("Failed to clean expired email tokens: %v", err)
		return
	}

	rowsAffected, _ = result.RowsAffected()
	if rowsAffected > 0 {
		utils.SafeInfo("Cleaned %d expired email verification tokens", rowsAffected)
	}

	// Nettoyer les tokens de reset password expirés
	result, err = db.ExecContext(ctx, `
		DELETE FROM password_reset_tokens 
		WHERE expires_at < NOW()
	`)

	if err != nil {
		utils.SafeWarn("Failed to clean expired password reset tokens: %v", err)
		return
	}

	rowsAffected, _ = result.RowsAffected()
	if rowsAffected > 0 {
		utils.SafeInfo("Cleaned %d expired password reset tokens", rowsAffected)
	}

	// Nettoyer les invitations expirées (plus de 30 jours)
	result, err = db.ExecContext(ctx, `
		DELETE FROM invitations 
		WHERE status = 'pending' AND created_at < NOW() - INTERVAL '30 days'
	`)

	if err != nil {
		utils.SafeWarn("Failed to clean expired invitations: %v", err)
		return
	}

	rowsAffected, _ = result.RowsAffected()
	if rowsAffected > 0 {
		utils.SafeInfo("Cleaned %d expired invitations", rowsAffected)
	}
}

// scheduleMonthlyRecap déclenche l'envoi du récap mensuel le 1er de chaque
// mois à 09:00 Europe/Paris. Idempotent grâce à email_campaign_sends
// (campaign_id, user_id), on peut donc relancer le scheduler sans dupliquer.
func scheduleMonthlyRecap(db *sql.DB) {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		utils.SafeWarn("monthly-recap: Europe/Paris timezone unavailable, falling back to UTC: %v", err)
		loc = time.UTC
	}

	handler := routes.NewMonthlyRecapHandlerForScheduler(db)

	for {
		nextRun := nextMonthlyRecapTrigger(time.Now().In(loc), loc)
		wait := time.Until(nextRun)
		utils.SafeInfo("monthly-recap: next run scheduled at %s (in %s)", nextRun.Format(time.RFC3339), wait.Truncate(time.Minute))

		timer := time.NewTimer(wait)
		<-timer.C

		now := time.Now().In(loc)
		prev := now.AddDate(0, 0, -1)
		campaignID := fmt.Sprintf("monthly_recap_%04d_%02d", prev.Year(), int(prev.Month()))

		// 4-hour budget for the loop (Resend throttling + decryption).
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
		result := handler.RunMonthlyRecap(ctx, handlers.MonthlyRecapRunOptions{
			CampaignID: campaignID,
			SkipSent:   true,
			Anchor:     now,
		})
		cancel()
		utils.SafeInfo("monthly-recap: campaign=%s total=%d sent=%d skipped=%d failed=%d duration_ms=%d",
			result.CampaignID, result.Total, result.Sent, result.Skipped, result.Failed, result.DurationMs)
	}
}

// nextMonthlyRecapTrigger returns the next 1st-of-month 09:00 in the given
// location, strictly after `from`.
func nextMonthlyRecapTrigger(from time.Time, loc *time.Location) time.Time {
	candidate := time.Date(from.Year(), from.Month(), 1, 9, 0, 0, 0, loc)
	if !candidate.After(from) {
		candidate = time.Date(from.Year(), from.Month()+1, 1, 9, 0, 0, 0, loc)
	}
	return candidate
}

// scheduleRefreshTokenCleanup tourne toutes les 24h et purge les tokens
// expirés depuis plus de 30 jours.
func scheduleRefreshTokenCleanup(s *services.RefreshTokenService) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	// Premier run après 1 min (évite la charge cold-start)
	time.Sleep(1 * time.Minute)
	if n, err := s.CleanupExpired(context.Background()); err == nil && n > 0 {
		utils.SafeInfo("Cleaned %d expired refresh tokens", n)
	}

	for range ticker.C {
		if n, err := s.CleanupExpired(context.Background()); err == nil && n > 0 {
			utils.SafeInfo("Cleaned %d expired refresh tokens", n)
		}
	}
}

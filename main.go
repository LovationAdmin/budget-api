// main.go
// ============================================================================
// BUDGET FAMILLE - API BACKEND
// ============================================================================
// VERSION CORRIGÉE : Logging sécurisé + Amélioration WebSocket
// ============================================================================

package main

import (
	"context"
	"database/sql"
	"os"
	"time"

	"github.com/LovationAdmin/budget-api/config"
	"github.com/LovationAdmin/budget-api/handlers"
	"github.com/LovationAdmin/budget-api/middleware"
	"github.com/LovationAdmin/budget-api/routes"
	"github.com/LovationAdmin/budget-api/utils"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

const (
	AppName    = "Budget Famille API"
	AppVersion = "2.4.0"
)

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

	// Démarrer le nettoyage automatique du cache
	go scheduleCacheCleaning(db)

	// Initialiser le handler WebSocket
	wsHandler := handlers.NewWSHandler()

	// Créer le routeur Gin
	router := gin.Default()

	// ============================================================================
	// CONFIGURATION CORS
	// ============================================================================
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:3000"
	}

	allowedOrigins := []string{
		frontendURL,
		"https://budgetfamille.com",
		"https://www.budgetfamille.com",
		"https://budget-ui-two.vercel.app",
		"http://localhost:3000",
		"http://localhost:5173",
	}

	utils.SafeInfo("CORS configured for %d origins", len(allowedOrigins))

	corsConfig := cors.Config{
		AllowOrigins: allowedOrigins,
		AllowMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders: []string{
			"Origin",
			"Content-Type",
			"Authorization",
			"Upgrade",
			"Connection",
			"Sec-WebSocket-Key",
			"Sec-WebSocket-Version",
			"Sec-WebSocket-Extensions",
		},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           86400,
	}

	// Appliquer CORS en premier
	router.Use(cors.New(corsConfig))

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

	// Rate Limiter
	router.Use(middleware.RateLimiter())

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
		routes.SetupAuthRoutes(v1, db)
		routes.SetupAdminRoutes(v1, db)
		routes.SetupAdminSuggestionsRoutes(v1, db, wsHandler)
		routes.SetupProtectedRoutes(v1, db, wsHandler)
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
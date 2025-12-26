package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	
	"budget-api/config"
	"budget-api/middleware"
	"budget-api/handlers"
	"budget-api/routes"
	"budget-api/services"
)

func main() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found")
	}

	// Initialize database connection
	db, err := config.InitDB()
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	// Run migrations
	if err := config.RunMigrations(db); err != nil {
		log.Fatal("Failed to run migrations:", err)
	}

	// ⭐ NOUVEAU: Démarrer le nettoyage automatique du cache des suggestions
	go scheduleCacheCleaning(db)

	// Initialize WebSocket Handler
	wsHandler := handlers.NewWSHandler()

	// Initialize Gin router
	router := gin.Default()

	// CORS configuration
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:3000" // Fallback pour dev local
	}

	allowedOrigins := []string{
		frontendURL,
		"https://budgetfamille.com",
		"https://www.budgetfamille.com",
		"https://budget-ui-two.vercel.app",
	}

	log.Printf("🌍 CORS: Allowing origins:")
	for _, origin := range allowedOrigins {
		log.Printf("   - %s", origin)
	}

	corsConfig := cors.Config{
		AllowOrigins:     allowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           86400, 
	}
	router.Use(cors.New(corsConfig))

	// Add logging middleware
	router.Use(func(c *gin.Context) {
		log.Printf("📨 %s %s from %s", c.Request.Method, c.Request.URL.Path, c.ClientIP())
		c.Next()
	})

	// Rate limiting middleware
	router.Use(middleware.RateLimiter())

	// API v1 routes
	v1 := router.Group("/api/v1")
	{
		// Public routes
		routes.SetupAuthRoutes(v1, db)
		
		// WebSocket Route (Protected check handled inside handler or via query token)
		v1.GET("/ws/budgets/:id", wsHandler.HandleWS)

		// Admin routes (non protégées pour l'instant, à sécuriser si besoin)
		routes.SetupAdminRoutes(v1, db)
		
		// ⭐ NOUVEAU: Admin routes pour suggestions (nettoyage cache)
		routes.SetupAdminSuggestionsRoutes(v1, db)

		// Protected routes
		protected := v1.Group("/")
		protected.Use(middleware.AuthMiddleware())
		{
			routes.SetupBudgetRoutes(protected, db) 
			routes.SetupUserRoutes(protected, db)
			routes.SetupInvitationRoutes(protected, db)
			routes.SetupEnableBankingRoutes(protected, db)
			
			// ⭐ NOUVEAU: Routes Market Suggestions
			routes.SetupMarketSuggestionsRoutes(protected, db)
		}
	}

	// Health check endpoint
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "ok",
			"service": "budget-api",
			"frontend_url": frontendURL,
		})
	})

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("🚀 Server starting on port %s", port)
	log.Printf("📊 Market Suggestions: Cache cleaning scheduled (every 24h)")
	
	if err := router.Run(":" + port); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}

// ⭐ NOUVELLE FONCTION: Nettoyage automatique du cache des suggestions
// S'exécute tous les jours à minuit pour supprimer les suggestions expirées
func scheduleCacheCleaning(db *sql.DB) {
	// Attendre le démarrage complet
	time.Sleep(5 * time.Second)
	
	log.Println("🧹 Cache cleaning scheduler started")
	
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	// Nettoyer immédiatement au démarrage
	cleanCache(db)

	// Puis nettoyer toutes les 24h
	for range ticker.C {
		cleanCache(db)
	}
}

func cleanCache(db *sql.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	aiService := services.NewClaudeAIService()
	marketAnalyzer := services.NewMarketAnalyzerService(db, aiService)

	if err := marketAnalyzer.CleanExpiredCache(ctx); err != nil {
		log.Printf("❌ Failed to clean suggestions cache: %v", err)
	} else {
		log.Println("✅ Suggestions cache cleaned successfully")
	}
}
package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"time"

	"github.com/LovationAdmin/budget-api/config"
	"github.com/LovationAdmin/budget-api/handlers"
	"github.com/LovationAdmin/budget-api/middleware"
	"github.com/LovationAdmin/budget-api/routes"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	// Initialize database connection
	db, err := config.InitDB()
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	log.Println("✅ Database connected successfully")

	// Run migrations
	if err := config.RunMigrations(db); err != nil {
		log.Fatal("Failed to run migrations:", err)
	}

	log.Println("✅ Database migrations completed")

	// Start automatic cache cleaning (runs daily)
	go scheduleCacheCleaning(db)

	// Initialize WebSocket Handler
	wsHandler := handlers.NewWSHandler()

	// Initialize Gin router
	router := gin.Default()

	// CORS configuration
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:3000"
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
		MaxAge:           86400, // 24 hours
	}
	router.Use(cors.New(corsConfig))

	// Add logging middleware
	router.Use(func(c *gin.Context) {
		start := time.Now()
		log.Printf("📨 %s %s from %s", c.Request.Method, c.Request.URL.Path, c.ClientIP())
		c.Next()
		duration := time.Since(start)
		log.Printf("✅ %s %s - %d (%v)", c.Request.Method, c.Request.URL.Path, c.Writer.Status(), duration)
	})

	// Rate limiting middleware
	router.Use(middleware.RateLimiter())

	// API v1 routes
	v1 := router.Group("/api/v1")
	{
		// Public routes (no authentication required)
		routes.SetupAuthRoutes(v1, db)

		// WebSocket Route (for real-time budget updates)
		v1.GET("/ws/budgets/:id", wsHandler.HandleWS)

		// Admin routes (protected by admin token)
		routes.SetupAdminRoutes(v1, db)
		routes.SetupAdminSuggestionsRoutes(v1, db)

		// Protected routes (require JWT authentication)
		protected := v1.Group("/")
		protected.Use(middleware.AuthMiddleware())
		{
			// MODIFIED: Pass wsHandler here
			routes.SetupBudgetRoutes(protected, db, wsHandler)
			routes.SetupUserRoutes(protected, db)
			routes.SetupInvitationRoutes(protected, db)
			routes.SetupEnableBankingRoutes(protected, db)
			routes.SetupMarketSuggestionsRoutes(protected, db)
		}
	}

	// Health check endpoint (for monitoring)
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "healthy",
			"version": "1.0.0",
			"time":    time.Now().Format(time.RFC3339),
		})
	})

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("🚀 Server starting on port %s...", port)
	log.Printf("📍 Health check: http://localhost:%s/health", port)
	log.Printf("📍 API Base URL: http://localhost:%s/api/v1", port)

	if err := router.Run(":" + port); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}

// scheduleCacheCleaning runs a daily cleanup of expired AI cache entries
func scheduleCacheCleaning(db *sql.DB) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	log.Println("🧹 Cache cleaning scheduler started (runs every 24 hours)")

	// Run immediately on startup
	cleanExpiredCache(db)

	for range ticker.C {
		cleanExpiredCache(db)
	}
}

// cleanExpiredCache removes cache entries older than 30 days
func cleanExpiredCache(db *sql.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := db.ExecContext(ctx, `
		DELETE FROM market_suggestions  
		WHERE created_at < NOW() - INTERVAL '30 days'
	`)

	if err != nil {
		log.Printf("❌ Cache cleanup failed: %v", err)
		return
	}

	rows, err := result.RowsAffected()
	if err != nil {
		log.Printf("❌ Failed to get affected rows: %v", err)
		return
	}

	if rows > 0 {
		log.Printf("🧹 Cleaned %d expired cache entries", rows)
	} else {
		log.Println("✅ No expired cache entries to clean")
	}
}
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
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	db, err := config.InitDB()
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	log.Println("✅ Database connected successfully")

	if err := config.RunMigrations(db); err != nil {
		log.Fatal("Failed to run migrations:", err)
	}

	go scheduleCacheCleaning(db)

	wsHandler := handlers.NewWSHandler()

	router := gin.Default()

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

	log.Printf("🌍 CORS: Allowing origins:")
	for _, origin := range allowedOrigins {
		log.Printf("   - %s", origin)
	}

	// --- FIX: Add WebSocket specific headers ---
	corsConfig := cors.Config{
		AllowOrigins:     allowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		// CRITICAL: Added Upgrade, Connection, and Sec-WebSocket-* headers
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "Upgrade", "Connection", "Sec-WebSocket-Key", "Sec-WebSocket-Version", "Sec-WebSocket-Extensions"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           86400,
	}
	
	// Apply CORS first
	router.Use(cors.New(corsConfig))

	// --- FIX: Define WebSocket route BEFORE logging middleware ---
	// This prevents the logger from hanging/buffering the long-lived WS connection
	router.GET("/api/v1/ws/budgets/:id", wsHandler.HandleWS)

	// Logging Middleware
	router.Use(func(c *gin.Context) {
		// Skip logging for health checks to reduce noise
		if c.Request.URL.Path == "/health" {
			c.Next()
			return
		}
		
		start := time.Now()
		log.Printf("📨 %s %s from %s", c.Request.Method, c.Request.URL.Path, c.ClientIP())
		c.Next()
		duration := time.Since(start)
		log.Printf("✅ %s %s - %d (%v)", c.Request.Method, c.Request.URL.Path, c.Writer.Status(), duration)
	})

	router.Use(middleware.RateLimiter())

	v1 := router.Group("/api/v1")
	{
		routes.SetupAuthRoutes(v1, db)
		routes.SetupAdminRoutes(v1, db)
		routes.SetupAdminSuggestionsRoutes(v1, db)

		protected := v1.Group("/")
		protected.Use(middleware.AuthMiddleware())
		{
			routes.SetupBudgetRoutes(protected, db, wsHandler)
			routes.SetupUserRoutes(protected, db)
			routes.SetupInvitationRoutes(protected, db)
			routes.SetupEnableBankingRoutes(protected, db)
			routes.SetupMarketSuggestionsRoutes(protected, db, wsHandler)
		}
	}

	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "healthy",
			"version": "1.0.0",
			"time":    time.Now().Format(time.RFC3339),
		})
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("🚀 Server starting on port %s...", port)
	if err := router.Run(":" + port); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}

func scheduleCacheCleaning(db *sql.DB) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	cleanExpiredCache(db)
	for range ticker.C {
		cleanExpiredCache(db)
	}
}

func cleanExpiredCache(db *sql.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := db.ExecContext(ctx, `DELETE FROM market_suggestions WHERE created_at < NOW() - INTERVAL '30 days'`)
	if err != nil {
		log.Printf("❌ Cache cleanup failed: %v", err)
		return
	}
	rows, _ := result.RowsAffected()
	if rows > 0 {
		log.Printf("🧹 Cleaned %d expired cache entries", rows)
	}
}
package main

import (
	"log"
	"os"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	
	"budget-api/config"
	"budget-api/middleware"
	"budget-api/handlers" // Assurez-vous que handlers inclut le nouveau ws.go
	"budget-api/routes"
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

		// Protected routes
		protected := v1.Group("/")
		protected.Use(middleware.AuthMiddleware())
		{
			// Note: Vous devrez mettre à jour SetupBudgetRoutes pour passer wsHandler si vous voulez broadcaster des events
			routes.SetupBudgetRoutes(protected, db) 
			routes.SetupUserRoutes(protected, db)
			routes.SetupInvitationRoutes(protected, db)
			routes.SetupBankingRoutes(protected, db)
			routes.SetupAdminRoutes(protected, db)
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
	if err := router.Run(":" + port); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
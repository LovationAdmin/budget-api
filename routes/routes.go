package routes

import (
	"database/sql"
	
	"budget-api/handlers"
	"budget-api/middleware"
	"budget-api/services" // Needed for the specific legacy handler instantiation

	"github.com/gin-gonic/gin"
)

// SetupRoutes combines the new middleware logic with all original routes to prevent regressions.
func SetupRoutes(r *gin.Engine, db *sql.DB) {
	// 1. Setup Services & Handlers
	// We instantiate these here to ensure we cover all logic from the old file
	
	// Auth Handlers
	authHandler := handlers.NewAuthHandler(db) // Using the new constructor style
	
	// User Handler (Restored)
	userHandler := &handlers.UserHandler{DB: db}

	// Budget & Invitation Handlers (New modular style)
	budgetHandler := &handlers.BudgetHandler{DB: db}
	invitationHandler := handlers.NewInvitationHandler(db)

	// Legacy Handler (Restored for specific routes like AcceptInvitation if not moved yet)
	budgetService := services.NewBudgetService(db)
	emailService := services.NewEmailService()
	h := handlers.NewHandler(budgetService, emailService)

	// 2. Define API Groups

	// --- Public Routes (No JWT required) ---
	// We use /api/v1 base, but you can change paths to match your exact frontend needs
	public := r.Group("/api/v1") 
	
	// Auth (Restored original paths to prevent frontend errors)
	public.POST("/auth/signup", authHandler.Register)
	public.POST("/auth/login", authHandler.Login)
	public.GET("/auth/verify", authHandler.VerifyEmail)           // RESTORED
	public.POST("/auth/verify/resend", authHandler.ResendVerification) // RESTORED

	// Invitation Accept (Usually public as it relies on a token in the URL)
	public.POST("/invitations/accept", h.AcceptInvitation) // RESTORED

	// --- Protected Routes (JWT Required) ---
	api := r.Group("/api/v1")
	api.Use(middleware.JWTAuthMiddleware())

	// User Profile & 2FA (COMPLETELY RESTORED)
	api.GET("/user/profile", userHandler.GetProfile)
	api.PUT("/user/profile", userHandler.UpdateProfile)
	api.POST("/user/password", userHandler.ChangePassword)
	api.POST("/user/2fa/setup", userHandler.SetupTOTP)
	api.POST("/user/2fa/verify", userHandler.VerifyTOTP)
	api.POST("/user/2fa/disable", userHandler.DisableTOTP)
	api.DELETE("/user/account", userHandler.DeleteAccount)

	// Budget Routes
	api.GET("/budgets", budgetHandler.GetUserBudgets)
	api.POST("/budgets", budgetHandler.CreateBudget)
	api.GET("/budgets/:id", budgetHandler.GetBudget)
	api.PUT("/budgets/:id", budgetHandler.UpdateBudget)
	api.DELETE("/budgets/:id", budgetHandler.DeleteBudget)
	api.GET("/budgets/:id/data", budgetHandler.GetBudgetData)
	api.PUT("/budgets/:id/data", budgetHandler.SaveBudgetData)

	// Member Management
	api.POST("/budgets/:id/invite", h.InviteMember) // Kept for backward compatibility if needed
	
	// Invitation Routes
	api.POST("/budgets/:id/invitations", invitationHandler.CreateInvitation)
	api.GET("/budgets/:id/invitations", invitationHandler.GetInvitations)
	api.DELETE("/budgets/:id/invitations/:invitation_id", invitationHandler.CancelInvitation)
	api.DELETE("/budgets/:id/members/:member_id", invitationHandler.RemoveMember)

	// Banking Routes
	SetupBankingRoutes(api, db)
	SetupEnableBankingRoutes(api, db)

	// Admin Routes
	SetupAdminRoutes(api, db)
}

// SetupBankingRoutes - Bridge API + General Banking
func SetupBankingRoutes(rg *gin.RouterGroup, db *sql.DB) {
	bankingHandler := handlers.NewBankingHandler(db)
	bridgeHandler := handlers.NewBridgeHandler(db)
	catHandler := handlers.NewCategorizationHandler(db)

	// Banking routes scoped by Budget
	rg.GET("/budgets/:id/banking/connections", bankingHandler.GetConnections)
	rg.POST("/budgets/:id/banking/sync", bridgeHandler.SyncAccounts)
	
	// Global Banking Actions (Bridge)
	rg.POST("/banking/bridge/connect", bridgeHandler.CreateConnection)
	rg.POST("/banking/bridge/refresh", bridgeHandler.RefreshBalances)
	rg.GET("/banking/bridge/transactions", bridgeHandler.GetTransactions)
	rg.GET("/banking/bridge/banks", bridgeHandler.GetBanks)
	
	// Account Specific
	rg.DELETE("/banking/connections/:connection_id", bankingHandler.DeleteConnection)
	rg.PUT("/banking/accounts/:account_id", bankingHandler.UpdateAccountPool)

	// Categorization
	rg.POST("/categorize", catHandler.CategorizeLabel)
}

// SetupEnableBankingRoutes - Enable Banking API (Complete)
func SetupEnableBankingRoutes(rg *gin.RouterGroup, db *sql.DB) {
	handler := handlers.NewEnableBankingHandler(db)

	// 1. List available banks
	rg.GET("/banking/enablebanking/banks", handler.GetBanks)

	// 2. Connect a bank (create auth request)
	rg.POST("/banking/enablebanking/connect", handler.CreateConnection)

	// 3. Callback after authorization
	rg.GET("/banking/enablebanking/callback", handler.HandleCallback)

	// 4. Sync accounts in a budget
	rg.POST("/budgets/:id/banking/enablebanking/sync", handler.SyncAccounts)

	// 5. Get Enable Banking connections
	rg.GET("/budgets/:id/banking/enablebanking/connections", handler.GetConnections)

	// 6. Refresh balances
	rg.POST("/banking/enablebanking/refresh", handler.RefreshBalances)

	// 7. Get transactions
	rg.GET("/banking/enablebanking/transactions", handler.GetTransactions)

	// 8. Delete connection
	rg.DELETE("/banking/enablebanking/connections/:id", handler.DeleteConnection)
}

// SetupAdminRoutes - Admin Routes
func SetupAdminRoutes(rg *gin.RouterGroup, db *sql.DB) {
	adminHandler := &handlers.AdminHandler{DB: db}
	
	// Migration endpoints
	rg.POST("/admin/migrate-budgets", adminHandler.MigrateAllBudgets)
	rg.POST("/admin/migrate-budget/:id", adminHandler.MigrateSingleBudget)
}
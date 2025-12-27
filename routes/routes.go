package routes

import (
	"database/sql"
	"budget-api/handlers"
	"budget-api/services"
	"github.com/gin-gonic/gin"
)

// ============================================================================
// PUBLIC ROUTES - Authentication
// ============================================================================

func SetupAuthRoutes(rg *gin.RouterGroup, db *sql.DB) {
	authHandler := &handlers.AuthHandler{DB: db}
	rg.POST("/auth/signup", authHandler.Signup)
	rg.POST("/auth/login", authHandler.Login)
	rg.GET("/auth/verify", authHandler.VerifyEmail)
	rg.POST("/auth/verify/resend", authHandler.ResendVerification)
}

// ============================================================================
// PROTECTED ROUTES - Budgets
// ============================================================================

func SetupBudgetRoutes(rg *gin.RouterGroup, db *sql.DB) {
	budgetService := services.NewBudgetService(db)
	emailService := services.NewEmailService()
	h := handlers.NewHandler(budgetService, emailService)

	rg.GET("/budgets", h.GetBudgets)
	rg.POST("/budgets", h.CreateBudget)
	rg.GET("/budgets/:id", h.GetBudget)
	rg.PUT("/budgets/:id", h.UpdateBudget)
	rg.DELETE("/budgets/:id", h.DeleteBudget)
	rg.GET("/budgets/:id/data", h.GetBudgetData)
	rg.PUT("/budgets/:id/data", h.UpdateBudgetData)
	rg.POST("/budgets/:id/invite", h.InviteMember)
	rg.DELETE("/budgets/:id/members/:user_id", h.RemoveMember)
	rg.POST("/invitations/accept", h.AcceptInvitation)
}

// ============================================================================
// PROTECTED ROUTES - User Profile & Settings
// ============================================================================

func SetupUserRoutes(rg *gin.RouterGroup, db *sql.DB) {
	userHandler := &handlers.UserHandler{DB: db}
	
	// Profile
	rg.GET("/user/profile", userHandler.GetProfile)
	rg.PUT("/user/profile", userHandler.UpdateProfile)
	
	// ✅ Location (NEW)
	rg.PUT("/user/location", userHandler.UpdateLocation)
	rg.GET("/user/location", userHandler.GetLocation)
	
	// Password
	rg.POST("/user/password", userHandler.ChangePassword)
	
	// 2FA
	rg.POST("/user/2fa/setup", userHandler.SetupTOTP)
	rg.POST("/user/2fa/verify", userHandler.VerifyTOTP)
	rg.POST("/user/2fa/disable", userHandler.DisableTOTP)
	
	// Account deletion
	rg.DELETE("/user/account", userHandler.DeleteAccount)
}

// ============================================================================
// PROTECTED ROUTES - Invitations
// ============================================================================

func SetupInvitationRoutes(rg *gin.RouterGroup, db *sql.DB) {
	invitationHandler := &handlers.InvitationHandler{DB: db}
	rg.GET("/budgets/:id/invitations", invitationHandler.GetInvitations)
	rg.DELETE("/budgets/:id/invitations/:invitation_id", invitationHandler.DeleteInvitation)
}

// ============================================================================
// PROTECTED ROUTES - Enable Banking (Reality Check)
// ============================================================================

func SetupEnableBankingRoutes(rg *gin.RouterGroup, db *sql.DB) {
	ebHandler := handlers.NewEnableBankingHandler(db)
	
	// Auth Flow
	rg.POST("/enable-banking/auth-url", ebHandler.GetAuthURL)
	rg.GET("/enable-banking/callback", ebHandler.HandleCallback)
	rg.POST("/enable-banking/save-connection", ebHandler.SaveConnection)
	
	// Transactions
	rg.GET("/banking/:budget_id/transactions", ebHandler.GetTransactions)
	rg.POST("/banking/:budget_id/map-transactions", ebHandler.MapTransactions)
	rg.GET("/banking/:budget_id/mapped-totals", ebHandler.GetMappedTotals)
	rg.DELETE("/banking/:budget_id/connections/:connection_id", ebHandler.DeleteConnection)
}

// ============================================================================
// PROTECTED ROUTES - Market Suggestions (AI)
// ============================================================================

func SetupMarketSuggestionsRoutes(rg *gin.RouterGroup, db *sql.DB) {
	msHandler := handlers.NewMarketSuggestionsHandler(db)
	
	// Analyze charges and get suggestions
	rg.POST("/budgets/:id/suggestions/bulk-analyze", msHandler.BulkAnalyzeCharges)
	rg.POST("/suggestions/analyze", msHandler.AnalyzeCharge)
	rg.GET("/suggestions/category/:category", msHandler.GetCategorySuggestions)
	
	// AI Categorization
	rg.POST("/categorize", msHandler.CategorizeCharge)
}

// ============================================================================
// ADMIN ROUTES - Suggestions Management
// ============================================================================

func SetupAdminSuggestionsRoutes(rg *gin.RouterGroup, db *sql.DB) {
	adminHandler := handlers.NewAdminSuggestionHandler(db)
	
	// Cache management
	rg.POST("/admin/suggestions/clean-cache", adminHandler.CleanExpiredCache)
	rg.POST("/admin/suggestions/retroactive-analysis", adminHandler.RetroactiveAnalysis)
}

// ============================================================================
// ADMIN ROUTES - General
// ============================================================================

func SetupAdminRoutes(rg *gin.RouterGroup, db *sql.DB) {
	adminHandler := &handlers.AdminHandler{DB: db}
	
	// User management
	rg.GET("/admin/users", adminHandler.GetAllUsers)
	rg.DELETE("/admin/users/:id", adminHandler.DeleteUser)
	
	// Budget stats
	rg.GET("/admin/stats", adminHandler.GetStats)
}
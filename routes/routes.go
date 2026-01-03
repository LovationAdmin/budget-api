package routes

import (
	"database/sql"

	"github.com/gin-gonic/gin"

	"github.com/LovationAdmin/budget-api/handlers"
	"github.com/LovationAdmin/budget-api/services"
)

// SetupAuthRoutes sets up public authentication routes.
func SetupAuthRoutes(rg *gin.RouterGroup, db *sql.DB) {
	authHandler := &handlers.AuthHandler{DB: db}
	
	// Signup & Login
	rg.POST("/auth/signup", authHandler.Signup)
	rg.POST("/auth/login", authHandler.Login)
	
	// Email Verification
	rg.GET("/auth/verify", authHandler.VerifyEmail)
	rg.POST("/auth/verify/resend", authHandler.ResendVerification)
	
	// Password Reset
	rg.POST("/auth/forgot-password", authHandler.ForgotPassword)
	rg.POST("/auth/reset-password", authHandler.ResetPassword)
}

// SetupBudgetRoutes sets up protected budget and related routes.
func SetupBudgetRoutes(rg *gin.RouterGroup, db *sql.DB, wsHandler *handlers.WSHandler) {
	// Créer les services nécessaires
	aiService := services.NewClaudeAIService()
	marketAnalyzer := services.NewMarketAnalyzerService(db, aiService)
	budgetService := services.NewBudgetService(db, wsHandler, marketAnalyzer)
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
	rg.POST("/invitations/accept", h.AcceptInvitation)
}

func SetupUserRoutes(rg *gin.RouterGroup, db *sql.DB) {
	userHandler := &handlers.UserHandler{DB: db}
	
	// Profile
	rg.GET("/user/profile", userHandler.GetProfile)
	rg.PUT("/user/profile", userHandler.UpdateProfile)
	
	// ❌ SUPPRIMÉ : Routes Location (maintenant au niveau budget)
	// rg.PUT("/user/location", userHandler.UpdateLocation)
	// rg.GET("/user/location", userHandler.GetLocation)
	
	// Security
	rg.POST("/user/password", userHandler.ChangePassword)
	rg.POST("/user/2fa/setup", userHandler.SetupTOTP)
	rg.POST("/user/2fa/verify", userHandler.VerifyTOTP)
	rg.POST("/user/2fa/disable", userHandler.DisableTOTP)
	
	// Account Management
	rg.DELETE("/user/account", userHandler.DeleteAccount)
	
	// GDPR Data Export
	rg.GET("/user/export-data", userHandler.ExportUserData)
}

func SetupInvitationRoutes(rg *gin.RouterGroup, db *sql.DB) {
	invitationHandler := &handlers.InvitationHandler{DB: db}
	rg.GET("/budgets/:id/invitations", invitationHandler.GetInvitations)
	rg.DELETE("/budgets/:id/invitations/:invitation_id", invitationHandler.CancelInvitation)
	rg.DELETE("/budgets/:id/members/:member_id", invitationHandler.RemoveMember)
}

func SetupAdminRoutes(rg *gin.RouterGroup, db *sql.DB) {
	adminHandler := &handlers.AdminHandler{DB: db}
	rg.POST("/admin/migrate-budgets", adminHandler.MigrateAllBudgets)
	rg.POST("/admin/migrate-budget/:id", adminHandler.MigrateSingleBudget)
}

func SetupEnableBankingRoutes(rg *gin.RouterGroup, db *sql.DB) {
	handler := handlers.NewEnableBankingHandler(db)
	rg.GET("/banking/enablebanking/banks", handler.GetBanks)
	rg.POST("/banking/enablebanking/connect", handler.CreateConnection)
	rg.GET("/banking/enablebanking/callback", handler.HandleCallback)
	rg.GET("/budgets/:id/banking/enablebanking/connections", handler.GetConnections)
	rg.POST("/budgets/:id/banking/enablebanking/sync", handler.SyncAccounts)
	
	rg.POST("/banking/enablebanking/refresh", handler.RefreshBalances)
	rg.GET("/banking/enablebanking/transactions", handler.GetTransactions)
	rg.DELETE("/banking/enablebanking/connections/:id", handler.DeleteConnection)
	rg.GET("/banking/budgets/:id/reality-check", handler.GetConnections)
}

func SetupMarketSuggestionsRoutes(rg *gin.RouterGroup, db *sql.DB, wsHandler *handlers.WSHandler) {
	handler := handlers.NewMarketSuggestionsHandler(db, wsHandler)

	rg.POST("/suggestions/analyze", handler.AnalyzeCharge)
	rg.GET("/suggestions/category/:category", handler.GetCategorySuggestions)
	rg.POST("/budgets/:id/suggestions/bulk-analyze", handler.BulkAnalyzeCharges)
	rg.POST("/categorize", handler.CategorizeCharge)
}

func SetupAdminSuggestionsRoutes(rg *gin.RouterGroup, db *sql.DB) {
	handler := handlers.NewMarketSuggestionsHandler(db, nil)
	rg.POST("/admin/suggestions/clean-cache", handler.CleanExpiredCache)
	
	adminHandler := handlers.NewAdminSuggestionHandler(db)
	rg.POST("/admin/suggestions/retroactive-analyze", adminHandler.RetroactiveAnalysis)
}
package routes

import (
	"database/sql"
	"budget-api/handlers"
	"budget-api/services"
	"github.com/gin-gonic/gin"
)

// SetupAuthRoutes sets up public authentication routes.
func SetupAuthRoutes(rg *gin.RouterGroup, db *sql.DB) {
	authHandler := &handlers.AuthHandler{DB: db}
	rg.POST("/auth/signup", authHandler.Signup)
	rg.POST("/auth/login", authHandler.Login)
	rg.GET("/auth/verify", authHandler.VerifyEmail)
	rg.POST("/auth/verify/resend", authHandler.ResendVerification)
}

// SetupBudgetRoutes sets up protected budget and related routes.
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
	rg.POST("/invitations/accept", h.AcceptInvitation)
}

func SetupUserRoutes(rg *gin.RouterGroup, db *sql.DB) {
	userHandler := &handlers.UserHandler{DB: db}
	rg.GET("/user/profile", userHandler.GetProfile)
	rg.PUT("/user/profile", userHandler.UpdateProfile)
	rg.POST("/user/password", userHandler.ChangePassword)
	rg.POST("/user/2fa/setup", userHandler.SetupTOTP)
	rg.POST("/user/2fa/verify", userHandler.VerifyTOTP)
	rg.POST("/user/2fa/disable", userHandler.DisableTOTP)
	rg.DELETE("/user/account", userHandler.DeleteAccount)
}

func SetupInvitationRoutes(rg *gin.RouterGroup, db *sql.DB) {
	invitationHandler := &handlers.InvitationHandler{DB: db}
	rg.GET("/budgets/:id/invitations", invitationHandler.GetInvitations)
	rg.DELETE("/budgets/:id/invitations/:invitation_id", invitationHandler.CancelInvitation)
	rg.DELETE("/budgets/:id/members/:member_id", invitationHandler.RemoveMember)
}

// SetupBankingRoutes reorganized for Budget Isolation
func SetupBankingRoutes(rg *gin.RouterGroup, db *sql.DB) {
	bankingHandler := handlers.NewBankingHandler(db)
	bridgeHandler := handlers.NewBridgeHandler(db)
	catHandler := handlers.NewCategorizationHandler(db)

	// Banking routes Scoped by Budget
	// GET /api/v1/budgets/:id/banking/connections
	rg.GET("/budgets/:id/banking/connections", bankingHandler.GetConnections)
	rg.POST("/budgets/:id/banking/sync", bridgeHandler.SyncAccounts)
	
	// Global Banking Actions (Bridge specific, not strictly budget-scoped but used in context)
	rg.POST("/banking/bridge/connect", bridgeHandler.CreateConnection)
	rg.POST("/banking/bridge/refresh", bridgeHandler.RefreshBalances)
	rg.GET("/banking/bridge/transactions", bridgeHandler.GetTransactions)
	
	// Account Specific
	rg.DELETE("/banking/connections/:connection_id", bankingHandler.DeleteConnection)
	rg.PUT("/banking/accounts/:account_id", bankingHandler.UpdateAccountPool)

	rg.POST("/categorize", catHandler.CategorizeLabel)
    rg.GET("/banking/bridge/banks", bridgeHandler.GetBanks)
}

func SetupAdminRoutes(rg *gin.RouterGroup, db *sql.DB) {
	adminHandler := &handlers.AdminHandler{DB: db}
	
	// Migration endpoints
	rg.POST("/admin/migrate-budgets", adminHandler.MigrateAllBudgets)
	rg.POST("/admin/migrate-budget/:id", adminHandler.MigrateSingleBudget)
}
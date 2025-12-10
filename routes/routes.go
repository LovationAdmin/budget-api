package routes

import (
	"database/sql"

	"budget-api/handlers"
	"budget-api/services"

	"github.com/gin-gonic/gin"
)

// SetupAuthRoutes sets up public authentication routes.
func SetupAuthRoutes(rg *gin.RouterGroup, db *sql.DB) {
	// Use the dedicated AuthHandler
	authHandler := &handlers.AuthHandler{DB: db}

	rg.POST("/auth/signup", authHandler.Signup)
	rg.POST("/auth/login", authHandler.Login)
	
	// NEW: Route pour la vérification d'email
	rg.GET("/auth/verify", authHandler.VerifyEmail)
}

// SetupBudgetRoutes sets up protected budget and related routes.
func SetupBudgetRoutes(rg *gin.RouterGroup, db *sql.DB) {
	// Initialize required services
	budgetService := services.NewBudgetService(db)
	emailService := services.NewEmailService()
	
	h := handlers.NewHandler(budgetService, emailService)

	// Budget routes
	rg.GET("/budgets", h.GetBudgets)
	rg.POST("/budgets", h.CreateBudget)
	rg.GET("/budgets/:id", h.GetBudget)
	rg.PUT("/budgets/:id", h.UpdateBudget)
	rg.DELETE("/budgets/:id", h.DeleteBudget)

	// Budget data routes
	rg.GET("/budgets/:id/data", h.GetBudgetData)
	rg.PUT("/budgets/:id/data", h.UpdateBudgetData)

	// Invitation routes handled by the Budget Handler (InviteMember, AcceptInvitation)
	rg.POST("/budgets/:id/invite", h.InviteMember)
	rg.POST("/invitations/accept", h.AcceptInvitation)
}

// SetupUserRoutes sets up protected user routes.
func SetupUserRoutes(rg *gin.RouterGroup, db *sql.DB) {
	// Use the dedicated UserHandler
	userHandler := &handlers.UserHandler{DB: db}
	
	rg.GET("/user/profile", userHandler.GetProfile)
	rg.PUT("/user/profile", userHandler.UpdateProfile)
	rg.POST("/user/password", userHandler.ChangePassword) 
	rg.POST("/user/2fa/setup", userHandler.SetupTOTP)
	rg.POST("/user/2fa/verify", userHandler.VerifyTOTP)
	rg.POST("/user/2fa/disable", userHandler.DisableTOTP)
	rg.DELETE("/user/account", userHandler.DeleteAccount)
}

// SetupInvitationRoutes sets up the remaining invitation/member management routes.
func SetupInvitationRoutes(rg *gin.RouterGroup, db *sql.DB) {
	// Use the dedicated InvitationHandler
	invitationHandler := &handlers.InvitationHandler{DB: db}
	
	rg.GET("/budgets/:id/invitations", invitationHandler.GetInvitations)
	rg.DELETE("/budgets/:id/invitations/:invitation_id", invitationHandler.CancelInvitation)
	rg.DELETE("/budgets/:id/members/:member_id", invitationHandler.RemoveMember)
}
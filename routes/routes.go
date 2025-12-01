package routes

import (
	"database/sql"

	"github.com/gin-gonic/gin"
	"budget-api/handlers"
)

func SetupAuthRoutes(router *gin.RouterGroup, db *sql.DB) {
	authHandler := &handlers.AuthHandler{DB: db}

	auth := router.Group("/auth")
	{
		auth.POST("/signup", authHandler.Signup)
		auth.POST("/login", authHandler.Login)
	}
}

func SetupUserRoutes(router *gin.RouterGroup, db *sql.DB) {
	userHandler := &handlers.UserHandler{DB: db}

	users := router.Group("/user")
	{
		users.GET("/profile", userHandler.GetProfile)
		users.PUT("/profile", userHandler.UpdateProfile)
		users.PUT("/password", userHandler.ChangePassword)
		users.DELETE("/account", userHandler.DeleteAccount)

		users.POST("/2fa/setup", userHandler.SetupTOTP)
		users.POST("/2fa/verify", userHandler.VerifyTOTP)
		users.POST("/2fa/disable", userHandler.DisableTOTP)
	}
}

func SetupBudgetRoutes(router *gin.RouterGroup, db *sql.DB) {
	budgetHandler := &handlers.BudgetHandler{DB: db}

	budgets := router.Group("/budgets")
	{
		budgets.POST("", budgetHandler.CreateBudget)
		budgets.GET("", budgetHandler.GetBudgets)
		budgets.GET("/:id", budgetHandler.GetBudget)
		budgets.PUT("/:id", budgetHandler.UpdateBudget)
		budgets.DELETE("/:id", budgetHandler.DeleteBudget)

		budgets.GET("/:id/data", budgetHandler.GetBudgetData)
		budgets.PUT("/:id/data", budgetHandler.UpdateBudgetData)
	}
}

func SetupInvitationRoutes(router *gin.RouterGroup, db *sql.DB) {
	invitationHandler := &handlers.InvitationHandler{DB: db}

	router.POST("/budgets/:id/invite", invitationHandler.InviteUser)
	router.GET("/budgets/:id/invitations", invitationHandler.GetInvitations)
	router.DELETE("/budgets/:id/invitations/:invitation_id", invitationHandler.CancelInvitation)
	router.DELETE("/budgets/:id/members/:member_id", invitationHandler.RemoveMember)

	router.POST("/invitations/accept", invitationHandler.AcceptInvitation)
}
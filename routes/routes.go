package routes

import (
	"budget-api/handlers"
	"budget-api/middleware"

	"github.com/gin-gonic/gin"
)

func SetupRoutes(r *gin.Engine, h *handlers.Handler, invH *handlers.InvitationHandler, authMiddleware gin.HandlerFunc) {
	api := r.Group("/api/v1")

	// Auth routes (public)
	api.POST("/auth/signup", h.Signup)
	api.POST("/auth/login", h.Login)

	// Protected routes
	protected := api.Group("")
	protected.Use(authMiddleware)
	{
		// Budget routes
		protected.GET("/budgets", h.GetBudgets)
		protected.POST("/budgets", h.CreateBudget)
		protected.GET("/budgets/:id", h.GetBudget)
		protected.PUT("/budgets/:id", h.UpdateBudget)
		protected.DELETE("/budgets/:id", h.DeleteBudget)

		// Budget data routes
		protected.GET("/budgets/:id/data", h.GetBudgetData)
		protected.PUT("/budgets/:id/data", h.UpdateBudgetData)

		// Invitation routes (using BudgetHandler)
		protected.POST("/budgets/:id/invite", h.InviteMember)
		protected.POST("/invitations/accept", h.AcceptInvitation)

		// Invitation routes (using InvitationHandler)
		protected.GET("/budgets/:id/invitations", invH.GetInvitations)
		protected.DELETE("/budgets/:id/invitations/:invitation_id", invH.CancelInvitation)
		protected.DELETE("/budgets/:id/members/:member_id", invH.RemoveMember)
	}
}
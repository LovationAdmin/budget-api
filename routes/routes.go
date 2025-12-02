package routes

import (
	"budget-api/handlers"

	"github.com/gin-gonic/gin"
)

func SetupRoutes(r *gin.Engine, h *handlers.Handler, authMiddleware gin.HandlerFunc) {
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
		protected.DELETE("/budgets/:id", h.DeleteBudget) // Vérifier que cette route existe

		// Budget data routes
		protected.GET("/budgets/:id/data", h.GetBudgetData)
		protected.PUT("/budgets/:id/data", h.UpdateBudgetData)

		// Invitation routes
		protected.POST("/budgets/:id/invite", h.InviteMember)
		protected.POST("/invitations/accept", h.AcceptInvitation)
	}
}
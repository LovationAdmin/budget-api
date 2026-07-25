// routes/routes.go
package routes

import (
	"database/sql"

	"github.com/gin-gonic/gin"

	"github.com/LovationAdmin/budget-api/handlers"
	"github.com/LovationAdmin/budget-api/middleware"
	"github.com/LovationAdmin/budget-api/services"
)

// SetupAuthRoutes sets up public authentication routes.
func SetupAuthRoutes(rg *gin.RouterGroup, db *sql.DB, rt *services.RefreshTokenService) {
	authHandler := handlers.NewAuthHandlerWithRefresh(db, rt)

	// Signup & Login (avec rate limits ciblés)
	rg.POST("/auth/signup", middleware.SignupRateLimit(), authHandler.Signup)
	rg.POST("/auth/login", middleware.LoginRateLimit(), authHandler.Login)

	// Refresh & Logout (cookie-based, pas d'auth Bearer requise)
	rg.POST("/auth/refresh", middleware.RefreshRateLimit(), authHandler.Refresh)
	rg.POST("/auth/logout", authHandler.Logout)

	// Email Verification
	rg.GET("/auth/verify-email", authHandler.VerifyEmail)
	rg.POST("/auth/verify/resend", middleware.VerifyResendRateLimit(), authHandler.ResendVerificationEmail)

	// Password Reset
	rg.POST("/auth/forgot-password", middleware.ForgotPasswordRateLimit(), authHandler.ForgotPassword)
	rg.POST("/auth/reset-password", middleware.ResetPasswordRateLimit(), authHandler.ResetPassword)
}

// SetupBudgetRoutes sets up protected budget and related routes.
func SetupBudgetRoutes(rg *gin.RouterGroup, db *sql.DB, wsHandler *handlers.WSHandler) {
	// Créer les services nécessaires
	aiService := services.NewClaudeAIService()
	marketAnalyzer := services.NewMarketAnalyzerService(db, aiService)

	// wsHandler implements Broadcaster, so this works fine
	budgetService := services.NewBudgetService(db, wsHandler, marketAnalyzer)
	emailService := services.NewEmailService()
	h := handlers.NewHandler(budgetService, emailService)

	// AI budget advisor (« Budget proposé par IA »)
	advisorService := services.NewBudgetAdvisorService(aiService)
	advisorHandler := handlers.NewBudgetAdvisorHandler(advisorService)

	rg.GET("/budgets", h.GetBudgets)
	rg.POST("/budgets", h.CreateBudget)
	rg.GET("/budgets/:id", h.GetBudget)
	rg.PUT("/budgets/:id", h.UpdateBudget)
	rg.DELETE("/budgets/:id", h.DeleteBudget)
	rg.GET("/budgets/:id/data", h.GetBudgetData)
	rg.PUT("/budgets/:id/data", h.UpdateBudgetData)
	rg.POST("/budgets/:id/invite", h.InviteMember)
	rg.POST("/invitations/accept", h.AcceptInvitation)

	// Stateless generation, usable both at creation and on an existing budget.
	rg.POST("/budgets/ai-proposal", advisorHandler.GenerateProposal)
}

func SetupUserRoutes(rg *gin.RouterGroup, db *sql.DB, rt *services.RefreshTokenService) {
	userHandler := &handlers.UserHandler{DB: db, RefreshTokens: rt}
	authHandler := handlers.NewAuthHandlerWithRefresh(db, rt)

	// Logout multi-device : révoque tous les refresh tokens du user
	rg.POST("/auth/logout-all", authHandler.LogoutAll)

	// Profile
	rg.GET("/user/profile", userHandler.GetProfile)
	rg.PUT("/user/profile", userHandler.UpdateProfile)

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

	// Stats endpoint (X-Admin-Secret header required)
	statsHandler := handlers.NewAdminStatsHandler(db)
	rg.GET("/admin/stats", statsHandler.GetStats)

	// ✅ NEW: Re-engagement campaigns
	campaignsHandler := handlers.NewAdminCampaignsHandler(db, services.NewEmailService())
	rg.POST("/admin/campaigns/send", campaignsHandler.SendReengagementCampaign)

	// ✅ NEW: Monthly recap — same admin-secret protection as the rest
	recapHandler := handlers.NewAdminMonthlyRecapHandler(
		db,
		services.NewEmailService(),
		newMonthlyRecapService(db),
	)
	rg.POST("/admin/campaigns/monthly-recap", recapHandler.SendMonthlyRecap)

	// One-shot maintenance: backfill the lockedMonths map in every budget so
	// dormant accounts converge to the auto-lock state introduced by the
	// 2026-05-15 persistence fix. BudgetService is wired with nil ws to keep
	// the backfill quiet (no per-row WebSocket broadcast).
	locksBackfillHandler := handlers.NewAdminLocksBackfillHandler(
		db,
		services.NewBudgetService(db, nil, nil),
	)
	rg.POST("/admin/maintenance/backfill-locks", locksBackfillHandler.BackfillLocks)
}

// newMonthlyRecapService wires the recap service against a BudgetService.
// The BudgetService here is created with nil ws/marketAnalyzer because the
// recap path only ever reads — we never broadcast or trigger cache work from
// it.
func newMonthlyRecapService(db *sql.DB) *services.MonthlyRecapService {
	budgetService := services.NewBudgetService(db, nil, nil)
	return services.NewMonthlyRecapService(db, budgetService)
}

// NewMonthlyRecapHandlerForScheduler exposes the recap handler to main.go so
// the scheduled job can drive it without a HTTP round-trip.
func NewMonthlyRecapHandlerForScheduler(db *sql.DB) *handlers.AdminMonthlyRecapHandler {
	return handlers.NewAdminMonthlyRecapHandler(
		db,
		services.NewEmailService(),
		newMonthlyRecapService(db),
	)
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
	// Initialize services locally
	aiService := services.NewClaudeAIService()
	marketAnalyzer := services.NewMarketAnalyzerService(db, aiService)

	// FIXED: Pass all 3 required arguments (DB, Analyzer, WS)
	handler := handlers.NewMarketSuggestionsHandler(db, marketAnalyzer, wsHandler)

	rg.POST("/suggestions/analyze", handler.AnalyzeCharge)
	rg.GET("/suggestions/category/:category", handler.GetCategorySuggestions)
	rg.POST("/budgets/:id/suggestions/bulk-analyze", handler.BulkAnalyzeCharges)

	// FIXED: Updated method name to match handler definition
	rg.POST("/categorize", handler.CategorizeLabel)
}

func SetupAdminSuggestionsRoutes(rg *gin.RouterGroup, db *sql.DB) {
	// Initialize services locally
	aiService := services.NewClaudeAIService()
	marketAnalyzer := services.NewMarketAnalyzerService(db, aiService)

	// FIXED: Pass required arguments. WS is nil for admin routes.
	handler := handlers.NewMarketSuggestionsHandler(db, marketAnalyzer, nil)

	rg.POST("/admin/suggestions/clean-cache", handler.CleanExpiredCache)

	adminHandler := handlers.NewAdminSuggestionHandler(db)
	rg.POST("/admin/suggestions/retroactive-analyze", adminHandler.RetroactiveAnalysis)
}

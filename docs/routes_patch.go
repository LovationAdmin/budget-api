// docs/routes_patch.go
// ============================================================================
// PATCH for routes/routes.go — add ONE line in SetupAdminRoutes
// ============================================================================
// This file is illustrative only. Do not import it.
// Apply the change manually to your existing routes/routes.go file.
// ============================================================================

/*

In routes/routes.go, find SetupAdminRoutes and add the campaign route:

func SetupAdminRoutes(rg *gin.RouterGroup, db *sql.DB) {
	adminHandler := &handlers.AdminHandler{DB: db}
	rg.POST("/admin/migrate-budgets", adminHandler.MigrateAllBudgets)
	rg.POST("/admin/migrate-budget/:id", adminHandler.MigrateSingleBudget)

	// Stats endpoint (X-Admin-Secret header required)
	statsHandler := handlers.NewAdminStatsHandler(db)
	rg.GET("/admin/stats", statsHandler.GetStats)

	// ✅ NEW: Re-engagement campaign endpoint (X-Admin-Secret header required)
	campaignsHandler := handlers.NewAdminCampaignsHandler(db, services.NewEmailService())
	rg.POST("/admin/campaigns/send", campaignsHandler.SendReengagementCampaign)
}

That's it. The route lives at POST /api/v1/admin/campaigns/send and is
protected by the same X-Admin-Secret header that guards /admin/stats.

*/

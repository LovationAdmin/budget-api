package handlers

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"budget-api/middleware"
	"budget-api/models"
)

type BudgetHandler struct {
	DB *sql.DB
}

// CreateBudget creates a new budget
func (h *BudgetHandler) CreateBudget(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req models.CreateBudgetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Create budget
	var budgetID string
	err := h.DB.QueryRow(`
		INSERT INTO budgets (name, owner_id)
		VALUES ($1, $2)
		RETURNING id
	`, req.Name, userID).Scan(&budgetID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create budget"})
		return
	}

	// Add owner as member
	_, err = h.DB.Exec(`
		INSERT INTO budget_members (budget_id, user_id, role, permissions)
		VALUES ($1, $2, 'owner', '{"read": true, "write": true}')
	`, budgetID, userID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add owner as member"})
		return
	}

	// Initialize budget data with empty structure
	initialData := `{
		"budgetTitle": "",
		"people": [],
		"charges": [],
		"projects": [],
		"yearlyData": {},
		"oneTimeIncomes": {},
		"currentYear": 2026,
		"viewMode": "monthly"
	}`

	_, err = h.DB.Exec(`
		INSERT INTO budget_data (budget_id, data, updated_by)
		VALUES ($1, $2, $3)
	`, budgetID, initialData, userID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initialize budget data"})
		return
	}

	budget := models.Budget{
		ID:        budgetID,
		Name:      req.Name,
		OwnerID:   userID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	c.JSON(http.StatusCreated, budget)
}

// GetBudgets returns all budgets accessible by the user
func (h *BudgetHandler) GetBudgets(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	rows, err := h.DB.Query(`
		SELECT b.id, b.name, b.owner_id, b.created_at, b.updated_at, bm.role
		FROM budgets b
		INNER JOIN budget_members bm ON b.id = bm.budget_id
		WHERE bm.user_id = $1
		ORDER BY b.updated_at DESC
	`, userID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch budgets"})
		return
	}
	defer rows.Close()

	budgets := []map[string]interface{}{}
	for rows.Next() {
		var budget models.Budget
		var role string
		if err := rows.Scan(&budget.ID, &budget.Name, &budget.OwnerID, &budget.CreatedAt, &budget.UpdatedAt, &role); err != nil {
			continue
		}
		
		budgetMap := map[string]interface{}{
			"id":         budget.ID,
			"name":       budget.Name,
			"owner_id":   budget.OwnerID,
			"created_at": budget.CreatedAt,
			"updated_at": budget.UpdatedAt,
			"role":       role,
			"is_owner":   budget.OwnerID == userID,
		}
		budgets = append(budgets, budgetMap)
	}

	c.JSON(http.StatusOK, budgets)
}

// GetBudget returns a single budget by ID
func (h *BudgetHandler) GetBudget(c *gin.Context) {
	userID := middleware.GetUserID(c)
	budgetID := c.Param("id")

	// Check if user has access to this budget
	var exists bool
	err := h.DB.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM budget_members
			WHERE budget_id = $1 AND user_id = $2
		)
	`, budgetID, userID).Scan(&exists)

	if err != nil || !exists {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	// Get budget details
	var budget models.Budget
	err = h.DB.QueryRow(`
		SELECT id, name, owner_id, created_at, updated_at
		FROM budgets
		WHERE id = $1
	`, budgetID).Scan(&budget.ID, &budget.Name, &budget.OwnerID, &budget.CreatedAt, &budget.UpdatedAt)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "Budget not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch budget"})
		return
	}

	// Get members
	members := []models.BudgetMember{}
	rows, err := h.DB.Query(`
		SELECT bm.id, bm.budget_id, bm.user_id, bm.role, bm.permissions, bm.joined_at,
		       u.email, u.name
		FROM budget_members bm
		INNER JOIN users u ON bm.user_id = u.id
		WHERE bm.budget_id = $1
	`, budgetID)

	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var member models.BudgetMember
			var user models.User
			rows.Scan(&member.ID, &member.BudgetID, &member.UserID, &member.Role,
				&member.Permissions, &member.JoinedAt, &user.Email, &user.Name)
			user.ID = member.UserID
			member.User = &user
			members = append(members, member)
		}
	}

	response := map[string]interface{}{
		"budget":  budget,
		"members": members,
		"is_owner": budget.OwnerID == userID,
	}

	c.JSON(http.StatusOK, response)
}

// GetBudgetData returns the data of a budget
func (h *BudgetHandler) GetBudgetData(c *gin.Context) {
	userID := middleware.GetUserID(c)
	budgetID := c.Param("id")

	// Check access
	var exists bool
	err := h.DB.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM budget_members
			WHERE budget_id = $1 AND user_id = $2
		)
	`, budgetID, userID).Scan(&exists)

	if err != nil || !exists {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	// Get budget data
	var data models.BudgetData
	err = h.DB.QueryRow(`
		SELECT id, budget_id, data, version, updated_by, updated_at
		FROM budget_data
		WHERE budget_id = $1
		ORDER BY updated_at DESC
		LIMIT 1
	`, budgetID).Scan(&data.ID, &data.BudgetID, &data.Data, &data.Version, &data.UpdatedBy, &data.UpdatedAt)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "Budget data not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch budget data"})
		return
	}

	c.JSON(http.StatusOK, data)
}

// UpdateBudgetData updates the data of a budget
func (h *BudgetHandler) UpdateBudgetData(c *gin.Context) {
	userID := middleware.GetUserID(c)
	budgetID := c.Param("id")

	// Check write permission
	var canWrite bool
	err := h.DB.QueryRow(`
		SELECT (permissions->>'write')::boolean
		FROM budget_members
		WHERE budget_id = $1 AND user_id = $2
	`, budgetID, userID).Scan(&canWrite)

	if err != nil || !canWrite {
		c.JSON(http.StatusForbidden, gin.H{"error": "Write access denied"})
		return
	}

	var req models.UpdateBudgetDataRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get current version
	var currentVersion int
	err = h.DB.QueryRow(`
		SELECT COALESCE(MAX(version), 0)
		FROM budget_data
		WHERE budget_id = $1
	`, budgetID).Scan(&currentVersion)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get version"})
		return
	}

	// Insert new version
	newVersion := currentVersion + 1
	var dataID string
	err = h.DB.QueryRow(`
		INSERT INTO budget_data (budget_id, data, version, updated_by)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, budgetID, req.Data, newVersion, userID).Scan(&dataID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update budget data"})
		return
	}

	// Log in audit
	_, _ = h.DB.Exec(`
		INSERT INTO audit_logs (budget_id, user_id, action, changes)
		VALUES ($1, $2, 'update_data', $3)
	`, budgetID, userID, req.Data)

	c.JSON(http.StatusOK, gin.H{
		"id":      dataID,
		"version": newVersion,
		"message": "Budget data updated successfully",
	})
}

// UpdateBudget updates budget metadata (name, etc.)
func (h *BudgetHandler) UpdateBudget(c *gin.Context) {
	userID := middleware.GetUserID(c)
	budgetID := c.Param("id")

	// Check if user is owner
	var isOwner bool
	err := h.DB.QueryRow(`
		SELECT owner_id = $1
		FROM budgets
		WHERE id = $2
	`, userID, budgetID).Scan(&isOwner)

	if err != nil || !isOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": "Only owner can update budget metadata"})
		return
	}

	var req models.CreateBudgetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, err = h.DB.Exec(`
		UPDATE budgets
		SET name = $1, updated_at = NOW()
		WHERE id = $2
	`, req.Name, budgetID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update budget"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Budget updated successfully"})
}

// DeleteBudget deletes a budget (owner only)
func (h *BudgetHandler) DeleteBudget(c *gin.Context) {
	userID := middleware.GetUserID(c)
	budgetID := c.Param("id")

	// Check if user is owner
	var isOwner bool
	err := h.DB.QueryRow(`
		SELECT owner_id = $1
		FROM budgets
		WHERE id = $2
	`, userID, budgetID).Scan(&isOwner)

	if err != nil || !isOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": "Only owner can delete budget"})
		return
	}

	// Delete budget (cascade will delete members, data, etc.)
	_, err = h.DB.Exec(`DELETE FROM budgets WHERE id = $1`, budgetID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete budget"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Budget deleted successfully"})
}
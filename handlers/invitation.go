package handlers

import (
	"database/sql"
	"net/http"
	"time"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"budget-api/middleware"
	"budget-api/models"
	"budget-api/utils"
)

type InvitationHandler struct {
	DB *sql.DB
}

// InviteUser sends an invitation to join a budget
func (h *InvitationHandler) InviteUser(c *gin.Context) {
	userID := middleware.GetUserID(c)
	budgetID := c.Param("id")

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	// Check if user is a member of the budget
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

	var req models.InvitationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check if user is already a member
	var alreadyMember bool
	err = h.DB.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM budget_members bm
			INNER JOIN users u ON bm.user_id = u.id
			WHERE bm.budget_id = $1 AND u.email = $2
		)
	`, budgetID, req.Email).Scan(&alreadyMember)

	if err == nil && alreadyMember {
		c.JSON(http.StatusConflict, gin.H{"error": "User is already a member"})
		return
	}

	// Check if there's a pending invitation
	var pendingInvitation bool
	err = h.DB.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM invitations
			WHERE budget_id = $1 AND email = $2 AND status = 'pending' AND expires_at > NOW()
		)
	`, budgetID, req.Email).Scan(&pendingInvitation)

	if err == nil && pendingInvitation {
		c.JSON(http.StatusConflict, gin.H{"error": "Invitation already sent"})
		return
	}

	// Generate invitation token
	token := uuid.New().String()

	// Create invitation
	var invitationID string
	expiresAt := time.Now().Add(7 * 24 * time.Hour) // 7 days
	err = h.DB.QueryRow(`
		INSERT INTO invitations (budget_id, email, invited_by, token, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, budgetID, req.Email, userID, token, expiresAt).Scan(&invitationID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create invitation"})
		return
	}

	// Get budget and inviter info for email
	var budgetName, inviterName string
	err = h.DB.QueryRow(`
		SELECT b.name, u.name
		FROM budgets b, users u
		WHERE b.id = $1 AND u.id = $2
	`, budgetID, userID).Scan(&budgetName, &inviterName)

	if err != nil {
		inviterName = "A user"
		budgetName = "a budget"
	}

	// Send invitation email
	err = utils.SendInvitationEmail(req.Email, inviterName, budgetName, token)
	if err != nil {
		// Log error but don't fail the request
		c.JSON(http.StatusCreated, gin.H{
			"id":      invitationID,
			"token":   token,
			"message": "Invitation created but email failed to send",
			"warning": "Please share the invitation link manually",
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":      invitationID,
		"message": "Invitation sent successfully",
	})
}

// GetInvitations returns all invitations for a budget
func (h *InvitationHandler) GetInvitations(c *gin.Context) {
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

	// Get invitations
	rows, err := h.DB.Query(`
		SELECT i.id, i.budget_id, i.email, i.invited_by, i.token, i.status, i.expires_at, i.created_at,
		       u.name as inviter_name
		FROM invitations i
		LEFT JOIN users u ON i.invited_by = u.id
		WHERE i.budget_id = $1
		ORDER BY i.created_at DESC
	`, budgetID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch invitations"})
		return
	}
	defer rows.Close()

	invitations := []map[string]interface{}{}
	for rows.Next() {
		var inv models.Invitation
		var inviterName sql.NullString
		err := rows.Scan(&inv.ID, &inv.BudgetID, &inv.Email, &inv.InvitedBy, &inv.Token,
			&inv.Status, &inv.ExpiresAt, &inv.CreatedAt, &inviterName)
		if err != nil {
			continue
		}

		invMap := map[string]interface{}{
			"id":         inv.ID,
			"email":      inv.Email,
			"status":     inv.Status,
			"expires_at": inv.ExpiresAt,
			"created_at": inv.CreatedAt,
		}

		if inviterName.Valid {
			invMap["inviter_name"] = inviterName.String
		}

		invitations = append(invitations, invMap)
	}

	c.JSON(http.StatusOK, invitations)
}

// AcceptInvitation accepts an invitation and adds user to budget
func (h *InvitationHandler) AcceptInvitation(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req models.AcceptInvitationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get invitation
	var inv models.Invitation
	var userEmail string
	err := h.DB.QueryRow(`
		SELECT i.id, i.budget_id, i.email, i.status, i.expires_at,
		       u.email as user_email
		FROM invitations i, users u
		WHERE i.token = $1 AND u.id = $2
	`, req.Token, userID).Scan(&inv.ID, &inv.BudgetID, &inv.Email, &inv.Status, &inv.ExpiresAt, &userEmail)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invitation not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch invitation"})
		return
	}

	// Check if invitation is valid
	if inv.Status != "pending" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invitation already " + inv.Status})
		return
	}

	if time.Now().After(inv.ExpiresAt) {
		// Mark as expired
		h.DB.Exec(`UPDATE invitations SET status = 'expired' WHERE id = $1`, inv.ID)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invitation has expired"})
		return
	}

	// Verify email matches
	if userEmail != inv.Email {
		c.JSON(http.StatusForbidden, gin.H{"error": "This invitation is for a different email address"})
		return
	}

	// Check if already a member
	var alreadyMember bool
	err = h.DB.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM budget_members
			WHERE budget_id = $1 AND user_id = $2
		)
	`, inv.BudgetID, userID).Scan(&alreadyMember)

	if alreadyMember {
		c.JSON(http.StatusConflict, gin.H{"error": "You are already a member"})
		return
	}

	// Add user to budget
	_, err = h.DB.Exec(`
		INSERT INTO budget_members (budget_id, user_id, role, permissions)
		VALUES ($1, $2, 'member', '{"read": true, "write": true}')
	`, inv.BudgetID, userID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add member"})
		return
	}

	// Mark invitation as accepted
	_, err = h.DB.Exec(`
		UPDATE invitations
		SET status = 'accepted'
		WHERE id = $1
	`, inv.ID)

	if err != nil {
		// Non-critical, just log
		c.JSON(http.StatusOK, gin.H{
			"message":   "Invitation accepted successfully",
			"budget_id": inv.BudgetID,
			"warning":   "Failed to update invitation status",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "Invitation accepted successfully",
		"budget_id": inv.BudgetID,
	})
}

// CancelInvitation cancels a pending invitation
func (h *InvitationHandler) CancelInvitation(c *gin.Context) {
	userID := middleware.GetUserID(c)
	budgetID := c.Param("id")
	invitationID := c.Param("invitation_id")

	// Check if user is member of budget
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

	// Delete invitation
	result, err := h.DB.Exec(`
		DELETE FROM invitations
		WHERE id = $1 AND budget_id = $2 AND status = 'pending'
	`, invitationID, budgetID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to cancel invitation"})
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invitation not found or already processed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Invitation cancelled successfully"})
}

// RemoveMember removes a member from a budget
func (h *InvitationHandler) RemoveMember(c *gin.Context) {
	userID := middleware.GetUserID(c)
	budgetID := c.Param("id")
	memberID := c.Param("member_id")

	// Check if user is owner
	var isOwner bool
	err := h.DB.QueryRow(`
		SELECT owner_id = $1
		FROM budgets
		WHERE id = $2
	`, userID, budgetID).Scan(&isOwner)

	if err != nil || !isOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": "Only owner can remove members"})
		return
	}

	// Can't remove owner
	if memberID == userID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Owner cannot be removed"})
		return
	}

	// Remove member
	result, err := h.DB.Exec(`
		DELETE FROM budget_members
		WHERE budget_id = $1 AND user_id = $2
	`, budgetID, memberID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove member"})
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Member not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Member removed successfully"})
}
package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"budget-api/internal/models"

	"github.com/google/uuid"
)

type BudgetService struct {
	db *sql.DB
}

func NewBudgetService(db *sql.DB) *BudgetService {
	return &BudgetService{db: db}
}

// Create creates a new budget
func (s *BudgetService) Create(ctx context.Context, name, ownerID string) (*models.Budget, error) {
	budget := &models.Budget{
		ID:        uuid.New().String(),
		Name:      name,
		OwnerID:   ownerID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	query := `
		INSERT INTO budgets (id, name, owner_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
	`

	_, err := s.db.ExecContext(ctx, query,
		budget.ID, budget.Name, budget.OwnerID,
		budget.CreatedAt, budget.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	// Add owner as member
	memberQuery := `
		INSERT INTO budget_members (id, budget_id, user_id, role, joined_at)
		VALUES ($1, $2, $3, $4, $5)
	`

	_, err = s.db.ExecContext(ctx, memberQuery,
		uuid.New().String(), budget.ID, ownerID, "owner", time.Now(),
	)

	if err != nil {
		return nil, err
	}

	return budget, nil
}

// GetByID gets a budget by ID
func (s *BudgetService) GetByID(ctx context.Context, id, userID string) (*models.Budget, error) {
	query := `
		SELECT b.id, b.name, b.owner_id, b.created_at, b.updated_at,
		       CASE WHEN b.owner_id = $2 THEN true ELSE false END as is_owner,
		       u.name as owner_name
		FROM budgets b
		LEFT JOIN users u ON b.owner_id = u.id
		INNER JOIN budget_members bm ON b.id = bm.budget_id
		WHERE b.id = $1 AND bm.user_id = $2
	`

	var budget models.Budget
	err := s.db.QueryRowContext(ctx, query, id, userID).Scan(
		&budget.ID,
		&budget.Name,
		&budget.OwnerID,
		&budget.CreatedAt,
		&budget.UpdatedAt,
		&budget.IsOwner,
		&budget.OwnerName,
	)

	if err != nil {
		return nil, err
	}

	// Get members
	members, err := s.GetMembers(ctx, id)
	if err != nil {
		return nil, err
	}
	budget.Members = members

	return &budget, nil
}

// GetUserBudgets gets all budgets for a user
func (s *BudgetService) GetUserBudgets(ctx context.Context, userID string) ([]models.Budget, error) {
	query := `
		SELECT b.id, b.name, b.owner_id, b.created_at, b.updated_at,
		       CASE WHEN b.owner_id = $1 THEN true ELSE false END as is_owner
		FROM budgets b
		INNER JOIN budget_members bm ON b.id = bm.budget_id
		WHERE bm.user_id = $1
		ORDER BY b.created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var budgets []models.Budget
	for rows.Next() {
		var budget models.Budget
		err := rows.Scan(
			&budget.ID,
			&budget.Name,
			&budget.OwnerID,
			&budget.CreatedAt,
			&budget.UpdatedAt,
			&budget.IsOwner,
		)
		if err != nil {
			return nil, err
		}

		// Get members for each budget
		members, _ := s.GetMembers(ctx, budget.ID)
		budget.Members = members

		budgets = append(budgets, budget)
	}

	return budgets, nil
}

// Update updates a budget
func (s *BudgetService) Update(ctx context.Context, id, name string) error {
	query := `
		UPDATE budgets
		SET name = $1, updated_at = $2
		WHERE id = $3
	`

	_, err := s.db.ExecContext(ctx, query, name, time.Now(), id)
	return err
}

// Delete deletes a budget (NOUVEAU)
func (s *BudgetService) Delete(ctx context.Context, budgetID string) error {
	// Delete members first (foreign key constraint)
	_, err := s.db.ExecContext(ctx, "DELETE FROM budget_members WHERE budget_id = $1", budgetID)
	if err != nil {
		return err
	}

	// Delete invitations
	_, err = s.db.ExecContext(ctx, "DELETE FROM invitations WHERE budget_id = $1", budgetID)
	if err != nil {
		return err
	}

	// Delete budget
	_, err = s.db.ExecContext(ctx, "DELETE FROM budgets WHERE id = $1", budgetID)
	return err
}

// GetData gets the data for a budget
func (s *BudgetService) GetData(ctx context.Context, budgetID string) (interface{}, error) {
	query := `SELECT data FROM budgets WHERE id = $1`

	var dataJSON []byte
	err := s.db.QueryRowContext(ctx, query, budgetID).Scan(&dataJSON)
	if err != nil {
		return nil, err
	}

	if len(dataJSON) == 0 {
		return map[string]interface{}{}, nil
	}

	var data interface{}
	if err := json.Unmarshal(dataJSON, &data); err != nil {
		return nil, err
	}

	return data, nil
}

// UpdateData updates the data for a budget
func (s *BudgetService) UpdateData(ctx context.Context, budgetID string, data interface{}) error {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return err
	}

	query := `
		UPDATE budgets
		SET data = $1, updated_at = $2
		WHERE id = $3
	`

	_, err = s.db.ExecContext(ctx, query, dataJSON, time.Now(), budgetID)
	return err
}

// GetMembers gets all members of a budget
func (s *BudgetService) GetMembers(ctx context.Context, budgetID string) ([]models.BudgetMember, error) {
	query := `
		SELECT bm.id, bm.user_id, bm.role, bm.joined_at, u.name, u.email
		FROM budget_members bm
		JOIN users u ON bm.user_id = u.id
		WHERE bm.budget_id = $1
		ORDER BY bm.joined_at
	`

	rows, err := s.db.QueryContext(ctx, query, budgetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []models.BudgetMember
	for rows.Next() {
		var member models.BudgetMember
		err := rows.Scan(
			&member.ID,
			&member.UserID,
			&member.Role,
			&member.JoinedAt,
			&member.UserName,
			&member.UserEmail,
		)
		if err != nil {
			return nil, err
		}
		members = append(members, member)
	}

	return members, nil
}

// CreateInvitation creates an invitation
func (s *BudgetService) CreateInvitation(ctx context.Context, budgetID, email, invitedBy string) (*models.Invitation, error) {
	invitation := &models.Invitation{
		ID:        uuid.New().String(),
		BudgetID:  budgetID,
		Email:     email,
		Token:     uuid.New().String(),
		Status:    "pending",
		InvitedBy: invitedBy,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour), // 7 days
		CreatedAt: time.Now(),
	}

	query := `
		INSERT INTO invitations (id, budget_id, email, token, status, invited_by, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	_, err := s.db.ExecContext(ctx, query,
		invitation.ID, invitation.BudgetID, invitation.Email,
		invitation.Token, invitation.Status, invitation.InvitedBy,
		invitation.ExpiresAt, invitation.CreatedAt,
	)

	if err != nil {
		return nil, err
	}

	return invitation, nil
}

// GetPendingInvitation gets a pending invitation (NOUVEAU)
func (s *BudgetService) GetPendingInvitation(ctx context.Context, budgetID, email string) (*models.Invitation, error) {
	query := `
		SELECT id, budget_id, email, token, expires_at, created_at
		FROM invitations
		WHERE budget_id = $1 AND email = $2 AND status = 'pending' AND expires_at > NOW()
		LIMIT 1
	`

	var invitation models.Invitation
	err := s.db.QueryRowContext(ctx, query, budgetID, email).Scan(
		&invitation.ID,
		&invitation.BudgetID,
		&invitation.Email,
		&invitation.Token,
		&invitation.ExpiresAt,
		&invitation.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &invitation, nil
}

// DeleteInvitation deletes an invitation (NOUVEAU)
func (s *BudgetService) DeleteInvitation(ctx context.Context, invitationID string) error {
	query := `DELETE FROM invitations WHERE id = $1`
	_, err := s.db.ExecContext(ctx, query, invitationID)
	return err
}

// AcceptInvitation accepts an invitation
func (s *BudgetService) AcceptInvitation(ctx context.Context, token, userID string) error {
	// Get invitation
	var invitation models.Invitation
	query := `
		SELECT id, budget_id, email, expires_at
		FROM invitations
		WHERE token = $1 AND status = 'pending'
	`

	err := s.db.QueryRowContext(ctx, query, token).Scan(
		&invitation.ID,
		&invitation.BudgetID,
		&invitation.Email,
		&invitation.ExpiresAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return sql.ErrNoRows
		}
		return err
	}

	// Check if expired
	if time.Now().After(invitation.ExpiresAt) {
		return sql.ErrNoRows
	}

	// Add user as member
	memberQuery := `
		INSERT INTO budget_members (id, budget_id, user_id, role, joined_at)
		VALUES ($1, $2, $3, $4, $5)
	`

	_, err = s.db.ExecContext(ctx, memberQuery,
		uuid.New().String(),
		invitation.BudgetID,
		userID,
		"member",
		time.Now(),
	)

	if err != nil {
		return err
	}

	// Update invitation status
	updateQuery := `
		UPDATE invitations
		SET status = 'accepted', updated_at = $1
		WHERE id = $2
	`

	_, err = s.db.ExecContext(ctx, updateQuery, time.Now(), invitation.ID)
	return err
}
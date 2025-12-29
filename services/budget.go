package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/LovationAdmin/budget-api/models"
	"github.com/LovationAdmin/budget-api/utils"

	"github.com/google/uuid"
)

type BudgetService struct {
	db *sql.DB
}

func NewBudgetService(db *sql.DB) *BudgetService {
	return &BudgetService{db: db}
}

// Helper struct for DB storage of encrypted blobs
type EncryptedData struct {
	Encrypted string `json:"encrypted"`
}

// Create creates a new budget with transactional safety
func (s *BudgetService) Create(ctx context.Context, name, ownerID string) (*models.Budget, error) {
	budget := &models.Budget{
		ID:        uuid.New().String(),
		Name:      name,
		OwnerID:   ownerID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	err := utils.WithTransaction(s.db, func(tx *sql.Tx) error {
		// 1. Insert Budget
		query := `
			INSERT INTO budgets (id, name, owner_id, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5)
		`
		if _, err := tx.ExecContext(ctx, query, budget.ID, budget.Name, budget.OwnerID, budget.CreatedAt, budget.UpdatedAt); err != nil {
			return err
		}

		// 2. Add Owner as Member
		memberQuery := `
			INSERT INTO budget_members (id, budget_id, user_id, role, joined_at)
			VALUES ($1, $2, $3, $4, $5)
		`
		if _, err := tx.ExecContext(ctx, memberQuery, uuid.New().String(), budget.ID, ownerID, "owner", time.Now()); err != nil {
			return err
		}

		return nil
	})

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

		members, _ := s.GetMembers(ctx, budget.ID)
		budget.Members = members

		budgets = append(budgets, budget)
	}

	return budgets, nil
}

// Update updates a budget name
func (s *BudgetService) Update(ctx context.Context, id, name string) error {
	query := `
		UPDATE budgets
		SET name = $1, updated_at = $2
		WHERE id = $3
	`
	_, err := s.db.ExecContext(ctx, query, name, time.Now(), id)
	return err
}

// Delete deletes a budget completely
func (s *BudgetService) Delete(ctx context.Context, budgetID string) error {
	return utils.WithTransaction(s.db, func(tx *sql.Tx) error {
		// Delete related records first
		if _, err := tx.ExecContext(ctx, "DELETE FROM budget_members WHERE budget_id = $1", budgetID); err != nil { return err }
		if _, err := tx.ExecContext(ctx, "DELETE FROM invitations WHERE budget_id = $1", budgetID); err != nil { return err }
		if _, err := tx.ExecContext(ctx, "DELETE FROM budget_data WHERE budget_id = $1", budgetID); err != nil { return err }
		// Delete budget
		if _, err := tx.ExecContext(ctx, "DELETE FROM budgets WHERE id = $1", budgetID); err != nil { return err }
		return nil
	})
}

// GetData gets the data for a budget and DECRYPTS it
func (s *BudgetService) GetData(ctx context.Context, budgetID string) (interface{}, error) {
	query := `SELECT data FROM budget_data WHERE budget_id = $1 ORDER BY updated_at DESC LIMIT 1`

	var rawJSON []byte
	err := s.db.QueryRowContext(ctx, query, budgetID).Scan(&rawJSON)
	if err == sql.ErrNoRows {
		return map[string]interface{}{}, nil
	}
	if err != nil {
		return nil, err
	}

	if len(rawJSON) == 0 {
		return map[string]interface{}{}, nil
	}

	// 1. Try to unmarshal as EncryptedData wrapper
	var wrapper EncryptedData
	if err := json.Unmarshal(rawJSON, &wrapper); err == nil && wrapper.Encrypted != "" {
		// 2. It IS encrypted -> Decrypt it
		decryptedBytes, err := utils.Decrypt(wrapper.Encrypted)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt data: %w", err)
		}
		
		// 3. Unmarshal the real data
		var realData interface{}
		if err := json.Unmarshal(decryptedBytes, &realData); err != nil {
			return nil, err
		}
		return realData, nil
	}

	// Fallback: If it wasn't encrypted (legacy data), return it as is
	var data interface{}
	if err := json.Unmarshal(rawJSON, &data); err != nil {
		return nil, err
	}

	return data, nil
}

// UpdateData ENCRYPTS the data before saving it
func (s *BudgetService) UpdateData(ctx context.Context, budgetID string, data interface{}) error {
	// 1. Convert real data to JSON bytes
	realDataJSON, err := json.Marshal(data)
	if err != nil {
		return err
	}

	// 2. Encrypt the bytes
	encryptedString, err := utils.Encrypt(realDataJSON)
	if err != nil {
		return err
	}

	// 3. Wrap in a JSON object so Postgres JSONB column accepts it
	wrapper := EncryptedData{Encrypted: encryptedString}
	storageJSON, err := json.Marshal(wrapper)
	if err != nil {
		return err
	}

	// 4. Save to DB
	var existingID string
	checkQuery := `SELECT id FROM budget_data WHERE budget_id = $1 LIMIT 1`
	err = s.db.QueryRowContext(ctx, checkQuery, budgetID).Scan(&existingID)

	if err == sql.ErrNoRows {
		insertQuery := `
			INSERT INTO budget_data (id, budget_id, data, version, updated_at)
			VALUES ($1, $2, $3, 1, $4)
		`
		_, err = s.db.ExecContext(ctx, insertQuery, uuid.New().String(), budgetID, storageJSON, time.Now())
		return err
	}

	if err != nil {
		return err
	}

	updateQuery := `
		UPDATE budget_data
		SET data = $1, version = version + 1, updated_at = $2
		WHERE budget_id = $3
	`
	_, err = s.db.ExecContext(ctx, updateQuery, storageJSON, time.Now(), budgetID)
	return err
}

// GetMembers gets all members of a budget (Populates Avatar)
func (s *BudgetService) GetMembers(ctx context.Context, budgetID string) ([]models.BudgetMember, error) {
	query := `
		SELECT bm.id, bm.user_id, bm.role, bm.joined_at, u.name, u.email, COALESCE(u.avatar, '')
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
		var avatar string

		err := rows.Scan(
			&member.ID,
			&member.UserID,
			&member.Role,
			&member.JoinedAt,
			&member.UserName,
			&member.UserEmail,
			&avatar,
		)
		if err != nil {
			return nil, err
		}
		
		member.User = &models.User{
			ID:    member.UserID,
			Name:  member.UserName,
			Email: member.UserEmail,
			Avatar: avatar,
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
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
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

// GetPendingInvitation gets a pending invitation
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

// DeleteInvitation deletes an invitation
func (s *BudgetService) DeleteInvitation(ctx context.Context, invitationID string) error {
	query := `DELETE FROM invitations WHERE id = $1`
	_, err := s.db.ExecContext(ctx, query, invitationID)
	return err
}

// IsMemberByEmail checks if an email is already a member of a budget
func (s *BudgetService) IsMemberByEmail(ctx context.Context, budgetID, email string) (bool, error) {
    query := `
        SELECT EXISTS(
            SELECT 1 FROM budget_members bm
            JOIN users u ON bm.user_id = u.id
            WHERE bm.budget_id = $1 AND u.email = $2
        )
    `
    var exists bool
    err := s.db.QueryRowContext(ctx, query, budgetID, email).Scan(&exists)
    return exists, err
}

// AcceptInvitation accepts invitation, cleans up duplicates, and notifies others via "fake update"
func (s *BudgetService) AcceptInvitation(ctx context.Context, token, userID string) error {
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

	if time.Now().After(invitation.ExpiresAt) {
		return sql.ErrNoRows
	}

	return utils.WithTransaction(s.db, func(tx *sql.Tx) error {
        // 1. Get User Name for Notification
        var userName string
        if err := tx.QueryRowContext(ctx, "SELECT name FROM users WHERE id = $1", userID).Scan(&userName); err != nil {
            return err
        }

		// 2. Add Member
		memberQuery := `
			INSERT INTO budget_members (id, budget_id, user_id, role, joined_at)
			VALUES ($1, $2, $3, $4, $5)
		`
		if _, err := tx.ExecContext(ctx, memberQuery, uuid.New().String(), invitation.BudgetID, userID, "member", time.Now()); err != nil {
			return err
		}

		// 3. Mark CURRENT invitation accepted
		updateQuery := `
			UPDATE invitations
			SET status = 'accepted', updated_at = $1
			WHERE id = $2
		`
		if _, err := tx.ExecContext(ctx, updateQuery, time.Now(), invitation.ID); err != nil {
			return err
		}

        // 4. CLEANUP: Delete pending invites for this email
        cleanupQuery := `DELETE FROM invitations WHERE budget_id = $1 AND email = $2 AND status = 'pending'`
        if _, err := tx.ExecContext(ctx, cleanupQuery, invitation.BudgetID, invitation.Email); err != nil {
            return err
        }

        // 5. NOTIFICATION TRIGGER: Update budget metadata (forces polling frontend to notice a change)
        timestamp := time.Now().Format(time.RFC3339)
        notifyQuery := `
            UPDATE budget_data 
            SET data = data || jsonb_build_object('lastUpdated', $1::text, 'updatedBy', $2::text),
                version = version + 1,
                updated_at = NOW()
            WHERE budget_id = $3
        `
        tx.ExecContext(ctx, notifyQuery, timestamp, userName, invitation.BudgetID)

		return nil
	})
}
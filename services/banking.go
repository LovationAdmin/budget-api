package services

import (
	"context"
	"database/sql"
	"time"

	"budget-api/models"
	"budget-api/utils"

	"github.com/google/uuid"
)

type BankingService struct {
	db *sql.DB
}

func NewBankingService(db *sql.DB) *BankingService {
	return &BankingService{db: db}
}

// GetBudgetConnections renvoie les connexions pour un BUDGET spécifique
func (s *BankingService) GetBudgetConnections(ctx context.Context, budgetID string) ([]models.BankConnection, error) {
	query := `
		SELECT id, institution_id, institution_name, status, expires_at, created_at
		FROM bank_connections
		WHERE budget_id = $1
		ORDER BY created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, budgetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var connections []models.BankConnection
	for rows.Next() {
		var conn models.BankConnection
		err := rows.Scan(&conn.ID, &conn.InstitutionID, &conn.InstitutionName, &conn.Status, &conn.ExpiresAt, &conn.CreatedAt)
		if err != nil {
			return nil, err
		}

		accounts, err := s.GetAccountsByConnection(ctx, conn.ID)
		if err == nil {
			conn.Accounts = accounts
		}

		connections = append(connections, conn)
	}

	return connections, nil
}

// GetAccountsByConnection fetches accounts for a specific connection
func (s *BankingService) GetAccountsByConnection(ctx context.Context, connectionID string) ([]models.BankAccount, error) {
	query := `
		SELECT id, connection_id, name, mask, currency, balance, is_savings_pool, last_synced_at
		FROM bank_accounts
		WHERE connection_id = $1
		ORDER BY name
	`
	rows, err := s.db.QueryContext(ctx, query, connectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []models.BankAccount
	for rows.Next() {
		var acc models.BankAccount
		err := rows.Scan(
			&acc.ID, &acc.ConnectionID, &acc.Name, &acc.Mask,
			&acc.Currency, &acc.Balance, &acc.IsSavingsPool, &acc.LastSyncedAt,
		)
		if err != nil {
			continue
		}
		accounts = append(accounts, acc)
	}
	return accounts, nil
}

// GetRealityCheckSum calcule le total pour un BUDGET spécifique
func (s *BankingService) GetRealityCheckSum(ctx context.Context, budgetID string) (float64, error) {
	query := `
		SELECT COALESCE(SUM(ba.balance), 0)
		FROM bank_accounts ba
		JOIN bank_connections bc ON ba.connection_id = bc.id
		WHERE bc.budget_id = $1 AND ba.is_savings_pool = TRUE
	`
	var total float64
	err := s.db.QueryRowContext(ctx, query, budgetID).Scan(&total)
	return total, err
}

// UpdateAccountPool toggles whether an account counts towards the Reality Check
func (s *BankingService) UpdateAccountPool(ctx context.Context, accountID string, isSavingsPool bool) error {
	_, err := s.db.ExecContext(ctx, "UPDATE bank_accounts SET is_savings_pool = $1 WHERE id = $2", isSavingsPool, accountID)
	return err
}

// DeleteConnection removes a connection and its accounts
func (s *BankingService) DeleteConnection(ctx context.Context, connectionID string) error {
	return utils.WithTransaction(s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM bank_accounts WHERE connection_id = $1", connectionID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM bank_connections WHERE id = $1", connectionID); err != nil {
			return err
		}
		return nil
	})
}

// SaveConnectionWithTokens saves the connection LINKED TO A BUDGET (Upsert Logic)
func (s *BankingService) SaveConnectionWithTokens(ctx context.Context, userID, budgetID, institutionID, institutionName, providerConnID, accessToken, refreshToken string, expiresAt time.Time) (string, error) {
	// 1. Encrypt Tokens
	encAccess, err := utils.Encrypt([]byte(accessToken))
	if err != nil {
		return "", err
	}
	encRefresh, err := utils.Encrypt([]byte(refreshToken))
	if err != nil {
		return "", err
	}

    // 2. Check if connection exists for this budget (Upsert Logic)
    var existingID string
    err = s.db.QueryRowContext(ctx, 
        "SELECT id FROM bank_connections WHERE provider_connection_id = $1 AND budget_id = $2", 
        providerConnID, budgetID).Scan(&existingID)

    if err == nil {
        // UPDATE Existing
        _, err = s.db.ExecContext(ctx, `
            UPDATE bank_connections 
            SET encrypted_access_token = $1, encrypted_refresh_token = $2, expires_at = $3, updated_at = NOW(), status = 'active'
            WHERE id = $4
        `, encAccess, encRefresh, expiresAt, existingID)
        return existingID, err
    }

    // INSERT New
	connID := uuid.New().String()
	query := `
		INSERT INTO bank_connections (id, user_id, budget_id, institution_id, institution_name, provider_connection_id, encrypted_access_token, encrypted_refresh_token, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err = s.db.ExecContext(ctx, query, connID, userID, budgetID, institutionID, institutionName, providerConnID, encAccess, encRefresh, expiresAt)
	return connID, err
}

// SaveAccount saves a bank account linked to a connection
func (s *BankingService) SaveAccount(ctx context.Context, connID, externalID, name, mask, currency string, balance float64) error {
    // Basic upsert on ID logic is tricky without a unique constraint on external_account_id
    // For now, we attempt insert, if duplicated we might want to handle cleaning up duplicates later
    // or add a unique constraint on (external_account_id, connection_id).
    
    // To be safe and simple: Check existence
    var exists int
    s.db.QueryRowContext(ctx, "SELECT 1 FROM bank_accounts WHERE external_account_id = $1 AND connection_id = $2", externalID, connID).Scan(&exists)

    if exists == 1 {
        _, err := s.db.ExecContext(ctx, 
            "UPDATE bank_accounts SET balance = $1, name = $2, last_synced_at = NOW() WHERE external_account_id = $3 AND connection_id = $4",
            balance, name, externalID, connID)
        return err
    }

	insertQuery := `
		INSERT INTO bank_accounts (id, connection_id, external_account_id, name, mask, currency, balance, last_synced_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
	`
	_, err := s.db.ExecContext(ctx, insertQuery, uuid.New().String(), connID, externalID, name, mask, currency, balance)
	return err
}
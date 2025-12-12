package services

import (
	"context"
	"database/sql"
	"errors"
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

// GetUserConnections returns all bank connections and their accounts for a user
func (s *BankingService) GetUserConnections(ctx context.Context, userID string) ([]models.BankConnection, error) {
	query := `
		SELECT id, institution_id, institution_name, status, expires_at, created_at
		FROM bank_connections
		WHERE user_id = $1
		ORDER BY created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, userID)
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

		// Fetch accounts for this connection
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

// GetRealityCheckSum calculates the total money available in "Savings Pool" accounts
func (s *BankingService) GetRealityCheckSum(ctx context.Context, userID string) (float64, error) {
	query := `
		SELECT COALESCE(SUM(ba.balance), 0)
		FROM bank_accounts ba
		JOIN bank_connections bc ON ba.connection_id = bc.id
		WHERE bc.user_id = $1 AND ba.is_savings_pool = TRUE
	`
	var total float64
	err := s.db.QueryRowContext(ctx, query, userID).Scan(&total)
	return total, err
}

// UpdateAccountPool toggles whether an account counts towards the Reality Check
func (s *BankingService) UpdateAccountPool(ctx context.Context, accountID, userID string, isSavingsPool bool) error {
	// Security check: Ensure account belongs to user
	checkQuery := `
		SELECT COUNT(*) 
		FROM bank_accounts ba
		JOIN bank_connections bc ON ba.connection_id = bc.id
		WHERE ba.id = $1 AND bc.user_id = $2
	`
	var count int
	err := s.db.QueryRowContext(ctx, checkQuery, accountID, userID).Scan(&count)
	if err != nil {
		return err
	}
	if count == 0 {
		return errors.New("account not found or access denied")
	}

	// Update
	_, err = s.db.ExecContext(ctx, "UPDATE bank_accounts SET is_savings_pool = $1 WHERE id = $2", isSavingsPool, accountID)
	return err
}

// DeleteConnection removes a connection and its accounts
func (s *BankingService) DeleteConnection(ctx context.Context, connectionID, userID string) error {
	// Security check
	var count int
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM bank_connections WHERE id = $1 AND user_id = $2", connectionID, userID).Scan(&count)
	if count == 0 {
		return errors.New("connection not found")
	}

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

// SaveConnectionWithTokens saves the connection and encrypts sensitive tokens
func (s *BankingService) SaveConnectionWithTokens(ctx context.Context, userID, institutionID, institutionName, providerConnID, accessToken, refreshToken string, expiresAt time.Time) (string, error) {
	// 1. Encrypt Tokens
	encAccess, err := utils.Encrypt([]byte(accessToken))
	if err != nil {
		return "", err
	}
	encRefresh, err := utils.Encrypt([]byte(refreshToken))
	if err != nil {
		return "", err
	}

	connID := uuid.New().String()

	query := `
		INSERT INTO bank_connections (id, user_id, institution_id, institution_name, provider_connection_id, encrypted_access_token, encrypted_refresh_token, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err = s.db.ExecContext(ctx, query, connID, userID, institutionID, institutionName, providerConnID, encAccess, encRefresh, expiresAt)
	return connID, err
}

// SaveAccount saves a bank account linked to a connection
func (s *BankingService) SaveAccount(ctx context.Context, connID, externalID, name, mask, currency string, balance float64) error {
	query := `
		INSERT INTO bank_accounts (id, connection_id, external_account_id, name, mask, currency, balance, last_synced_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
	`
	_, err := s.db.ExecContext(ctx, query, uuid.New().String(), connID, externalID, name, mask, currency, balance)
	return err
}
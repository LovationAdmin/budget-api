package services

import (
	"context"
	"database/sql"
	"time"
	"fmt"

	"budget-api/models"
	"budget-api/utils"

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

// SaveConnectionWithTokens sauvegarde une connexion Enable Banking
func (s *BankingService) SaveConnectionWithTokens(
	ctx context.Context,
	userID string,
	budgetID string,
	accountUID string,
	bankName string,
	sessionID string,
	provider string,
	accessToken string,
	expiresAt time.Time,
) (string, error) {
	
	// Extraire le pays du nom de la banque (ou utiliser une valeur par défaut)
	country := "FR" // Par défaut
	
	// Pour Enable Banking, on utilise le session_id comme identifiant
	// et on stocke le nom de la banque dans aspsp_name
	
	query := `
		INSERT INTO banking_connections (
			user_id,
			budget_id,
			aspsp_name,
			aspsp_country,
			session_id,
			access_token,
			expires_at,
			status,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
		ON CONFLICT (user_id, budget_id, aspsp_name, aspsp_country)
		DO UPDATE SET
			session_id = EXCLUDED.session_id,
			access_token = EXCLUDED.access_token,
			expires_at = EXCLUDED.expires_at,
			updated_at = NOW()
		RETURNING id
	`
	
	var connectionID string
	err := s.db.QueryRowContext(
		ctx,
		query,
		userID,
		budgetID,
		bankName,
		country,
		sessionID,
		accessToken,
		expiresAt,
		"active",
	).Scan(&connectionID)
	
	if err != nil {
		return "", fmt.Errorf("failed to save connection: %w", err)
	}
	
	return connectionID, nil
}

// SaveAccount sauvegarde un compte bancaire Enable Banking
func (s *BankingService) SaveAccount(
	ctx context.Context,
	connectionID string,
	accountID string, // C'est le UID du compte Enable Banking
	name string,
	mask string,
	currency string,
	balance float64,
) error {
	
	query := `
		INSERT INTO banking_accounts (
			connection_id,
			account_id,
			account_name,
			account_type,
			currency,
			balance,
			last_sync_at,
			created_at
		) VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
		ON CONFLICT (connection_id, account_id)
		DO UPDATE SET
			account_name = EXCLUDED.account_name,
			currency = EXCLUDED.currency,
			balance = EXCLUDED.balance,
			last_sync_at = NOW()
	`
	
	_, err := s.db.ExecContext(
		ctx,
		query,
		connectionID,
		accountID,
		name,
		"CACC", // Type par défaut, pourrait être passé en paramètre
		currency,
		balance,
	)
	
	if err != nil {
		return fmt.Errorf("failed to save account: %w", err)
	}
	
	return nil
}

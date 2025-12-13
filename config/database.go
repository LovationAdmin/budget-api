package config

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/lib/pq"
)

func InitDB() (*sql.DB, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL environment variable is required")
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	return db, nil
}

func RunMigrations(db *sql.DB) error {
	migrations := []string{
		`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`,
		
		`CREATE TABLE IF NOT EXISTS users (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			email VARCHAR(255) UNIQUE NOT NULL,
			password_hash VARCHAR(255) NOT NULL,
			name VARCHAR(255) NOT NULL,
			totp_secret VARCHAR(255),
			totp_enabled BOOLEAN DEFAULT FALSE,
			email_verified BOOLEAN DEFAULT FALSE,
			avatar TEXT,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,
		
		`CREATE TABLE IF NOT EXISTS budgets (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			name VARCHAR(255) NOT NULL,
			owner_id UUID REFERENCES users(id) ON DELETE CASCADE,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,
		
		`CREATE TABLE IF NOT EXISTS budget_members (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			budget_id UUID REFERENCES budgets(id) ON DELETE CASCADE,
			user_id UUID REFERENCES users(id) ON DELETE CASCADE,
			role VARCHAR(50) DEFAULT 'member',
			permissions JSONB DEFAULT '{"read": true, "write": true}',
			joined_at TIMESTAMP DEFAULT NOW(),
			UNIQUE(budget_id, user_id)
		)`,
		
		`CREATE TABLE IF NOT EXISTS invitations (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			budget_id UUID REFERENCES budgets(id) ON DELETE CASCADE,
			email VARCHAR(255) NOT NULL,
			invited_by UUID REFERENCES users(id),
			token VARCHAR(255) UNIQUE NOT NULL,
			status VARCHAR(50) DEFAULT 'pending',
			expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,
		
		`CREATE TABLE IF NOT EXISTS budget_data (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			budget_id UUID REFERENCES budgets(id) ON DELETE CASCADE,
			data JSONB NOT NULL,
			version INTEGER DEFAULT 1,
			updated_by UUID REFERENCES users(id),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,
		
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			budget_id UUID REFERENCES budgets(id) ON DELETE CASCADE,
			user_id UUID REFERENCES users(id),
			action VARCHAR(100) NOT NULL,
			changes JSONB,
			created_at TIMESTAMP DEFAULT NOW()
		)`,
		
		`CREATE TABLE IF NOT EXISTS sessions (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			user_id UUID REFERENCES users(id) ON DELETE CASCADE,
			refresh_token VARCHAR(500) UNIQUE NOT NULL,
			expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT NOW()
		)`,
		
		`CREATE INDEX IF NOT EXISTS idx_budget_members_budget_id ON budget_members(budget_id)`,
		`CREATE INDEX IF NOT EXISTS idx_budget_members_user_id ON budget_members(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_budget_data_budget_id ON budget_data(budget_id)`,
		`CREATE INDEX IF NOT EXISTS idx_invitations_email ON invitations(email)`,
		`CREATE INDEX IF NOT EXISTS idx_invitations_token ON invitations(token)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_budget_id ON audit_logs(budget_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id)`,

		`ALTER TABLE invitations ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT NOW()`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS avatar TEXT`,

		`CREATE TABLE IF NOT EXISTS email_verifications (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			user_id UUID REFERENCES users(id) ON DELETE CASCADE,
			token VARCHAR(255) NOT NULL,
			expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_email_verifications_token ON email_verifications(token)`,

		// --- BANKING TABLES ---
		`CREATE TABLE IF NOT EXISTS bank_connections (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			institution_id VARCHAR(255) NOT NULL,
			institution_name VARCHAR(255),
			provider_connection_id VARCHAR(255) NOT NULL, -- NOTE: Removed UNIQUE constraint here manually in migration below
			encrypted_access_token TEXT,
			encrypted_refresh_token TEXT,
			expires_at TIMESTAMP,
			status VARCHAR(50) DEFAULT 'active',
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,

        // MIGRATION: Add budget_id support
        `ALTER TABLE bank_connections ADD COLUMN IF NOT EXISTS budget_id UUID REFERENCES budgets(id) ON DELETE CASCADE`,

        // MIGRATION: Fix Unique Constraints for Multi-Budget
        // 1. Drop the old strict constraint (if it exists from previous runs)
        `ALTER TABLE bank_connections DROP CONSTRAINT IF EXISTS bank_connections_provider_connection_id_key`,
        // 2. Drop our custom constraint if it exists (to ensure clean recreate)
        `ALTER TABLE bank_connections DROP CONSTRAINT IF EXISTS unique_provider_connection_per_budget`,
        // 3. Add the new composite constraint (Unique ProviderID + BudgetID)
        `ALTER TABLE bank_connections ADD CONSTRAINT unique_provider_connection_per_budget UNIQUE (provider_connection_id, budget_id)`,

		`CREATE TABLE IF NOT EXISTS bank_accounts (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			connection_id UUID NOT NULL REFERENCES bank_connections(id) ON DELETE CASCADE,
			external_account_id VARCHAR(255) NOT NULL,
			name VARCHAR(255),
			mask VARCHAR(10),
			currency VARCHAR(3) DEFAULT 'EUR',
			balance DECIMAL(20, 2) DEFAULT 0,
			is_savings_pool BOOLEAN DEFAULT FALSE,
			last_synced_at TIMESTAMP DEFAULT NOW()
		)`,

		`CREATE INDEX IF NOT EXISTS idx_bank_connections_user ON bank_connections(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_bank_accounts_connection ON bank_accounts(connection_id)`,
        `CREATE INDEX IF NOT EXISTS idx_bank_connections_budget ON bank_connections(budget_id)`,

        `CREATE TABLE IF NOT EXISTS label_mappings (
            normalized_label VARCHAR(255) PRIMARY KEY,
            category VARCHAR(50) NOT NULL,
            source VARCHAR(20) DEFAULT 'AI',
            created_at TIMESTAMP DEFAULT NOW()
        )`,
        `CREATE INDEX IF NOT EXISTS idx_label_mappings_label ON label_mappings(normalized_label)`,
	}

	for _, migration := range migrations {
		if _, err := db.Exec(migration); err != nil {
            // We log errors but don't fail hard, as some "DROP CONSTRAINT" might fail if constraint doesn't exist
            // which is expected on a fresh DB vs an existing one.
			fmt.Printf("Migration notice: %v\n", err)
		}
	}

	return nil
}
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
		
		// Updated definition for new installs
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

		// --- EMERGENCY FIX ---
		// This line will add the missing column to your existing database
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

	}

	for _, migration := range migrations {
		if _, err := db.Exec(migration); err != nil {
			return fmt.Errorf("failed to run migration: %w", err)
		}
	}

	return nil
}
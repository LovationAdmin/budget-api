package config

import (
	"database/sql"
	"fmt"
	"os"
	"time"

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

	// ============================================================================
	// 🚀 OPTIMISATIONS PERFORMANCE
	// ============================================================================
	
	// Augmenter le pool de connexions
	db.SetMaxOpenConns(25)                    // Max 25 connexions simultanées (bon pour Render)
	db.SetMaxIdleConns(10)                    // 5 → 10 (évite re-création)
	db.SetConnMaxLifetime(5 * time.Minute)    // Recycler connexions après 5min
	db.SetConnMaxIdleTime(2 * time.Minute)    // Fermer idle après 2min

	fmt.Println("✅ Database connection pool configured:")
	fmt.Printf("   - MaxOpenConns: 25\n")
	fmt.Printf("   - MaxIdleConns: 10\n")
	fmt.Printf("   - ConnMaxLifetime: 5m\n")
	fmt.Printf("   - ConnMaxIdleTime: 2m\n")

	return db, nil
}

func RunMigrations(db *sql.DB) error {
	fmt.Println("🔄 Running database migrations...")
	start := time.Now()

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

		`CREATE TABLE IF NOT EXISTS email_verifications (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			user_id UUID REFERENCES users(id) ON DELETE CASCADE,
			token VARCHAR(255) NOT NULL,
			expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT NOW()
		)`,

		// ============================================================================
		// 🆕 PASSWORD RESET TABLE
		// ============================================================================
		`CREATE TABLE IF NOT EXISTS password_resets (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			token VARCHAR(255) NOT NULL UNIQUE,
			expires_at TIMESTAMP NOT NULL,
			used BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMP DEFAULT NOW()
		)`,

		`CREATE TABLE IF NOT EXISTS bank_connections (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			budget_id UUID REFERENCES budgets(id) ON DELETE CASCADE,
			institution_id VARCHAR(255) NOT NULL,
			institution_name VARCHAR(255),
			provider_connection_id VARCHAR(255) NOT NULL,
			encrypted_access_token TEXT,
			encrypted_refresh_token TEXT,
			expires_at TIMESTAMP,
			status VARCHAR(50) DEFAULT 'active',
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,

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

		`CREATE TABLE IF NOT EXISTS label_mappings (
			normalized_label VARCHAR(255) PRIMARY KEY,
			category VARCHAR(50) NOT NULL,
			source VARCHAR(20) DEFAULT 'AI',
			created_at TIMESTAMP DEFAULT NOW()
		)`,

		`CREATE TABLE IF NOT EXISTS banking_connections (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			budget_id UUID NOT NULL REFERENCES budgets(id) ON DELETE CASCADE,
			aspsp_name VARCHAR(255) NOT NULL,
			aspsp_country VARCHAR(2) NOT NULL,
			session_id UUID NOT NULL,
			access_token TEXT,
			refresh_token TEXT,
			expires_at TIMESTAMP,
			status VARCHAR(50) DEFAULT 'active',
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,

		`CREATE TABLE IF NOT EXISTS banking_accounts (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			connection_id UUID NOT NULL REFERENCES banking_connections(id) ON DELETE CASCADE,
			account_id UUID NOT NULL,
			account_name VARCHAR(255),
			account_type VARCHAR(50),
			currency VARCHAR(3),
			balance DECIMAL(15,2),
			last_sync_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT NOW()
		)`,

		`CREATE TABLE IF NOT EXISTS market_suggestions (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			category VARCHAR(50) NOT NULL,
			country VARCHAR(2) NOT NULL,
			merchant_name VARCHAR(255),
			competitors JSONB NOT NULL,
			last_updated TIMESTAMP DEFAULT NOW(),
			expires_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT NOW()
		)`,

		`CREATE TABLE IF NOT EXISTS ai_api_usage (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			user_id UUID REFERENCES users(id) ON DELETE SET NULL,
			request_type VARCHAR(50) NOT NULL,
			category VARCHAR(50),
			country VARCHAR(2),
			input_tokens INT DEFAULT 0,
			output_tokens INT DEFAULT 0,
			total_tokens INT DEFAULT 0,
			cost_usd DECIMAL(10, 6) DEFAULT 0,
			cache_hit BOOLEAN DEFAULT FALSE,
			duration_ms INT,
			created_at TIMESTAMP DEFAULT NOW()
		)`,

		`CREATE TABLE IF NOT EXISTS affiliate_links (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			category VARCHAR(50) NOT NULL,
			country VARCHAR(2) NOT NULL,
			provider_name VARCHAR(255) NOT NULL,
			affiliate_url TEXT NOT NULL,
			commission_rate DECIMAL(5, 2),
			is_active BOOLEAN DEFAULT TRUE,
			priority INT DEFAULT 0,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,

		// ============================================================================
		// 🚀 INDEXES CRITIQUES POUR PERFORMANCE
		// ============================================================================
		
		// Indexes budget_members (CRITICAL - évite full table scan)
		`CREATE INDEX IF NOT EXISTS idx_budget_members_budget_id ON budget_members(budget_id)`,
		`CREATE INDEX IF NOT EXISTS idx_budget_members_user_id ON budget_members(user_id)`,
		
		// Indexes budget_data (CRITICAL - accès rapide aux données)
		`CREATE INDEX IF NOT EXISTS idx_budget_data_budget_id ON budget_data(budget_id)`,
		
		// Indexes invitations
		`CREATE INDEX IF NOT EXISTS idx_invitations_email ON invitations(email)`,
		`CREATE INDEX IF NOT EXISTS idx_invitations_token ON invitations(token)`,
		`CREATE INDEX IF NOT EXISTS idx_invitations_budget_id ON invitations(budget_id)`,
		
		// Indexes audit_logs
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_budget_id ON audit_logs(budget_id)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_user_id ON audit_logs(user_id)`,
		
		// Indexes sessions
		`CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_refresh_token ON sessions(refresh_token)`,
		
		// Indexes email_verifications
		`CREATE INDEX IF NOT EXISTS idx_email_verifications_token ON email_verifications(token)`,
		`CREATE INDEX IF NOT EXISTS idx_email_verifications_user_id ON email_verifications(user_id)`,
		
		// 🆕 Indexes password_resets
		`CREATE INDEX IF NOT EXISTS idx_password_resets_token ON password_resets(token)`,
		`CREATE INDEX IF NOT EXISTS idx_password_resets_user_id ON password_resets(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_password_resets_expires_at ON password_resets(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_password_resets_used ON password_resets(used)`,
		
		// Indexes users
		`CREATE INDEX IF NOT EXISTS idx_users_email ON users(email)`,
		`CREATE INDEX IF NOT EXISTS idx_users_country ON users(country)`,
		
		// Indexes budgets
		`CREATE INDEX IF NOT EXISTS idx_budgets_owner_id ON budgets(owner_id)`,
		`CREATE INDEX IF NOT EXISTS idx_budgets_created_at ON budgets(created_at)`,
		
		// Indexes bank_connections
		`CREATE INDEX IF NOT EXISTS idx_bank_connections_user ON bank_connections(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_bank_connections_budget ON bank_connections(budget_id)`,
		
		// Indexes bank_accounts
		`CREATE INDEX IF NOT EXISTS idx_bank_accounts_connection ON bank_accounts(connection_id)`,
		
		// Indexes label_mappings
		`CREATE INDEX IF NOT EXISTS idx_label_mappings_label ON label_mappings(normalized_label)`,
		
		// Indexes banking_connections (Enable Banking)
		`CREATE INDEX IF NOT EXISTS idx_banking_connections_user_budget ON banking_connections(user_id, budget_id)`,
		`CREATE INDEX IF NOT EXISTS idx_banking_connections_session ON banking_connections(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_banking_connections_status ON banking_connections(status)`,
		
		// Indexes banking_accounts
		`CREATE INDEX IF NOT EXISTS idx_banking_accounts_connection ON banking_accounts(connection_id)`,
		`CREATE INDEX IF NOT EXISTS idx_banking_accounts_last_sync ON banking_accounts(last_sync_at)`,
		
		// Indexes market_suggestions
		`CREATE INDEX IF NOT EXISTS idx_market_suggestions_category_country ON market_suggestions(category, country)`,
		`CREATE INDEX IF NOT EXISTS idx_market_suggestions_expires ON market_suggestions(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_market_suggestions_merchant ON market_suggestions(merchant_name)`,
		
		// Indexes ai_api_usage
		`CREATE INDEX IF NOT EXISTS idx_ai_usage_user ON ai_api_usage(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_usage_type ON ai_api_usage(request_type)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_usage_created ON ai_api_usage(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_usage_cache ON ai_api_usage(cache_hit)`,
		
		// Indexes affiliate_links
		`CREATE INDEX IF NOT EXISTS idx_affiliate_category_country ON affiliate_links(category, country)`,
		`CREATE INDEX IF NOT EXISTS idx_affiliate_active ON affiliate_links(is_active)`,

		// ============================================================================
		// CONSTRAINTS & ALTER TABLES
		// ============================================================================
		
		// Ajouter colonnes users pour localisation (si elles n'existent pas)
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS country VARCHAR(2) DEFAULT 'FR'`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS postal_code VARCHAR(10)`,
		
		`ALTER TABLE bank_connections DROP CONSTRAINT IF EXISTS bank_connections_provider_connection_id_key`,
		`ALTER TABLE bank_connections DROP CONSTRAINT IF EXISTS unique_provider_connection_per_budget`,
		`ALTER TABLE bank_connections ADD CONSTRAINT unique_provider_connection_per_budget UNIQUE (provider_connection_id, budget_id)`,

		`ALTER TABLE bank_accounts DROP CONSTRAINT IF EXISTS unique_account_per_connection`,
		`ALTER TABLE bank_accounts ADD CONSTRAINT unique_account_per_connection UNIQUE (connection_id, external_account_id)`,

		`ALTER TABLE banking_connections DROP CONSTRAINT IF EXISTS unique_banking_connection_per_budget`,
		`ALTER TABLE banking_connections ADD CONSTRAINT unique_banking_connection_per_budget 
			UNIQUE (user_id, budget_id, aspsp_name, aspsp_country)`,

		`ALTER TABLE banking_accounts DROP CONSTRAINT IF EXISTS unique_banking_account_per_connection`,
		`ALTER TABLE banking_accounts ADD CONSTRAINT unique_banking_account_per_connection 
			UNIQUE (connection_id, account_id)`,

		// Market suggestions unique indexes
		`DROP INDEX IF EXISTS idx_unique_market_suggestion_null`,
		`DROP INDEX IF EXISTS idx_unique_market_suggestion_not_null`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_unique_market_suggestion_null
			ON market_suggestions (category, country)
			WHERE merchant_name IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_unique_market_suggestion_not_null
			ON market_suggestions (category, country, merchant_name)
			WHERE merchant_name IS NOT NULL`,

		// Affiliate links unique index
		`DROP INDEX IF EXISTS idx_unique_affiliate_link`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_unique_affiliate_link
			ON affiliate_links (category, country, provider_name)`,

		// ============================================================================
		// SEED DATA
		// ============================================================================
		`INSERT INTO affiliate_links (category, country, provider_name, affiliate_url, commission_rate, priority) 
		VALUES 
			('INTERNET', 'FR', 'Ariase', 'https://www.ariase.com/box', 5.00, 1),
			('MOBILE', 'FR', 'Ariase', 'https://www.ariase.com/mobile', 5.00, 1),
			('ENERGY', 'FR', 'Papernest', 'https://www.papernest.com/energie/', 8.00, 1),
			('LOAN', 'FR', 'Meilleurtaux', 'https://www.meilleurtaux.com/', 10.00, 1)
		ON CONFLICT DO NOTHING`,
	}

	successCount := 0
	errorCount := 0

	for i, migration := range migrations {
		if _, err := db.Exec(migration); err != nil {
			// Log errors but don't fail hard (some DROP CONSTRAINT may fail on fresh DB)
			fmt.Printf("⚠️  Migration %d warning: %v\n", i+1, err)
			errorCount++
		} else {
			successCount++
		}
	}

	elapsed := time.Since(start)
	fmt.Printf("✅ Migrations completed in %v\n", elapsed)
	fmt.Printf("   - Success: %d\n", successCount)
	if errorCount > 0 {
		fmt.Printf("   - Warnings: %d (expected for existing DBs)\n", errorCount)
	}

	return nil
}
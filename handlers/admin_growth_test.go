package handlers

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"github.com/LovationAdmin/budget-api/config"
)

// Runs against a real Postgres: TEST_DATABASE_URL (an empty database, the
// schema is created by the migrations).
func TestGrowthActiveUsersArePerPerson(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := config.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	mustExec(`TRUNCATE users CASCADE`)

	user := func(email string) string {
		id := uuid.New().String()
		mustExec(`INSERT INTO users (id, email, password_hash, name, email_verified) VALUES ($1, $2, 'x', 'Test', true)`, id, email)
		return id
	}
	seenDaysAgo := func(id string, days int) {
		mustExec(`INSERT INTO refresh_tokens (id, user_id, family_id, token_hash, issued_at, expires_at, revoked_at)
			VALUES ($1, $2, $1, $3, NOW() - make_interval(days => $4), NOW() + INTERVAL '30 days', NOW())`,
			uuid.New().String(), id, uuid.New().String(), days)
	}
	budget := func(owner string, others ...string) {
		id := uuid.New().String()
		mustExec(`INSERT INTO budgets (id, name, owner_id, last_viewed_at) VALUES ($1, 'B', $2, NOW())`, id, owner)
		for _, m := range append([]string{owner}, others...) {
			mustExec(`INSERT INTO budget_members (budget_id, user_id) VALUES ($1, $2)`, id, m)
		}
	}

	// A shared budget, opened yesterday by its owner only: the partner, last
	// seen 20 days ago, is active over 30 days but not over 7.
	owner, partner := user("owner@test.dev"), user("partner@test.dev")
	budget(owner, partner)
	seenDaysAgo(owner, 1) // a revoked (rotated) token still says they came
	seenDaysAgo(partner, 20)
	// Opened the app but never made a budget: not active.
	seenDaysAgo(user("nobudget@test.dev"), 1)
	// A budget, never seen in 30 days: not active.
	solo := user("solo@test.dev")
	budget(solo)
	seenDaysAgo(solo, 45)

	g, err := (&AdminStatsHandler{DB: db}).growth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if g.ActiveUsers7Days != 1 || g.ActiveUsers30Days != 2 || g.Funnel.Active30Days != 2 {
		t.Errorf("active 7/30 days = %d/%d (funnel %d), want 1/2 (2)", g.ActiveUsers7Days, g.ActiveUsers30Days, g.Funnel.Active30Days)
	}
	if g.Funnel.SignedUp != 4 || g.Funnel.WithBudget != 3 || g.Funnel.Collaborating != 2 {
		t.Errorf("funnel = %+v, want 4 signed up, 3 with a budget, 2 collaborating", g.Funnel)
	}
}

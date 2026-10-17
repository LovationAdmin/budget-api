package services

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/LovationAdmin/budget-api/config"
	"github.com/google/uuid"
)

type fakeExpiryMailer struct {
	sent map[string]time.Time // email → deletion date announced
	fail map[string]bool
}

func (m *fakeExpiryMailer) SendUnverifiedExpiryEmail(toEmail, _, token, _ string, deleteFrom time.Time) error {
	if m.fail[toEmail] {
		return context.DeadlineExceeded
	}
	if token == "" {
		panic("no token")
	}
	m.sent[toEmail] = deleteFrom
	return nil
}

// Runs against a real Postgres: TEST_DATABASE_URL (an empty database, the
// schema is created by the migrations).
func TestUnverifiedCleanup(t *testing.T) {
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

	user := func(email string, verified bool, ageDays int) string {
		id := uuid.New().String()
		mustExec(`INSERT INTO users (id, email, password_hash, name, email_verified, created_at)
			VALUES ($1, $2, 'x', 'Test', $3, NOW() - make_interval(days => $4))`, id, email, verified, ageDays)
		return id
	}
	warnedDaysAgo := func(id string, days int) {
		mustExec(`INSERT INTO email_campaign_sends (campaign_id, user_id, status, created_at)
			VALUES ($1, $2, 'sent', NOW() - make_interval(days => $3))`, UnverifiedWarningCampaign, id, days)
	}
	budget := func(owner string, others ...string) string {
		id := uuid.New().String()
		mustExec(`INSERT INTO budgets (id, name, owner_id) VALUES ($1, 'B', $2)`, id, owner)
		for _, m := range append([]string{owner}, others...) {
			mustExec(`INSERT INTO budget_members (budget_id, user_id) VALUES ($1, $2)`, id, m)
		}
		mustExec(`INSERT INTO budget_data (budget_id, data, updated_by) VALUES ($1, '{}', $2)`, id, owner)
		return id
	}

	fresh := user("fresh@x.test", false, 10)       // too young: nothing
	due := user("due@x.test", false, 25)           // 23+ days: warned, kept
	old := user("old@x.test", false, 40)           // old, never warned: warned, kept
	expired := user("expired@x.test", false, 40)   // warned 8 days ago: deleted
	recent := user("recent@x.test", false, 40)     // warned 3 days ago: kept
	verified := user("verified@x.test", true, 400) // confirmed: untouched
	solo := user("solo@x.test", false, 300)        // own budget, nobody else: deleted with it
	sharer := user("sharer@x.test", false, 300)    // budget shared with `verified`: never
	failing := user("failing@x.test", false, 40)   // email fails: not marked as warned
	warnedDaysAgo(expired, 8)
	warnedDaysAgo(recent, 3)
	warnedDaysAgo(solo, 8)
	soloBudget := budget(solo)
	// The references to users without ON DELETE rule must not block the delete.
	mustExec(`INSERT INTO audit_logs (budget_id, user_id, action) VALUES ($1, $2, 'update')`, soloBudget, solo)
	mustExec(`INSERT INTO invitations (budget_id, email, invited_by, token, expires_at)
		VALUES ($1, 'friend@x.test', $2, $3, NOW() + INTERVAL '7 days')`, soloBudget, solo, uuid.New().String())
	budget(sharer, verified)

	mailer := &fakeExpiryMailer{sent: map[string]time.Time{}, fail: map[string]bool{"failing@x.test": true}}
	cleanup := &UnverifiedCleanup{DB: db, Mailer: mailer}
	res, err := cleanup.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if res.Warned != 2 || res.WarnFailed != 1 || res.Deleted != 2 || res.DeleteFailed != 0 {
		t.Errorf("result = %+v", res)
	}
	for _, email := range []string{"due@x.test", "old@x.test"} {
		d, ok := mailer.sent[email]
		if !ok {
			t.Errorf("%s not warned", email)
		} else if days := time.Until(d).Hours() / 24; days < 6.9 || days > 7.1 {
			t.Errorf("%s: deletion announced in %.1f days", email, days)
		}
	}
	if len(mailer.sent) != 2 {
		t.Errorf("warned: %v", mailer.sent)
	}

	exists := func(q string, args ...any) bool {
		var n int
		if err := db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n > 0
	}
	for id, want := range map[string]bool{fresh: true, due: true, old: true, expired: false, recent: true, verified: true, solo: false, sharer: true, failing: true} {
		if got := exists(`SELECT COUNT(*) FROM users WHERE id = $1`, id); got != want {
			t.Errorf("user %s exists = %v, want %v", id, got, want)
		}
	}
	if exists(`SELECT COUNT(*) FROM budgets WHERE id = $1`, soloBudget) {
		t.Error("the deleted account's own budget is still there")
	}
	// The warning carries a confirmation link that lasts until the deletion.
	if !exists(`SELECT COUNT(*) FROM email_verification_tokens WHERE user_id = $1 AND expires_at > NOW() + INTERVAL '6 days'`, old) {
		t.Error("no week-long confirmation token for the warned account")
	}
	if exists(`SELECT COUNT(*) FROM email_campaign_sends WHERE user_id = $1 AND status = 'sent'`, failing) {
		t.Error("a failed warning counts as sent")
	}

	// Next day: nobody is warned twice, and the failed one is retried.
	mailer.sent = map[string]time.Time{}
	delete(mailer.fail, "failing@x.test")
	res, err = cleanup.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Warned != 1 || res.Deleted != 0 || len(mailer.sent) != 1 {
		t.Errorf("second run = %+v, warned %v", res, mailer.sent)
	}
	if _, ok := mailer.sent["failing@x.test"]; !ok {
		t.Error("the failed warning was not retried")
	}

	// Confirming the address in the meantime keeps the account.
	mustExec(`UPDATE users SET email_verified = true WHERE id = $1`, recent)
	mustExec(`UPDATE email_campaign_sends SET created_at = NOW() - INTERVAL '8 days' WHERE user_id = $1`, recent)
	if res, err = cleanup.Run(ctx); err != nil || res.Deleted != 0 {
		t.Errorf("confirmed account: %+v %v", res, err)
	}
}

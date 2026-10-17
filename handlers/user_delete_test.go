package handlers

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/LovationAdmin/budget-api/config"
	"github.com/LovationAdmin/budget-api/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Deleting one's account from the profile, against a real Postgres:
// TEST_DATABASE_URL (an empty database, the schema comes from the migrations).
func TestDeleteAccount(t *testing.T) {
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
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	count := func(q string, args ...any) int {
		t.Helper()
		var n int
		if err := db.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	mustExec(`TRUNCATE users CASCADE`)

	hash, err := utils.HashPassword("Correct-horse-42")
	if err != nil {
		t.Fatal(err)
	}
	user := func(email string) string {
		id := uuid.New().String()
		mustExec(`INSERT INTO users (id, email, password_hash, name, email_verified) VALUES ($1, $2, $3, 'Test', true)`, id, email, hash)
		return id
	}
	budget := func(owner, editor string, others ...string) string {
		id := uuid.New().String()
		mustExec(`INSERT INTO budgets (id, name, owner_id) VALUES ($1, 'B', $2)`, id, owner)
		for _, m := range append([]string{owner}, others...) {
			mustExec(`INSERT INTO budget_members (budget_id, user_id) VALUES ($1, $2)`, id, m)
		}
		mustExec(`INSERT INTO budget_data (budget_id, data, updated_by) VALUES ($1, '{}', $2)`, id, editor)
		mustExec(`INSERT INTO audit_logs (budget_id, user_id, action) VALUES ($1, $2, 'update')`, id, editor)
		mustExec(`INSERT INTO invitations (budget_id, email, invited_by, token, expires_at)
			VALUES ($1, 'friend@x.test', $2, $3, NOW() + INTERVAL '7 days')`, id, editor, uuid.New().String())
		return id
	}
	alex := user("alex@x.test")
	sam := user("sam@x.test")
	own := budget(alex, alex)         // Alex's own budget
	shared := budget(sam, alex, alex) // Sam's budget, last edited by Alex

	h := &UserHandler{DB: db}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.DELETE("/user/account", func(c *gin.Context) { c.Set("user_id", alex); h.DeleteAccount(c) })
	del := func(password string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodDelete, "/user/account", strings.NewReader(`{"password":"`+password+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept-Language", "fr-FR")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	// A typo is not an expired session: 403, so the app doesn't sign out.
	if w := del("wrong"); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "Mot de passe incorrect.") {
		t.Fatalf("wrong password: %d %s", w.Code, w.Body.String())
	}
	if count(`SELECT COUNT(*) FROM users WHERE id = $1`, alex) != 1 {
		t.Fatal("account deleted with a wrong password")
	}

	if w := del("Correct-horse-42"); w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	if count(`SELECT COUNT(*) FROM users WHERE id = $1`, alex) != 0 {
		t.Error("account still there")
	}
	if count(`SELECT COUNT(*) FROM budgets WHERE id = $1`, own) != 0 {
		t.Error("Alex's own budget still there")
	}
	// Sam keeps the shared budget, its data and history, detached from Alex.
	if count(`SELECT COUNT(*) FROM budget_data WHERE budget_id = $1 AND updated_by IS NULL`, shared) != 1 ||
		count(`SELECT COUNT(*) FROM audit_logs WHERE budget_id = $1 AND user_id IS NULL`, shared) != 1 ||
		count(`SELECT COUNT(*) FROM invitations WHERE budget_id = $1 AND invited_by IS NULL`, shared) != 1 {
		t.Error("the shared budget lost its data or still points to Alex")
	}
	if count(`SELECT COUNT(*) FROM budget_members WHERE budget_id = $1`, shared) != 1 {
		t.Error("Alex is still a member of Sam's budget")
	}
}

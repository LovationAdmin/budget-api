package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/LovationAdmin/budget-api/config"
	"github.com/LovationAdmin/budget-api/services"
	"github.com/LovationAdmin/budget-api/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// « Who I am in this budget », against a real Postgres: TEST_DATABASE_URL (an
// empty database, the schema comes from the migrations). One account per
// person: nobody can take a person already linked to someone else, nor link
// another member's account.
func TestSetMemberPerson(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	t.Setenv("DATA_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
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
	mustExec(`TRUNCATE users CASCADE`)

	user := func(email string) string {
		id := uuid.New().String()
		mustExec(`INSERT INTO users (id, email, password_hash, name, email_verified) VALUES ($1, $2, 'x', $2, true)`, id, email)
		return id
	}
	alex := user("alex@x.test") // owner
	sam := user("sam@x.test")
	lou := user("lou@x.test")
	outsider := user("out@x.test")
	budgetID := uuid.New().String()
	mustExec(`INSERT INTO budgets (id, name, owner_id) VALUES ($1, 'Foyer', $2)`, budgetID, alex)
	for _, m := range []string{alex, sam, lou} {
		mustExec(`INSERT INTO budget_members (budget_id, user_id) VALUES ($1, $2)`, budgetID, m)
	}
	// Budget data is stored encrypted, as the app saves it.
	sealed, err := utils.Encrypt([]byte(`{"people":[{"id":"pa","name":"Alex"},{"id":"ps","name":"Sam"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	wrapper, _ := json.Marshal(services.EncryptedData{Encrypted: sealed})
	mustExec(`INSERT INTO budget_data (budget_id, data, updated_by) VALUES ($1, $2, $3)`, budgetID, string(wrapper), alex)

	h := NewHandler(services.NewBudgetService(db, nil, nil), nil)
	gin.SetMode(gin.TestMode)
	put := func(caller, member, body string) int {
		t.Helper()
		r := gin.New()
		r.PUT("/budgets/:id/members/:member_id/person", func(c *gin.Context) { c.Set("user_id", caller); h.SetMemberPerson(c) })
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/budgets/"+budgetID+"/members/"+member+"/person", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w.Code
	}
	linkOf := func(member string) string {
		t.Helper()
		var p sql.NullString
		if err := db.QueryRow(`SELECT person_id FROM budget_members WHERE budget_id = $1 AND user_id = $2`, budgetID, member).Scan(&p); err != nil {
			t.Fatal(err)
		}
		if !p.Valid {
			return "<null>"
		}
		return p.String
	}
	expect := func(what string, got, want int) {
		t.Helper()
		if got != want {
			t.Errorf("%s: status %d, want %d", what, got, want)
		}
	}

	expect("Sam links himself to Sam", put(sam, sam, `{"person_id":"ps"}`), http.StatusOK)
	expect("Alex can't take Sam's person", put(alex, alex, `{"person_id":"ps"}`), http.StatusConflict)
	expect("Lou can't take Sam's person either", put(lou, lou, `{"person_id":"ps"}`), http.StatusConflict)
	expect("unknown person", put(alex, alex, `{"person_id":"nobody"}`), http.StatusBadRequest)
	expect("Alex links himself to Alex", put(alex, alex, `{"person_id":"pa"}`), http.StatusOK)
	expect("Lou: not in the household", put(lou, lou, `{"person_id":""}`), http.StatusOK)
	expect("Sam can't link Lou's account", put(sam, lou, `{"person_id":"ps"}`), http.StatusForbidden)
	expect("Sam can't clear Alex's link", put(sam, alex, `{"person_id":null}`), http.StatusForbidden)
	expect("the owner can't set someone else's link", put(alex, lou, `{"person_id":"pa"}`), http.StatusForbidden)
	expect("not a member of the budget", put(outsider, outsider, `{"person_id":"pa"}`), http.StatusNotFound)
	if got := linkOf(sam); got != "ps" {
		t.Errorf("Sam's link: %q, want ps", got)
	}
	if got := linkOf(lou); got != "" {
		t.Errorf("Lou's link: %q, want \"\" (not in the household)", got)
	}

	// The members list says who is who, so the app greys out taken persons.
	members, err := services.NewBudgetService(db, nil, nil).GetMembers(t.Context(), budgetID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range members {
		if m.UserID == sam && (m.PersonID == nil || *m.PersonID != "ps") {
			t.Errorf("members list: Sam's person_id %v, want ps", m.PersonID)
		}
	}

	// The owner clears a mistaken link: the person is free again.
	expect("owner clears Sam's link", put(alex, sam, `{"person_id":null}`), http.StatusOK)
	if got := linkOf(sam); got != "<null>" {
		t.Errorf("Sam's link after clearing: %q, want null", got)
	}
	expect("Lou can now be Sam's former person", put(lou, lou, `{"person_id":"ps"}`), http.StatusOK)
	expect("Sam forgets his own link (already none)", put(sam, sam, `{}`), http.StatusOK)

	// Leaving the budget frees the person.
	mustExec(`DELETE FROM budget_members WHERE budget_id = $1 AND user_id = $2`, budgetID, lou)
	expect("Sam takes the person Lou left", put(sam, sam, `{"person_id":"ps"}`), http.StatusOK)
}

package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LovationAdmin/budget-api/config"
	"github.com/LovationAdmin/budget-api/services"
	"github.com/LovationAdmin/budget-api/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Leaving the monthly recap from its link (one click from the mailbox, or the
// app's page), coming back, the account page — and the recap run skipping
// whoever left. Against a real Postgres: TEST_DATABASE_URL.
func TestEmailPreferences(t *testing.T) {
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
	flag := func(id, column string) bool {
		t.Helper()
		var v bool
		if err := db.QueryRow(`SELECT `+column+` FROM users WHERE id = $1`, id).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	mustExec(`TRUNCATE users CASCADE`)
	user := func(email string) string {
		id := uuid.New().String()
		mustExec(`INSERT INTO users (id, email, password_hash, name, email_verified) VALUES ($1, $2, 'x', 'Test', true)`, id, email)
		budget := uuid.New().String()
		mustExec(`INSERT INTO budgets (id, name, owner_id) VALUES ($1, 'Famille', $2)`, budget, id)
		mustExec(`INSERT INTO budget_members (budget_id, user_id) VALUES ($1, $2)`, budget, id)
		mustExec(`INSERT INTO budget_data (budget_id, data, updated_by) VALUES ($1, $2, $3)`, budget,
			`{"people":[{"id":"a","name":"Awa","salary":3000}],"charges":[{"id":"r","label":"Loyer","amount":1000}]}`, id)
		return id
	}
	alex, sam := user("alex@x.test"), user("sam@x.test")

	h := &EmailPreferencesHandler{DB: db}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/email/unsubscribe", h.Unsubscribe)
	r.GET("/email/unsubscribe", h.OpenUnsubscribe)
	r.POST("/email/resubscribe", h.Resubscribe)
	r.GET("/email/preferences", h.Get)
	r.PUT("/user/email-preferences", func(c *gin.Context) { c.Set("user_id", sam); h.UpdateMine(c) })
	do := func(method, target, contentType, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	token := utils.UnsubscribeToken(alex, utils.EmailListMonthlyRecap)
	// A forged or broken link changes nothing.
	if w := do(http.MethodPost, "/email/unsubscribe?token="+token+"x", "", ""); w.Code != http.StatusBadRequest || !flag(alex, "email_monthly_recap") {
		t.Fatalf("forged token: %d", w.Code)
	}
	// A link scanner following the link (GET) changes nothing either: it lands on the page.
	if w := do(http.MethodGet, "/email/unsubscribe?token="+token, "", ""); w.Code != http.StatusFound ||
		!strings.Contains(w.Header().Get("Location"), "/emails/desabonnement?token="+token) || !flag(alex, "email_monthly_recap") {
		t.Fatalf("GET: %d %s", w.Code, w.Header().Get("Location"))
	}
	// RFC 8058 one click, as Gmail sends it.
	w := do(http.MethodPost, "/email/unsubscribe?token="+token, "application/x-www-form-urlencoded", "List-Unsubscribe=One-Click")
	if w.Code != http.StatusOK || flag(alex, "email_monthly_recap") || !flag(alex, "email_news") {
		t.Fatalf("one click: %d %s", w.Code, w.Body.String())
	}
	var state emailListState
	w = do(http.MethodGet, "/email/preferences?token="+token, "", "")
	if json.Unmarshal(w.Body.Bytes(), &state) != nil || state.Subscribed || state.List != "monthly_recap" || state.Email != "al**@x.test" {
		t.Errorf("preferences: %s", w.Body.String())
	}

	// The recap run skips Alex now.
	recap := NewAdminMonthlyRecapHandler(db, services.NewEmailService(), services.NewMonthlyRecapService(db, services.NewBudgetService(db, nil, nil)))
	res := recap.RunMonthlyRecap(context.Background(), MonthlyRecapRunOptions{CampaignID: "test", DryRun: true, Month: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)})
	if res.Total != 1 || res.Sent != 1 || res.Failed != 0 || res.Aborted {
		t.Errorf("recap run: %+v", res)
	}

	// Changed their mind, from the page.
	if w := do(http.MethodPost, "/email/resubscribe", "application/json", `{"token":"`+token+`"}`); w.Code != http.StatusOK || !flag(alex, "email_monthly_recap") {
		t.Fatalf("resubscribe: %d %s", w.Code, w.Body.String())
	}
	// From the account page: only what is sent changes.
	if w := do(http.MethodPut, "/user/email-preferences", "application/json", `{"news":false}`); w.Code != http.StatusOK ||
		flag(sam, "email_news") || !flag(sam, "email_monthly_recap") {
		t.Fatalf("account page: %d %s", w.Code, w.Body.String())
	}
}

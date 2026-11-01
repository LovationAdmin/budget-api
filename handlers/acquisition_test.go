package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"

	"github.com/LovationAdmin/budget-api/config"
)

func TestNormalizeAcquisition(t *testing.T) {
	for in, want := range map[string]string{"bing": "bing", " Google ": "google", "chatgpt": "chatgpt", "evil<script>": "other", "": "other"} {
		if got := normalizeSource(in); got != want {
			t.Errorf("normalizeSource(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"/tableau-budget-familial-gratuit?utm_source=x": "/tableau-budget-familial-gratuit",
		"/blog/x#faq":            "/blog/x",
		"https://evil.example/x": "",
		"//evil.example":         "",
		"":                       "",
	} {
		if got := normalizeLanding(in); got != want {
			t.Errorf("normalizeLanding(%q) = %q, want %q", in, got, want)
		}
	}
	if got := normalizeLanding("/" + strings.Repeat("a", 300)); len(got) != 120 {
		t.Errorf("landing not bounded: %d chars", len(got))
	}
}

func trackDownload(h *AcquisitionHandler, body string) int {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	// sendBeacon posts text/plain: the body must still be read as JSON.
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/track/download", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	h.TrackDownload(c)
	// A bodyless status is flushed by the server after the handlers; read it from gin.
	return c.Writer.Status()
}

func TestTrackDownloadRejectsUnknownFiles(t *testing.T) {
	h := &AcquisitionHandler{} // no database: rejected before any write
	for _, body := range []string{`{"file":"../../etc/passwd","source":"bing"}`, `{"file":"other.pdf"}`, `not json`} {
		if code := trackDownload(h, body); code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", body, code)
		}
	}
}

// Runs against a real Postgres: TEST_DATABASE_URL (an empty database, the
// schema is created by the migrations).
func TestAcquisitionCountsBySource(t *testing.T) {
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
	if _, err := db.ExecContext(ctx, `TRUNCATE acquisition_events`); err != nil {
		t.Fatal(err)
	}

	h := &AcquisitionHandler{DB: db}
	for _, body := range []string{
		`{"file":"tableau-budget-familial-a-imprimer.pdf","source":"bing"}`,
		`{"file":"tableau-budget-familial-budgetfamille.xlsx","source":"bing"}`,
		`{"file":"tableau-budget-familial-budgetfamille.xlsx","source":"bing"}`,
		`{"file":"budget-template-printable.pdf","source":"google"}`,
		`{"file":"budget-template.xlsx","source":"made-up"}`,
	} {
		if code := trackDownload(h, body); code != http.StatusNoContent {
			t.Fatalf("%s: status %d, want 204", body, code)
		}
	}
	recordAcquisition(ctx, db, "signup", "bing", normalizeLanding("/tableau-budget-familial-gratuit?utm_source=x"))
	recordAcquisition(ctx, db, "signup", "bing", "/tableau-budget-familial-gratuit")
	recordAcquisition(ctx, db, "signup", "google", "/calcul-reste-a-vivre")
	// Older than 90 days: out of the counts, but it dates the start of tracking.
	if _, err := db.ExecContext(ctx, `INSERT INTO acquisition_events (kind, source, detail, created_at) VALUES ('signup', 'email', '/', NOW() - INTERVAL '120 days')`); err != nil {
		t.Fatal(err)
	}

	a, err := (&AdminStatsHandler{DB: db}).acquisition(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if a.Since == "" {
		t.Error("since is empty")
	}
	want := []AcquisitionRow{
		{Source: "bing", Downloads: 3, DownloadsPDF: 1, DownloadsExcel: 2, Signups: 2},
		{Source: "google", Downloads: 1, DownloadsPDF: 1, Signups: 1},
		{Source: "other", Downloads: 1, DownloadsExcel: 1},
	}
	if len(a.Sources) != len(want) {
		t.Fatalf("sources = %+v, want %+v", a.Sources, want)
	}
	for i := range want {
		if a.Sources[i] != want[i] {
			t.Errorf("sources[%d] = %+v, want %+v", i, a.Sources[i], want[i])
		}
	}
	if len(a.Landings) != 2 || a.Landings[0] != (LandingRow{Page: "/tableau-budget-familial-gratuit", Signups: 2}) {
		t.Errorf("landings = %+v", a.Landings)
	}
}

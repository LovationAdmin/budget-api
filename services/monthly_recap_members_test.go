package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LovationAdmin/budget-api/utils"
)

func loadDemoPayload(t *testing.T) *budgetPayload {
	t.Helper()
	raw, err := os.ReadFile("testdata/recap_demo.json")
	if err != nil {
		t.Fatal(err)
	}
	var blob map[string]interface{}
	if err := json.Unmarshal(raw, &blob); err != nil {
		t.Fatal(err)
	}
	p, err := decodeBudgetPayload(blob)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Pocket money is salary − contribution; personal charges are part of it and
// never reach the pot totals.
func TestRecapMembers_PocketMoneyAndPersonalCharges(t *testing.T) {
	p := loadDemoPayload(t)
	members := resolveMembers(p, 2026, 9) // October 2026
	if len(members) != 2 {
		t.Fatalf("want 2 members, got %d", len(members))
	}
	byName := map[string]RecapMember{}
	for _, m := range members {
		byName[m.Name] = m
	}
	c, m := byName["Camille"], byName["Mehdi"]
	if c.Contribution != 1700 || c.PocketMoney != 1100 || c.PersonalCharges != 185 {
		t.Errorf("Camille = %+v", c)
	}
	if m.Contribution != 1150 || m.PocketMoney != 950 || m.PersonalCharges != 120 {
		t.Errorf("Mehdi = %+v", m)
	}
	v := resolveMonthValues(p, 2026, 9)
	if v.Charges != 2192.49 {
		t.Errorf("household charges = %v, personal charges must stay out of the pot", v.Charges)
	}
}

// A closed month reads members from its snapshot, personal charges included.
func TestRecapMembers_ClosedMonthSnapshot(t *testing.T) {
	var p budgetPayload
	if err := json.Unmarshal([]byte(`{
		"people": [{"id": "a", "name": "Awa", "salary": 9999}],
		"yearlyData": {"2026": {
			"lockedMonths": {"Septembre": true},
			"snapshots": [null,null,null,null,null,null,null,null,{
				"v": 1,
				"people": [{"id": "a", "name": "Awa", "salary": 3000, "contribution": 1800}],
				"charges": [], "projects": [], "oneOffs": [],
				"personal": [{"id": "x", "ownerId": "a", "amount": 200, "label": "Charge privée"}]
			}]
		}}
	}`), &p); err != nil {
		t.Fatal(err)
	}
	got := resolveMembers(&p, 2026, 8)
	if len(got) != 1 || got[0].Name != "Awa" || got[0].Contribution != 1800 || got[0].PocketMoney != 1200 || got[0].PersonalCharges != 200 {
		t.Errorf("closed month members = %+v", got)
	}
}

// Renders the recap of November from the demo household (the yearly
// property tax falls due); EMAIL_PREVIEW_DIR keeps the HTML and its text.
func TestRecapRender_DemoHousehold(t *testing.T) {
	raw, err := os.ReadFile("testdata/recap_demo.json")
	if err != nil {
		t.Fatal(err)
	}
	var blob map[string]interface{}
	if err := json.Unmarshal(raw, &blob); err != nil {
		t.Fatal(err)
	}
	blob["splitRules"] = []interface{}{map[string]interface{}{"from": "2026-09", "method": "prorata", "margin": 5, "basis": "month"}}
	for _, loc := range []string{"fr", "en"} {
		data, err := PreviewRecap(blob, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), loc, "Camille", "https://budgetfamille.com", "b1")
		if err != nil {
			t.Fatal(err)
		}
		data.UnsubscribeURL = "https://budgetfamille.com/emails/desabonnement?token=x"
		subject, html, err := utils.RenderMonthlyRecapEmail(loc, data.Month.ShortLabel, data.BudgetName, data)
		if err != nil {
			t.Fatal(err)
		}
		wants := map[string][]string{
			"fr": {"Novembre est prêt.", "Du 1ᵉʳ au 30 novembre", "Les virements du mois", "dont 185 € de charges perso",
				"Au prorata des salaires · marge 5 %", "Recalculé tout seul", "Ce qui change en novembre", "Taxe foncière", "Échéance annuelle",
				"Où va l’argent", "Vacances été 2027", "Votre épargne générale", "Fin octobre", "Ouvrir novembre dans l’app",
				"complete/month?m=2026-11&amp;utm_source=email", "complete/members?utm_source=email", "#repartition",
				"Ne plus recevoir le récap mensuel", "emails/desabonnement?token=x"},
			"en": {"November is ready.", "November 1 – 30", "This month’s transfers", "incl. 185 € personal bills",
				"In proportion to pay · 5% margin", "What changes in November", "Yearly bill due", "Where the money goes",
				"Your general savings", "End of October", "Open November in the app", "Stop the monthly recap"},
		}[loc]
		for _, w := range wants {
			if !strings.Contains(html, w) {
				t.Errorf("%s: html missing %q", loc, w)
			}
		}
		if loc == "fr" && subject != "Votre mois de novembre est prêt · Famille Martin" {
			t.Errorf("fr subject %q", subject)
		}
		if loc == "en" && subject != "Your November is ready · Famille Martin" {
			t.Errorf("en subject %q", subject)
		}
		if strings.Contains(html, "Envoi famille") || strings.Contains(html, "Salle de sport") || strings.Contains(html, "Charge privée") {
			t.Error("personal charge labels must never appear in the recap")
		}
		if len(html) > 90*1024 {
			t.Errorf("%s: %d bytes, Gmail clips past 102 KB", loc, len(html))
		}
		text := utils.PlainTextOf(html)
		if strings.Contains(text, "{") || strings.Contains(text, "&#") || !strings.Contains(text, data.Month.ShortLabel) {
			t.Errorf("%s: text part is not clean text:\n%s", loc, text)
		}
		if dir := os.Getenv("EMAIL_PREVIEW_DIR"); dir != "" {
			_ = os.WriteFile(filepath.Join(dir, "recap_"+loc+".html"), []byte(html), 0o644)
			_ = os.WriteFile(filepath.Join(dir, "recap_"+loc+".txt"), []byte(text), 0o644)
		}
	}
}

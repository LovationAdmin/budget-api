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

// Renders the recap from the demo household; EMAIL_PREVIEW_DIR keeps the HTML.
func TestRecapRender_DemoHousehold(t *testing.T) {
	p := loadDemoPayload(t)
	now := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	month := func(at time.Time) RecapMonth {
		m := aggregateMonth(p, at, "fr", false)
		m.Members = resolveMembers(p, m.Year, m.MonthIdx)
		m.URL = "https://budgetfamille.com/budget/b1/complete/month?m=" + makeYM(m.Year, m.MonthIdx)
		return m
	}
	data := &RecapData{
		UserName: "Camille", BudgetName: "Famille Martin", Currency: "EUR", CurrencySymbol: "€", Locale: "fr", Year: 2026,
		AppURL: "https://budgetfamille.com", CampaignID: "monthly_recap_2026_10",
		PreviousMonth: month(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)),
		CurrentMonth:  month(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)),
		NextMonth:     month(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)),
		YearIncome:    27500, YearExpenses: 24100, YearSavings: 3400,
		UsesContributions: usesContributions(p),
		BudgetURL:         "https://budgetfamille.com/budget/b1/complete/month",
		Tip:               recapTip("fr", now, "https://budgetfamille.com"),
		Projects: []RecapProject{
			{Name: "Vacances été 2027", TargetAmount: 3000, AllocatedYTD: 500, Progress: 16.7, Status: "on_track", HasTarget: true},
			{Name: "Fonds d'urgence", TargetAmount: 5000, AllocatedYTD: 1500, Progress: 30, Status: "ahead", HasTarget: true},
		},
	}
	for _, loc := range []string{"fr", "en"} {
		subject, html, err := utils.RenderMonthlyRecapEmail(loc, data.PreviousMonth.Label, data.BudgetName, data)
		if err != nil {
			t.Fatal(err)
		}
		if loc == "fr" && (subject != "Votre bilan d’octobre 2026 · Famille Martin" || !strings.Contains(html, "dont 185 € de charges perso")) {
			t.Errorf("fr subject %q or members block missing", subject)
		}
		if strings.Contains(html, "Envoi famille") || strings.Contains(html, "Charge privée") {
			t.Error("personal charge labels must never appear in the recap")
		}
		if dir := os.Getenv("EMAIL_PREVIEW_DIR"); dir != "" {
			_ = os.WriteFile(filepath.Join(dir, "recap_"+loc+".html"), []byte(html), 0o644)
		}
	}
}

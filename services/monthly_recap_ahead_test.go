package services

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LovationAdmin/budget-api/utils"
)

// What moves from October to November: a new bill, a yearly bill due, a loan
// that ends, a raise of a bill, a saving that starts, a member paying more
// and a one-off income; biggest first.
func TestRecapChanges_Kinds(t *testing.T) {
	p := decodeJSON(t, `{
		"people":[
			{"id":"a","name":"Awa","salary":3000,"contributions":[{"from":"2026-01","mode":"fixed","value":1500},{"from":"2026-11","mode":"fixed","value":1600}]},
			{"id":"b","name":"Bo","salary":2000,"contributions":[{"from":"2026-01","mode":"fixed","value":1000}]}
		],
		"charges":[
			{"id":"rent","label":"Loyer","amount":1000,"amountHistory":[{"from":"2026-01","amount":1000},{"from":"2026-11","amount":1040}],"category":"housing"},
			{"id":"gym","label":"Sport","amount":30,"startDate":"2026-11-01"},
			{"id":"tax","label":"Taxe foncière","amount":900,"frequency":"yearly","startDate":"2025-11-01"},
			{"id":"car","label":"Crédit auto","amount":250,"endDate":"2026-10-31"},
			{"id":"canteen","label":"Cantine","amount":90,"frequency":"custom","months":[10,12]}
		],
		"projects":[{"id":"trip","label":"Voyage","monthlyAmount":200,"startDate":"2026-11-01"}],
		"oneTimeIncomes":{"2026":[{},{},{},{},{},{},{},{},{},{},{"amount":300,"items":[{"id":"x","label":"Prime","amount":300}]}]}
	}`)
	got := recapChanges(p, month(2026, 10), month(2026, 11), "fr")
	type c struct {
		kind  string
		delta float64
		good  bool
	}
	// Each amount is the effect on what is left of the month: + green, − red.
	want := map[string]c{
		"Taxe foncière": {"due", -900, false}, "Prime": {"oneoff", 300, true}, "Crédit auto": {"end", 250, true},
		"Voyage": {"saving_new", -200, false}, "Awa": {"pay_up", 100, true}, "Cantine": {"pause", 90, true},
	}
	if len(got) != recapMaxChanges {
		t.Fatalf("want the %d biggest changes, got %+v", recapMaxChanges, got)
	}
	for _, x := range got {
		w, ok := want[x.Label]
		if !ok {
			t.Errorf("unexpected change %+v (the smaller ones are left out)", x)
			continue
		}
		if x.Kind != w.kind || x.Delta != w.delta || x.Good != w.good {
			t.Errorf("%s: got %+v, want %+v", x.Label, x, w)
		}
	}
	if got[0].Label != "Taxe foncière" {
		t.Errorf("biggest first, got %s", got[0].Label)
	}
	for _, x := range got {
		if x.Good != (x.Delta > 0) {
			t.Errorf("%s: the colour follows the sign, got %+v", x.Label, x)
		}
	}
	// The two left out: the rent going up 40, the new 30 gym.
	p.OneTimeIncomes = nil
	p.Projects = nil
	rest := recapChanges(p, month(2026, 10), month(2026, 11), "fr")
	kinds := map[string]string{}
	for _, x := range rest {
		kinds[x.Label] = x.Kind
	}
	if kinds["Loyer"] != "up" || kinds["Sport"] != "new" {
		t.Errorf("rent up and new gym expected, got %v", kinds)
	}
}

func TestRecapCharges_HeaviestFirstWithNotes(t *testing.T) {
	p := decodeJSON(t, `{"charges":[
		{"id":"a","label":"A","amount":10},{"id":"b","label":"B","amount":800},{"id":"c","label":"C","amount":1200,"frequency":"yearly","smooth":true},
		{"id":"d","label":"D","amount":40,"overrides":{"2026-11":60}},{"id":"e","label":"Charge privée","amount":30},
		{"id":"f","label":"F","amount":20},{"id":"g","label":"G","amount":5},{"id":"h","label":"H","amount":0}
	]}`)
	list, more, moreAmount := recapCharges(p, 2026, 10)
	if len(list) != recapMaxCharges || more != 1 || moreAmount != 5 {
		t.Fatalf("got %d listed, %d more (%v)", len(list), more, moreAmount)
	}
	if list[0].Label != "B" || list[0].Bar != 100 || list[1].Label != "C" || list[1].Amount != 100 || list[1].Note != "smoothed" {
		t.Errorf("order or smoothing wrong: %+v", list[:2])
	}
	for _, c := range list {
		if c.Label == "D" && (c.Amount != 60 || c.Note != "adjusted") {
			t.Errorf("month-only amount: %+v", c)
		}
		if c.Label == "Charge privée" && !c.Private {
			t.Error("a private charge keeps its name hidden")
		}
	}
}

func TestSplitMethodAt(t *testing.T) {
	p := decodeJSON(t, `{"splitRules":[
		{"from":"2026-09","method":"reste","margin":5,"basis":"month"},
		{"from":"2027-02","method":null}
	]}`)
	if m := splitMethodAt(p, "2026-08", "fr"); m != nil {
		t.Errorf("no method before September, got %+v", m)
	}
	if m := splitMethodAt(p, "2026-11", "fr"); m == nil || m.Method != "reste" || m.Margin != 5 || m.Since != "Septembre 2026" {
		t.Errorf("November follows the method: %+v", m)
	}
	if m := splitMethodAt(p, "2027-03", "fr"); m != nil {
		t.Errorf("stopped by hand in February, got %+v", m)
	}
	// Malformed rules never fail the recap.
	if bad := decodeJSON(t, `{"splitRules":{"oops":1}}`); splitMethodAt(bad, "2026-11", "fr") != nil {
		t.Error("malformed rules are ignored")
	}
}

func TestRecapSchedule(t *testing.T) {
	paris, _ := time.LoadLocation("Europe/Paris")
	at := func(m time.Month, d, h int) time.Time { return time.Date(2026, m, d, h, 0, 0, 0, paris) }

	if got := NextRecapRun(at(10, 29, 12), 27, paris); !got.Equal(at(11, 27, 9)) {
		t.Errorf("after the 27th: next run %v", got)
	}
	if got := NextRecapRun(at(10, 27, 8), 27, paris); !got.Equal(at(10, 27, 9)) {
		t.Errorf("the 27th before 9: next run %v", got)
	}
	if got := NextRecapRun(time.Date(2026, 12, 28, 0, 0, 0, 0, paris), 27, paris); !got.Equal(time.Date(2027, 1, 27, 9, 0, 0, 0, paris)) {
		t.Errorf("year change: %v", got)
	}
	if _, open := RecapWindow(at(10, 27, 8), 27, paris); open {
		t.Error("closed before 9 on the 27th")
	}
	target, open := RecapWindow(at(10, 29, 12), 27, paris)
	if !open || target.Month() != time.November || RecapCampaignID(target) != "monthly_recap_2026_11" {
		t.Errorf("the 29th: window open for November, got %v %v", open, target)
	}
	if _, open := RecapWindow(at(11, 2, 12), 27, paris); open {
		t.Error("closed once the month started")
	}

	os.Setenv("MONTHLY_RECAP_DAY", "25")
	defer os.Unsetenv("MONTHLY_RECAP_DAY")
	if RecapSendDay() != 25 {
		t.Error("MONTHLY_RECAP_DAY")
	}
	os.Setenv("MONTHLY_RECAP_DAY", "31")
	if RecapSendDay() != DefaultRecapSendDay {
		t.Error("days past the 28th do not exist every month")
	}
}

// A recap of a near-empty budget still renders, in both languages.
func TestRecapRender_EmptyBudget(t *testing.T) {
	for _, loc := range []string{"fr", "en"} {
		data, err := PreviewRecap(map[string]interface{}{}, month(2026, 11), loc, "Awa", "https://budgetfamille.com", "b")
		if err != nil {
			t.Fatal(err)
		}
		_, html, err := utils.RenderMonthlyRecapEmail(loc, data.Month.ShortLabel, data.BudgetName, data)
		if err != nil {
			t.Fatalf("%s: %v", loc, err)
		}
		if strings.Contains(html, "Les virements du mois") || strings.Contains(html, "−0") {
			t.Errorf("%s: nothing to transfer, nothing negative", loc)
		}
	}
}

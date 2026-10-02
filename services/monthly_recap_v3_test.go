package services

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LovationAdmin/budget-api/utils"
)

func decodeJSON(t *testing.T, raw string) *budgetPayload {
	t.Helper()
	var v interface{}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("bad fixture: %v", err)
	}
	p, err := decodeBudgetPayload(v)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return p
}

func month(y, m int) time.Time { return time.Date(y, time.Month(m), 1, 0, 0, 0, 0, time.UTC) }

func assertMoney(t *testing.T, what string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.005 {
		t.Errorf("%s: want %v, got %v", what, want, got)
	}
}

func TestV3_ContributionsAreThePotInflows(t *testing.T) {
	p := decodeJSON(t, `{
		"schemaVersion": 3,
		"people": [
			{"id":"a","name":"Alice","salary":3000,"contributions":[{"from":"2026-01","mode":"fixed","value":1800}]},
			{"id":"b","name":"Bob","salary":2000,"contributions":[{"from":"2026-01","mode":"percent","value":50}],
			 "contributionOverrides":{"2026-10":400}},
			{"id":"c","name":"Chloé","salary":1000}
		]
	}`)
	sep := aggregateMonth(p, month(2026, 9), "fr", false)
	assertMoney(t, "september inflows", sep.BaseIncome, 1800+1000+1000)
	oct := aggregateMonth(p, month(2026, 10), "fr", false)
	assertMoney(t, "october inflows (override)", oct.BaseIncome, 1800+400+1000)
	if !usesContributions(p) {
		t.Error("usesContributions should be true")
	}
	if usesContributions(decodeJSON(t, `{"people":[{"id":"a","salary":10,"contributions":[{"from":"2026-01","mode":"all"}]}]}`)) {
		t.Error("an 'all' rule is not a partial contribution")
	}
}

func TestV3_SalaryStepsAndOverrides(t *testing.T) {
	p := decodeJSON(t, `{
		"people": [{"id":"a","name":"A","salary":2200,
			"salaryHistory":[{"from":"2026-01","amount":2000},{"from":"2026-07","amount":2200}],
			"salaryOverrides":{"2026-12":3500},
			"contributions":[{"from":"2026-01","mode":"percent","value":50}]}]
	}`)
	assertMoney(t, "before first step uses earliest step", aggregateMonth(p, month(2025, 6), "fr", false).BaseIncome, 1000)
	assertMoney(t, "march", aggregateMonth(p, month(2026, 3), "fr", false).BaseIncome, 1000)
	assertMoney(t, "july", aggregateMonth(p, month(2026, 7), "fr", false).BaseIncome, 1100)
	assertMoney(t, "december bonus", aggregateMonth(p, month(2026, 12), "fr", false).BaseIncome, 1750)
}

func TestV3_ChargeFrequencies(t *testing.T) {
	p := decodeJSON(t, `{
		"charges": [
			{"id":"rent","label":"Loyer","amount":1000,
			 "amountHistory":[{"from":"2026-01","amount":950},{"from":"2026-09","amount":1000}],
			 "overrides":{"2026-11":0}},
			{"id":"school","label":"Cantine","amount":120,"frequency":"custom","months":[1,2,3,4,5,6,9,10,11,12]},
			{"id":"tax","label":"Taxe foncière","amount":1200,"frequency":"yearly","startDate":"2025-10-01"},
			{"id":"insurance","label":"Assurance","amount":600,"frequency":"yearly","smooth":true},
			{"id":"trip","label":"Voyage","amount":800,"frequency":"once","startDate":"2026-08-01"},
			{"id":"gym","label":"Salle","amount":40,
			 "amountHistory":[{"from":"2026-01","amount":40},{"from":"2026-05","amount":0},{"from":"2026-10","amount":45}]}
		]
	}`)
	cases := []struct {
		m    int
		want float64
	}{
		{3, 950 + 120 + 50 + 40},          // rent v1, school, smoothed insurance, gym
		{6, 950 + 120 + 50},               // gym paused (0 step = absent)
		{7, 950 + 50},                     // no school in July
		{8, 950 + 50 + 800},               // one-off trip
		{9, 1000 + 120 + 50},              // rent step
		{10, 1000 + 120 + 1200 + 50 + 45}, // yearly tax due (anchored on October), gym resumes
		{11, 0 + 120 + 50 + 45},           // rent skipped this month only
	}
	for _, c := range cases {
		got := aggregateMonth(p, month(2026, c.m), "fr", false).RecurringCharges
		assertMoney(t, fmt.Sprintf("charges 2026-%02d", c.m), got, c.want)
	}
}

func TestV3_SavingsRecurringRuleAndFreeStored(t *testing.T) {
	p := decodeJSON(t, `{
		"projects": [
			{"id":"car","label":"Voiture","monthlyAmount":150,"startDate":"2026-03-01","endDate":"2026-12-31",
			 "amountHistory":[{"from":"2026-03","amount":100},{"from":"2026-06","amount":150}],
			 "overrides":{"2026-08":0}},
			{"id":"zero","label":"Zéro","monthlyAmount":0},
			{"id":"free","label":"Libre"},
			{"id":"epargne","label":"Épargne générale"}
		],
		"yearlyData": {"2026": {"months": [
			{}, {"free": 30}, {"free": 30, "car": 999}, {}, {}, {}, {}, {"car": 150}, {}, {}, {}, {}
		]}}
	}`)
	assertMoney(t, "feb (free only)", aggregateMonth(p, month(2026, 2), "fr", false).ProjectsAllocated, 30)
	assertMoney(t, "march (rule beats stale mirror)", aggregateMonth(p, month(2026, 3), "fr", false).ProjectsAllocated, 30+100)
	assertMoney(t, "june step", aggregateMonth(p, month(2026, 6), "fr", false).ProjectsAllocated, 150)
	assertMoney(t, "august skipped", aggregateMonth(p, month(2026, 8), "fr", false).ProjectsAllocated, 0)
	assertMoney(t, "2027 outside window", aggregateMonth(p, month(2027, 1), "fr", false).ProjectsAllocated, 0)
}

func TestV3_ClosedMonthSnapshotWins(t *testing.T) {
	snap := `{"v":1,"closedAt":"2026-08-01T00:00:00Z",
		"people":[{"id":"a","name":"A","salary":2000,"contribution":1500}],
		"charges":[{"id":"rent","label":"Loyer","amount":900,"planned":900}],
		"projects":[{"id":"car","label":"Voiture","allocation":80}],
		"oneOffs":[{"id":"x","label":"Prime","amount":250}]}`
	raw := `{
		"people":[{"id":"a","name":"A","salary":5000}],
		"charges":[{"id":"rent","label":"Loyer","amount":1400}],
		"projects":[{"id":"car","label":"Voiture","monthlyAmount":300}],
		"yearlyData":{"2026":{
			"months":[{},{},{},{},{},{},{"car":80},{"car":300}],
			"expenses":[{},{},{},{},{},{},{"car":20}],
			"lockedMonths":{"Juillet":true,"Août":false},
			"snapshots":[null,null,null,null,null,null,` + snap + `,` + snap + `]
		}},
		"oneTimeIncomes":{"2026":[{"amount":0},{"amount":0},{"amount":0},{"amount":0},{"amount":0},{"amount":0},{"amount":250},{"amount":0}]}
	}`
	p := decodeJSON(t, raw)
	jul := aggregateMonth(p, month(2026, 7), "fr", false)
	assertMoney(t, "frozen inflows", jul.BaseIncome, 1500)
	assertMoney(t, "frozen one-off", jul.OneTimeIncome, 250)
	assertMoney(t, "frozen charges", jul.RecurringCharges, 900)
	assertMoney(t, "frozen savings", jul.ProjectsAllocated, 80)
	assertMoney(t, "spent still read from expenses", jul.ProjectsSpent, 20)
	assertMoney(t, "net savings", jul.NetSavings, 1500+250-900-80)
	if !jul.IsLocked {
		t.Error("July should be locked")
	}
	// August was reopened (lock=false): its snapshot is ignored, rules apply.
	aug := aggregateMonth(p, month(2026, 8), "fr", false)
	assertMoney(t, "reopened month uses rules", aug.BaseIncome, 5000)
	assertMoney(t, "reopened charges", aug.RecurringCharges, 1400)
	assertMoney(t, "reopened savings", aug.ProjectsAllocated, 300)
}

func TestV3_OneOffItemsAndLegacyBareNumbers(t *testing.T) {
	p := decodeJSON(t, `{
		"oneTimeIncomes": {
			"2026": [{"amount":300,"description":"Prime, Remboursement","items":[
				{"id":"1","label":"Prime","amount":200},{"id":"2","label":"Remboursement","amount":100}]}],
			"2025": [0, 150]
		}
	}`)
	assertMoney(t, "items", aggregateMonth(p, month(2026, 1), "fr", false).OneTimeIncome, 300)
	assertMoney(t, "bare number", aggregateMonth(p, month(2025, 2), "fr", false).OneTimeIncome, 150)
}

func TestV3_YearTotalsFollowTheEngine(t *testing.T) {
	p := decodeJSON(t, `{
		"people":[{"id":"a","name":"A","salary":3000,"contributions":[{"from":"2026-01","mode":"fixed","value":2000}]}],
		"charges":[
			{"id":"rent","label":"Loyer","amount":1000},
			{"id":"tax","label":"Taxe","amount":600,"frequency":"yearly","startDate":"2026-10-01"}
		],
		"yearlyData":{"2026":{"expenses":[{"car":100}]}}
	}`)
	income, expenses := aggregateYearTotals(p, 2026)
	assertMoney(t, "year inflows", income, 12*2000)
	assertMoney(t, "year expenses", expenses, 12*1000+600+100)
}

// legacyAggregate is the pre-v3 recap algorithm, kept to prove that budgets
// without any v3 field produce exactly the same numbers as before.
func legacyAggregate(p *budgetPayload, year, monthIdx int) (income, oneTime, charges, allocated float64) {
	for _, person := range p.People {
		if isActiveInMonth(person.StartDate, person.EndDate, year, monthIdx) {
			income += person.Salary
		}
	}
	for _, c := range p.Charges {
		if isActiveInMonth(c.StartDate, c.EndDate, year, monthIdx) {
			charges += c.Amount
		}
	}
	if list, ok := p.OneTimeIncomes[fmt.Sprintf("%d", year)]; ok && monthIdx < len(list) {
		oneTime = list[monthIdx].Amount
	}
	if yd, ok := p.YearlyData[fmt.Sprintf("%d", year)]; ok && monthIdx < len(yd.Months) {
		allocated = sumMap(yd.Months[monthIdx])
	}
	return
}

func TestV3_LegacyParity(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	money := func(max int) float64 { return float64(r.Intn(max*100)) / 100 }
	date := func() string {
		if r.Intn(3) > 0 {
			return ""
		}
		return fmt.Sprintf("%d-%02d-%02d", 2025+r.Intn(3), 1+r.Intn(12), 1+r.Intn(28))
	}
	for n := 0; n < 250; n++ {
		p := &budgetPayload{YearlyData: map[string]budgetYear{}, OneTimeIncomes: map[string][]budgetOneTime{}}
		for i := 0; i < 1+r.Intn(4); i++ {
			p.People = append(p.People, budgetPerson{ID: fmt.Sprintf("p%d", i), Salary: money(5000), StartDate: date(), EndDate: date()})
		}
		for i := 0; i < r.Intn(8); i++ {
			p.Charges = append(p.Charges, budgetCharge{ID: fmt.Sprintf("c%d", i), Amount: money(1500), StartDate: date(), EndDate: date()})
		}
		for i := 0; i < r.Intn(4); i++ {
			p.Projects = append(p.Projects, budgetProject{ID: fmt.Sprintf("s%d", i), TargetAmount: money(5000)})
		}
		for _, y := range []string{"2025", "2026"} {
			yd := budgetYear{}
			ot := []budgetOneTime{}
			for m := 0; m < 12; m++ {
				alloc := map[string]float64{}
				for _, s := range p.Projects {
					if r.Intn(2) == 0 {
						alloc[s.ID] = money(400)
					}
				}
				yd.Months = append(yd.Months, alloc)
				yd.Expenses = append(yd.Expenses, map[string]float64{})
				ot = append(ot, budgetOneTime{Amount: map[bool]float64{true: money(800), false: 0}[r.Intn(4) == 0]})
			}
			p.YearlyData[y] = yd
			p.OneTimeIncomes[y] = ot
		}
		for _, y := range []int{2025, 2026} {
			for m := 0; m < 12; m++ {
				inc, one, ch, al := legacyAggregate(p, y, m)
				got := aggregateMonth(p, month(y, m+1), "fr", false)
				if math.Abs(got.BaseIncome-inc) > 0.005 || math.Abs(got.OneTimeIncome-one) > 0.005 ||
					math.Abs(got.RecurringCharges-ch) > 0.005 || math.Abs(got.ProjectsAllocated-al) > 0.005 {
					t.Fatalf("budget %d %d-%02d: legacy (%v,%v,%v,%v) vs v3 (%v,%v,%v,%v)", n, y, m+1,
						inc, one, ch, al, got.BaseIncome, got.OneTimeIncome, got.RecurringCharges, got.ProjectsAllocated)
				}
			}
		}
	}
}

func TestV3_TemplatesLabelPotInflows(t *testing.T) {
	data := &RecapData{
		UserName: "Libasse", BudgetName: "Famille", Currency: "EUR", CurrencySymbol: "€", Locale: "fr", Year: 2026,
		PreviousMonth: RecapMonth{Label: "Septembre 2026"}, CurrentMonth: RecapMonth{Label: "Octobre 2026"},
		NextMonth: RecapMonth{Label: "Novembre 2026"}, UsesContributions: true,
	}
	_, html, err := utils.RenderMonthlyRecapEmail("fr", "Octobre 2026", "Famille", data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "Entrées du pot commun") || strings.Contains(html, "Revenus prévus") {
		t.Error("fr template should label pot inflows when contributions are used")
	}
	data.UsesContributions = false
	_, html, _ = utils.RenderMonthlyRecapEmail("fr", "Octobre 2026", "Famille", data)
	if !strings.Contains(html, "Revenus prévus") || strings.Contains(html, "pot commun") {
		t.Error("fr template should keep 'Revenus' for legacy budgets")
	}
	data.UsesContributions = true
	_, html, err = utils.RenderMonthlyRecapEmail("en", "October 2026", "Family", data)
	if err != nil || !strings.Contains(html, "Shared-pot inflows") {
		t.Errorf("en template should label pot inflows (err=%v)", err)
	}
}

// TestV3_ParityWithUIEngine replays budgets written by the UI codec
// (budget-ui/src/lib/budget) and compares each month with the totals the UI
// engine computed for them: frozen closed months, reopened months, dated
// steps, overrides, frequencies, contributions and one-off items.
func TestV3_ParityWithUIEngine(t *testing.T) {
	raw, err := os.ReadFile("testdata/v3_parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Blob   map[string]interface{} `json:"blob"`
		Months map[string]struct {
			Contributions float64 `json:"contributions"`
			OneOff        float64 `json:"oneOff"`
			Charges       float64 `json:"charges"`
			Savings       float64 `json:"savings"`
		} `json:"months"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty fixture")
	}
	for i, c := range cases {
		p, err := decodeBudgetPayload(c.Blob)
		if err != nil {
			t.Fatalf("budget %d: decode: %v", i, err)
		}
		for ym, want := range c.Months {
			var y, m int
			fmt.Sscanf(ym, "%d-%d", &y, &m)
			got := aggregateMonth(p, month(y, m), "fr", false)
			if math.Abs(got.BaseIncome-want.Contributions) > 0.005 || math.Abs(got.OneTimeIncome-want.OneOff) > 0.005 ||
				math.Abs(got.RecurringCharges-want.Charges) > 0.005 || math.Abs(got.ProjectsAllocated-want.Savings) > 0.005 {
				t.Errorf("budget %d %s: UI (%v,%v,%v,%v) vs recap (%v,%v,%v,%v)", i, ym,
					want.Contributions, want.OneOff, want.Charges, want.Savings,
					got.BaseIncome, got.OneTimeIncome, got.RecurringCharges, got.ProjectsAllocated)
			}
		}
	}
}

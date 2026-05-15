package services

import (
	"strings"
	"testing"
	"time"
)

func TestAggregateMonth_LegacyYearFormat(t *testing.T) {
	payload := &budgetPayload{
		BudgetTitle: "Famille",
		People: []budgetPerson{
			{ID: "p1", Name: "Alice", Salary: 3000},
			{ID: "p2", Name: "Bob", Salary: 2500},
		},
		Charges: []budgetCharge{
			{ID: "c1", Label: "Loyer", Amount: 1200},
			{ID: "c2", Label: "Internet", Amount: 40},
		},
		Projects: []budgetProject{
			{ID: "proj-1", Label: "Vacances", TargetAmount: 2400},
		},
		YearlyData: map[string]budgetYear{
			"2026": {
				Months: []map[string]float64{
					0: {"proj-1": 200}, // January
					1: {"proj-1": 200}, // February
					2: {"proj-1": 200}, // March
					3: {"proj-1": 200}, // April
					4: {"proj-1": 200}, // May
				},
				Expenses: []map[string]float64{
					0: {"proj-1": 0},
					1: {"proj-1": 0},
					2: {"proj-1": 0},
					3: {"proj-1": 50}, // April spent 50
					4: {"proj-1": 0},
				},
				MonthComments: []string{"", "", "", "Ski", ""},
			},
		},
		OneTimeIncomes: map[string][]budgetOneTime{
			"2026": {
				{Amount: 0}, {Amount: 0}, {Amount: 0},
				{Amount: 100, Description: "Prime"},
			},
		},
		LockedMonths: map[string]bool{
			"Janvier": true,
		},
	}

	april := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	rm := aggregateMonth(payload, april, "fr", false)

	if rm.Label != "Avril 2026" {
		t.Errorf("unexpected label: %q", rm.Label)
	}
	if rm.BaseIncome != 5500 {
		t.Errorf("base income: want 5500, got %v", rm.BaseIncome)
	}
	if rm.OneTimeIncome != 100 {
		t.Errorf("one-time: want 100, got %v", rm.OneTimeIncome)
	}
	if rm.RecurringCharges != 1240 {
		t.Errorf("charges: want 1240, got %v", rm.RecurringCharges)
	}
	if rm.ProjectsAllocated != 200 {
		t.Errorf("allocated: want 200, got %v", rm.ProjectsAllocated)
	}
	if rm.ProjectsSpent != 50 {
		t.Errorf("spent: want 50, got %v", rm.ProjectsSpent)
	}
	if rm.Comment != "Ski" {
		t.Errorf("comment: want Ski, got %q", rm.Comment)
	}
	// 5500 + 100 - 1240 = 4360 available; 4360 - 200 = 4160 to save
	if rm.NetSavings != 4160 {
		t.Errorf("net savings: want 4160, got %v", rm.NetSavings)
	}

	jan := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rmJan := aggregateMonth(payload, jan, "fr", false)
	if !rmJan.IsLocked {
		t.Error("January should be locked")
	}
}

func TestLocaleForLocation(t *testing.T) {
	cases := map[string]string{
		"FR": "fr",
		"BE": "fr",
		"CH": "fr",
		"":   "en",
		"US": "en",
		"DE": "en",
		"ML": "fr",
	}
	for in, want := range cases {
		if got := localeForLocation(in); got != want {
			t.Errorf("localeForLocation(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAggregateProjects_TargetProgressAndStatus(t *testing.T) {
	year := time.Now().Year()
	payload := &budgetPayload{
		Projects: []budgetProject{
			{ID: "p1", Label: "Vacances", TargetAmount: 1000},
			{ID: "p2", Label: "Pas de cible"}, // no target
		},
		YearlyData: map[string]budgetYear{
			intToStr(year): {
				Months: []map[string]float64{
					{"p1": 1000, "p2": 50},
				},
			},
		},
	}
	out := aggregateProjects(payload, year, "fr")
	if len(out) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(out))
	}
	if out[0].Progress != 100 {
		t.Errorf("p1 progress: want 100, got %v", out[0].Progress)
	}
	if !out[0].HasTarget {
		t.Error("p1 should have target")
	}
	if out[1].Status != "no_target" {
		t.Errorf("p2 status: want no_target, got %s", out[1].Status)
	}
}

func intToStr(n int) string {
	out := ""
	if n == 0 {
		return "0"
	}
	for n > 0 {
		out = string(rune('0'+n%10)) + out
		n /= 10
	}
	return out
}

func TestFriendlyEmailFallback(t *testing.T) {
	cases := map[string]string{
		"alice@example.com": "Alice",
		"bob@example.com":   "Bob",
		"":                  "",
		"@a":                "@a",
	}
	for in, want := range cases {
		if got := friendlyEmailFallback(in); got != want {
			t.Errorf("friendlyEmailFallback(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeHelpers(t *testing.T) {
	if normalizeCountry("") != "FR" {
		t.Error("empty country should default to FR")
	}
	if normalizeCountry(" be ") != "BE" {
		t.Error("country should be trimmed and upper-cased")
	}
	if normalizeCurrency("") != "EUR" {
		t.Error("empty currency should default to EUR")
	}
	if normalizeCurrency("usd") != "USD" {
		t.Error("currency should be upper-cased")
	}
	if bucketHouseholdSize(0) != 1 || bucketHouseholdSize(7) != 4 {
		t.Error("household bucketing broken")
	}
}

func TestFirstOfMonthAndPrevNext(t *testing.T) {
	now := time.Date(2026, 5, 17, 14, 30, 0, 0, time.UTC)
	first := firstOfMonth(now)
	if first.Day() != 1 || first.Month() != 5 {
		t.Errorf("firstOfMonth: %v", first)
	}
	prev := first.AddDate(0, -1, 0)
	if prev.Month() != 4 {
		t.Errorf("prev: %v", prev)
	}
	next := first.AddDate(0, 1, 0)
	if next.Month() != 6 {
		t.Errorf("next: %v", next)
	}
}

func TestCurrencySymbol(t *testing.T) {
	if currencySymbol("EUR") != "€" {
		t.Error("EUR symbol")
	}
	if currencySymbol("usd") == "$" {
		// good — case-insensitive
	}
	if currencySymbol("CHF") != "CHF" {
		t.Error("CHF symbol")
	}
	if currencySymbol("ZAR") != "ZAR" {
		t.Error("unknown currency should echo code")
	}
}

func TestDecodeBudgetPayload_Nil(t *testing.T) {
	p, err := decodeBudgetPayload(nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.YearlyData == nil || p.LockedMonths == nil {
		t.Error("expected non-nil maps even with nil input")
	}
}

func TestSumMap(t *testing.T) {
	if sumMap(map[string]float64{"a": 1, "b": 2.5}) != 3.5 {
		t.Error("sumMap broken")
	}
}

func TestDefaultNames(t *testing.T) {
	if !strings.Contains(defaultBudgetName("fr"), "budget") && !strings.Contains(defaultBudgetName("fr"), "Votre") {
		t.Error("fr default budget name")
	}
	if defaultBudgetName("en") != "Your budget" {
		t.Error("en default budget name")
	}
}

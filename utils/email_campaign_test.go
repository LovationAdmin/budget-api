package utils

import (
	"strings"
	"testing"
)

// minimalRecap mirrors the shape services.RecapData exposes to the template.
// We can't import services here (would create a cycle), so we just declare a
// struct with the same field names — Go templates resolve at runtime.
type minimalRecap struct {
	UserName       string
	BudgetName     string
	Currency       string
	CurrencySymbol string
	Locale         string
	Year           int
	AppURL         string
	LoginURL       string
	CampaignID     string

	PreviousMonth recapMonth
	CurrentMonth  recapMonth
	NextMonth     recapMonth

	YearIncome   float64
	YearExpenses float64
	YearSavings  float64

	Projects []recapProject

	OtherBudgets []budgetSummary

	OtherBudgetCount  int
	HiddenBudgetCount int
	GeneratedAt       string
}

type budgetSummary struct {
	ID             string
	Name           string
	Currency       string
	CurrencySymbol string
	YearIncome     float64
	YearExpenses   float64
	YearSavings    float64
	URL            string
}

type recapMonth struct {
	Label             string
	ShortLabel        string
	Year              int
	MonthIdx          int
	BaseIncome        float64
	OneTimeIncome     float64
	TotalIncome       float64
	RecurringCharges  float64
	ProjectsAllocated float64
	ProjectsSpent     float64
	Available         float64
	NetSavings        float64
	NetCashflow       float64
	Comment           string
	ProjectNotes      []projectNote
	HasData           bool
	IsLocked          bool
}

type projectNote struct {
	Name string
	Note string
}

type recapProject struct {
	Name         string
	TargetAmount float64
	AllocatedYTD float64
	SpentYTD     float64
	Progress     float64
	Status       string
	HasTarget    bool
}

func buildSampleRecap() minimalRecap {
	return minimalRecap{
		UserName:       "Alice",
		BudgetName:     "Famille Dupont",
		Currency:       "EUR",
		CurrencySymbol: "€",
		Locale:         "fr",
		Year:           2026,
		AppURL:         "https://app.budgetfamille.com",
		LoginURL:       "https://app.budgetfamille.com/login",
		CampaignID:     "monthly_recap_2026_04",

		PreviousMonth: recapMonth{
			Label: "Avril 2026", ShortLabel: "Avril", Year: 2026, MonthIdx: 3,
			BaseIncome: 5500, OneTimeIncome: 100, TotalIncome: 5600,
			RecurringCharges: 1240, ProjectsAllocated: 200, ProjectsSpent: 50,
			Available: 4360, NetSavings: 4160, NetCashflow: 4310,
			Comment: "Ski en famille", HasData: true,
			ProjectNotes: []projectNote{
				{Name: "Vacances", Note: "Acompte chalet versé"},
				{Name: "Voiture", Note: "Révision faite"},
			},
		},
		CurrentMonth: recapMonth{
			Label: "Mai 2026", ShortLabel: "Mai", Year: 2026, MonthIdx: 4,
			BaseIncome: 5500, TotalIncome: 5500, RecurringCharges: 1240,
			ProjectsAllocated: 200, Available: 4260, NetSavings: 4060,
			NetCashflow: 4260, HasData: true,
			Comment: "Mois de la fête des mères",
			ProjectNotes: []projectNote{
				{Name: "Vacances", Note: "Réserver l'avion"},
			},
		},
		NextMonth: recapMonth{
			Label: "Juin 2026", ShortLabel: "Juin", Year: 2026, MonthIdx: 5,
			BaseIncome: 5500, TotalIncome: 5500, RecurringCharges: 1240,
			ProjectsAllocated: 200, Available: 4260, HasData: true,
			Comment: "Anniversaire des jumeaux",
			ProjectNotes: []projectNote{
				{Name: "Voiture", Note: "Contrôle technique prévu"},
			},
		},
		YearIncome:   28000,
		YearExpenses: 6200,
		YearSavings:  21800,
		Projects: []recapProject{
			{Name: "Vacances", TargetAmount: 2400, AllocatedYTD: 1000, Progress: 41.6, Status: "on_track", HasTarget: true},
			{Name: "Épargne", AllocatedYTD: 500, HasTarget: false, Status: "no_target"},
		},
		OtherBudgets: []budgetSummary{
			{
				ID: "b2", Name: "Studio location",
				Currency: "EUR", CurrencySymbol: "€",
				YearIncome: 12000, YearExpenses: 8400, YearSavings: 3600,
				URL: "https://app.budgetfamille.com/budget/b2",
			},
		},
		OtherBudgetCount:  3,
		HiddenBudgetCount: 2,
		GeneratedAt:       "2026-05-01",
	}
}

func TestRenderMonthlyRecapEmail_French(t *testing.T) {
	recap := buildSampleRecap()
	subject, html, err := RenderMonthlyRecapEmail("fr", recap.CurrentMonth.Label, recap.BudgetName, recap)
	if err != nil {
		t.Fatalf("render fr failed: %v", err)
	}
	if !strings.Contains(subject, "Mai 2026") {
		t.Errorf("subject missing month: %q", subject)
	}
	if !strings.Contains(subject, "Famille Dupont") {
		t.Errorf("subject missing budget: %q", subject)
	}
	wants := []string{
		"Avril 2026", "Mai", "Juin",
		"Tes projets avancent",
		"Vacances",
		"€",
		"Bonjour Alice",
		"monthly_recap_2026_04",
		"Ski en famille",
		"Acompte chalet versé",
		"Tes notes par poste",
		// CurrentMonth notes/comment
		"Mois de la fête des mères",
		"Réserver",
		// NextMonth notes/comment
		"Anniversaire des jumeaux",
		"Contrôle technique prévu",
		// Other budgets
		"Tes autres budgets",
		"Studio location",
		"2 autres budgets",
	}
	for _, w := range wants {
		if !strings.Contains(html, w) {
			t.Errorf("html missing %q", w)
		}
	}
}

func TestRenderMonthlyRecapEmail_English(t *testing.T) {
	recap := buildSampleRecap()
	recap.Locale = "en"
	recap.PreviousMonth.Label = "April 2026"
	recap.PreviousMonth.ShortLabel = "April"
	recap.CurrentMonth.Label = "May 2026"
	recap.CurrentMonth.ShortLabel = "May"
	recap.NextMonth.Label = "June 2026"
	recap.NextMonth.ShortLabel = "June"

	subject, html, err := RenderMonthlyRecapEmail("en", recap.CurrentMonth.Label, recap.BudgetName, recap)
	if err != nil {
		t.Fatalf("render en failed: %v", err)
	}
	if !strings.Contains(subject, "May 2026") {
		t.Errorf("subject missing month: %q", subject)
	}
	wants := []string{
		"April 2026", "May", "June",
		"Your projects are progressing",
		"Hi Alice",
		"Acompte chalet versé",
		"line-item notes",
		"Your intention for this month",
		"What you're planning",
		"Your other budgets",
		"Studio location",
		"2 other budgets",
	}
	for _, w := range wants {
		if !strings.Contains(html, w) {
			t.Errorf("html missing %q", w)
		}
	}
}

func TestFormatMoney(t *testing.T) {
	const nbsp = " "
	cases := []struct {
		in     float64
		symbol string
		want   string
	}{
		{0, "€", "0 €"},
		{12, "€", "12 €"},
		{1234, "€", "1" + nbsp + "234 €"},
		{1234567, "€", "1" + nbsp + "234" + nbsp + "567 €"},
		{-50, "€", "−50 €"},
		{100, "$", "100 $"},
	}
	for _, c := range cases {
		got := formatMoney(c.in, c.symbol)
		if got != c.want {
			t.Errorf("formatMoney(%v, %s) = %q, want %q", c.in, c.symbol, got, c.want)
		}
	}
}

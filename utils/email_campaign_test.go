package utils

import (
	"os"
	"path/filepath"
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

	YearIncome        float64
	YearExpenses      float64
	YearSavings       float64
	UsesContributions bool
	BudgetURL         string
	Tip               recapTip

	Projects []recapProject

	OtherBudgets []budgetSummary

	OtherBudgetCount  int
	HiddenBudgetCount int
	GeneratedAt       string
}

type recapTip struct {
	Emoji, Title, Body, CTA, URL string
}

type recapMember struct {
	Name            string
	Salary          float64
	Contribution    float64
	PocketMoney     float64
	PersonalCharges float64
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
	Members           []recapMember
	PersonalCharges   float64
	URL               string
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
			Members: []recapMember{
				{Name: "Alice", Salary: 3200, Contribution: 2000, PocketMoney: 1200, PersonalCharges: 150},
				{Name: "Bruno", Salary: 2300, Contribution: 1500, PocketMoney: 800},
			},
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
			URL:     "https://app.budgetfamille.com/budget/b1/complete/month?m=2026-05",
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
		UsesContributions: true,
		BudgetURL:         "https://app.budgetfamille.com/budget/b1/complete/month",
		Tip:               recapTip{Emoji: "🎯", Title: "Un objectif, une date", Body: "Le montant mensuel se calcule tout seul.", CTA: "Créer une cagnotte", URL: "https://app.budgetfamille.com/dashboard"},
		YearIncome:        28000,
		YearExpenses:      6200,
		YearSavings:       21800,
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
	if !strings.Contains(subject, "Votre bilan de mai 2026") {
		t.Errorf("subject missing month: %q", subject)
	}
	if !strings.Contains(subject, "Famille Dupont") {
		t.Errorf("subject missing budget: %q", subject)
	}
	wants := []string{
		"Bilan d’avril 2026", "Votre mois d’avril", "Et maintenant, mai", "À prévoir en juin",
		"Vos cagnottes avancent",
		"Vacances",
		"€",
		"Bonjour Alice",
		"monthly_recap_2026_04",
		"Ski en famille",
		"Acompte chalet versé",
		"Vos notes par poste",
		// Household: contributions, spending money, personal charges
		"Côté foyer", "Argent de poche", "dont 150 € de charges perso", "Bruno",
		// Deep link, tip
		"complete/month?m=2026-05&amp;utm_source=email",
		"Le saviez-vous", "Un objectif, une date",
		// CurrentMonth notes/comment
		"Mois de la fête des mères",
		"Réserver",
		// NextMonth notes/comment
		"Anniversaire des jumeaux",
		"Contrôle technique prévu",
		// Other budgets
		"Vos autres budgets",
		"Studio location",
		"Et 2 autres budgets",
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
		"Spending money", "incl. 150 € personal charges",
		"Acompte chalet versé",
		"line-item notes",
		"Your intention for this month",
		"What you’re planning",
		"Your other budgets",
		"Studio location",
		"2 other budgets",
		"Did you know?",
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

func TestRenderWhatsNew202610(t *testing.T) {
	t.Setenv("FRONTEND_URL", "https://budgetfamille.com")
	subject, html, err := RenderCampaignEmail(CampaignWhatsNew202610, "Alice", "whatsnew_2026_10")
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if subject == "" {
		t.Error("missing subject")
	}
	for _, w := range []string{
		"Bonjour Alice",
		"sincèrement désolé",
		"vos données sont intactes",
		"argent de poche",
		"Budget IA",
		"mode sombre",
		"https://budgetfamille.com/email/2026-10/month-light.jpg",
		"https://budgetfamille.com/dashboard?utm_source=email&amp;utm_medium=campaign&amp;utm_campaign=whatsnew_2026_10",
		"répondez «&nbsp;STOP&nbsp;»",
	} {
		if !strings.Contains(html, w) {
			t.Errorf("html missing %q", w)
		}
	}
	// No name: a friendly fallback instead of "Bonjour ,".
	_, html, _ = RenderCampaignEmail(CampaignWhatsNew202610, "", "whatsnew_2026_10")
	if !strings.Contains(html, "Bonjour à vous") {
		t.Error("empty name should fall back to « Bonjour à vous »")
	}
	if dir := os.Getenv("EMAIL_PREVIEW_DIR"); dir != "" {
		_, html, _ = RenderCampaignEmail(CampaignWhatsNew202610, "Camille", "whatsnew_2026_10")
		_ = os.WriteFile(filepath.Join(dir, "whatsnew.html"), []byte(html), 0o644)
	}
}

func TestMailboxAddress(t *testing.T) {
	cases := map[string]string{
		"lovation.pro@gmail.com":                    "lovation.pro@gmail.com",
		"Libasse — Budget Famille <libasse@bf.com>": "libasse@bf.com",
		" <a@b.c> ": "a@b.c",
	}
	for in, want := range cases {
		if got := mailboxAddress(in); got != want {
			t.Errorf("mailboxAddress(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeMonth(t *testing.T) {
	cases := map[string]string{
		"Septembre 2026": "de septembre 2026",
		"Avril 2026":     "d’avril 2026",
		"Août":           "d’août",
		"octobre":        "d’octobre",
		"Mai":            "de mai",
	}
	for in, want := range cases {
		if got := deMonth(in); got != want {
			t.Errorf("deMonth(%q) = %q, want %q", in, got, want)
		}
	}
}

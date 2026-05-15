// cmd/recap_preview/main.go
// ============================================================================
// Renders the monthly recap email with sample data and writes it to disk.
// Run with: go run ./cmd/recap_preview > /tmp/recap_fr.html
// ============================================================================

package main

import (
	"fmt"
	"os"

	"github.com/LovationAdmin/budget-api/utils"
)

type recapMonth struct {
	Label, ShortLabel string
	Year, MonthIdx    int
	BaseIncome, OneTimeIncome, TotalIncome,
	RecurringCharges, ProjectsAllocated, ProjectsSpent,
	Available, NetSavings, NetCashflow float64
	Comment           string
	HasData, IsLocked bool
}

type recapProject struct {
	Name                                   string
	TargetAmount, AllocatedYTD, SpentYTD,
	Progress float64
	Status    string
	HasTarget bool
}

type sample struct {
	UserName, BudgetName, Currency, CurrencySymbol, Locale string
	Year                                                   int
	AppURL, LoginURL, CampaignID                           string
	PreviousMonth, CurrentMonth, NextMonth                 recapMonth
	YearIncome, YearExpenses, YearSavings                  float64
	Projects                                               []recapProject
	OtherBudgetCount                                       int
	GeneratedAt                                            string
}

func main() {
	r := sample{
		UserName: "Alice", BudgetName: "Famille Dupont", Currency: "EUR",
		CurrencySymbol: "€", Locale: "fr", Year: 2026,
		AppURL: "https://app.budgetfamille.com", LoginURL: "https://app.budgetfamille.com/login",
		CampaignID: "monthly_recap_2026_04",
		PreviousMonth: recapMonth{
			Label: "Avril 2026", ShortLabel: "Avril", Year: 2026, MonthIdx: 3,
			BaseIncome: 5500, OneTimeIncome: 100, TotalIncome: 5600,
			RecurringCharges: 1240, ProjectsAllocated: 200, ProjectsSpent: 50,
			Available: 4360, NetSavings: 4160, NetCashflow: 4310,
			Comment: "Quelques courses imprévues et un week-end ski",
			HasData: true,
		},
		CurrentMonth: recapMonth{
			Label: "Mai 2026", ShortLabel: "Mai", Year: 2026, MonthIdx: 4,
			BaseIncome: 5500, TotalIncome: 5500, RecurringCharges: 1240,
			ProjectsAllocated: 200, Available: 4260, NetSavings: 4060,
			NetCashflow: 4260, HasData: true,
		},
		NextMonth: recapMonth{
			Label: "Juin 2026", ShortLabel: "Juin", Year: 2026, MonthIdx: 5,
			BaseIncome: 5500, TotalIncome: 5500, RecurringCharges: 1240,
			ProjectsAllocated: 200, Available: 4260, HasData: true,
		},
		YearIncome: 28000, YearExpenses: 6200, YearSavings: 21800,
		Projects: []recapProject{
			{Name: "Vacances été", TargetAmount: 2400, AllocatedYTD: 1000, Progress: 41, Status: "on_track", HasTarget: true},
			{Name: "Travaux cuisine", TargetAmount: 5000, AllocatedYTD: 600, Progress: 12, Status: "behind", HasTarget: true},
			{Name: "Épargne libre", AllocatedYTD: 500, HasTarget: false, Status: "no_target"},
		},
		OtherBudgetCount: 1,
		GeneratedAt:      "2026-05-01",
	}

	locale := "fr"
	if len(os.Args) > 1 && os.Args[1] == "en" {
		locale = "en"
		r.Locale = "en"
		r.PreviousMonth.Label, r.PreviousMonth.ShortLabel = "April 2026", "April"
		r.CurrentMonth.Label, r.CurrentMonth.ShortLabel = "May 2026", "May"
		r.NextMonth.Label, r.NextMonth.ShortLabel = "June 2026", "June"
		r.PreviousMonth.Comment = "A few unexpected grocery runs and a ski weekend"
		r.Projects[0].Name = "Summer holidays"
		r.Projects[1].Name = "Kitchen renovation"
		r.Projects[2].Name = "Open savings"
	}

	subject, html, err := utils.RenderMonthlyRecapEmail(locale, r.CurrentMonth.Label, r.BudgetName, r)
	if err != nil {
		fmt.Fprintln(os.Stderr, "render:", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "SUBJECT:", subject)
	fmt.Print(html)
}

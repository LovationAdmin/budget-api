// services/monthly_recap.go
// ============================================================================
// MONTHLY RECAP — aggregates the previous / current / next month for a budget
// and produces the template data consumed by monthly_recap_*.html.
// ============================================================================
//
// Sent once per month (cron + admin trigger) to every verified user, scoped to
// their most recently updated budget. The aggregation runs against the
// canonical year-based budget_data shape produced by the UI's
// convertNewFormatToOld() function.
//
// The recap is purely read-only: it never writes back to budget_data.
// ============================================================================

package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/LovationAdmin/budget-api/utils"
)

// ----------------------------------------------------------------------------
// PUBLIC TYPES (consumed by the HTML templates)
// ----------------------------------------------------------------------------

type RecapMonth struct {
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
	Available         float64 // income - recurring charges
	NetSavings        float64 // available - allocated to projects
	NetCashflow       float64 // income - charges - actual spent
	Comment           string
	ProjectNotes      []ProjectNote // per-project notes for the month
	HasData           bool
	IsLocked          bool
}

// ProjectNote is a single per-project note attached to a month, surfaced
// from the budget's expenseComments map.
type ProjectNote struct {
	Name string
	Note string
}

type RecapProject struct {
	Name         string
	TargetAmount float64
	AllocatedYTD float64
	SpentYTD     float64
	Progress     float64 // 0..100 (against target if set, else against allocated)
	Status       string  // on_track | ahead | behind | no_target
	HasTarget    bool
}

type RecapData struct {
	UserName       string
	BudgetName     string
	Currency       string
	CurrencySymbol string
	Locale         string // "fr" | "en"
	Year           int
	AppURL         string
	LoginURL       string
	CampaignID     string

	PreviousMonth RecapMonth
	CurrentMonth  RecapMonth
	NextMonth     RecapMonth

	YearIncome   float64
	YearExpenses float64 // recurring charges + project spend
	YearSavings  float64 // YearIncome - YearExpenses

	Projects []RecapProject

	// OtherBudgets is a short summary of the user's other budgets (capped
	// in the caller). Empty when the user has a single budget.
	OtherBudgets []BudgetSummary

	// OtherBudgetCount is the total number of secondary budgets the user
	// owns (i.e. total minus the primary one), independent of how many we
	// display in OtherBudgets.
	OtherBudgetCount int

	// HiddenBudgetCount is the number of secondary budgets the user owns
	// beyond what's shown in OtherBudgets (e.g. user has 8 budgets, we show
	// the primary + 3 others, HiddenBudgetCount = 4).
	HiddenBudgetCount int

	GeneratedAt string
}

// BudgetSummary is the compact per-budget block we surface at the end of the
// recap email when the user owns more than one budget.
type BudgetSummary struct {
	ID             string
	Name           string
	Currency       string
	CurrencySymbol string
	YearIncome     float64
	YearExpenses   float64
	YearSavings    float64
	URL            string
}

// ----------------------------------------------------------------------------
// SERVICE
// ----------------------------------------------------------------------------

type MonthlyRecapService struct {
	DB     *sql.DB
	Budget *BudgetService
}

func NewMonthlyRecapService(db *sql.DB, budget *BudgetService) *MonthlyRecapService {
	return &MonthlyRecapService{DB: db, Budget: budget}
}

// VerifiedUser is the minimal user shape needed to route a recap.
type VerifiedUser struct {
	ID    string
	Email string
	Name  string
}

// budgetSummary is the per-user budget shortlist returned by the SQL.
type budgetSummary struct {
	ID        string
	Name      string
	Location  string
	Currency  string
	UpdatedAt time.Time
}

// ListVerifiedUsers returns all users with email_verified=true.
// Use limit > 0 to cap the result set for dry runs.
func (s *MonthlyRecapService) ListVerifiedUsers(ctx context.Context, limit int) ([]VerifiedUser, error) {
	q := `SELECT id::text, email, COALESCE(name, '') FROM users WHERE email_verified = TRUE ORDER BY created_at ASC`
	args := []any{}
	if limit > 0 {
		q += " LIMIT $1"
		args = append(args, limit)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	rows, err := s.DB.QueryContext(queryCtx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]VerifiedUser, 0)
	for rows.Next() {
		var u VerifiedUser
		if err := rows.Scan(&u.ID, &u.Email, &u.Name); err != nil {
			return nil, err
		}
		if u.Email == "" {
			continue
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ListUserBudgets returns every budget the user is a member of, ordered by
// most-recently consulted (falling back to most-recently updated when the
// last_viewed_at column has never been bumped). Pass limit > 0 to cap the
// returned slice (useful for the recap which only needs the primary budget +
// a few others to summarise). The returned totalCount is the user's full
// budget count, independent of the limit, so callers can show "X more" hints.
func (s *MonthlyRecapService) ListUserBudgets(ctx context.Context, userID string, limit int) (budgets []budgetSummary, totalCount int, err error) {
	q := `
		SELECT b.id::text,
		       COALESCE(b.name, ''),
		       COALESCE(b.location, 'FR'),
		       COALESCE(b.currency, 'EUR'),
		       b.updated_at,
		       COUNT(*) OVER () AS total_count
		FROM budgets b
		JOIN budget_members bm ON bm.budget_id = b.id
		WHERE bm.user_id = $1
		ORDER BY COALESCE(b.last_viewed_at, b.updated_at) DESC
	`
	args := []any{userID}
	if limit > 0 {
		q += " LIMIT $2"
		args = append(args, limit)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	rows, err := s.DB.QueryContext(queryCtx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []budgetSummary{}
	total := 0
	for rows.Next() {
		var b budgetSummary
		if err := rows.Scan(&b.ID, &b.Name, &b.Location, &b.Currency, &b.UpdatedAt, &total); err != nil {
			return nil, 0, err
		}
		out = append(out, b)
	}
	return out, total, rows.Err()
}

// PickRecapBudget returns the budget to recap for this user. We choose the
// most recently updated budget the user is a member of (owner or invited).
// Returns nil if the user has no budgets.
func (s *MonthlyRecapService) PickRecapBudget(ctx context.Context, userID string) (*budgetSummary, int, error) {
	q := `
		SELECT b.id::text, COALESCE(b.name, ''), COALESCE(b.location, 'FR'), COALESCE(b.currency, 'EUR'), b.updated_at
		FROM budgets b
		JOIN budget_members bm ON bm.budget_id = b.id
		WHERE bm.user_id = $1
		ORDER BY b.updated_at DESC
	`
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	rows, err := s.DB.QueryContext(queryCtx, q, userID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var picked *budgetSummary
	count := 0
	for rows.Next() {
		var b budgetSummary
		if err := rows.Scan(&b.ID, &b.Name, &b.Location, &b.Currency, &b.UpdatedAt); err != nil {
			return nil, 0, err
		}
		count++
		if picked == nil {
			b := b
			picked = &b
		}
	}
	return picked, count, rows.Err()
}

// BuildRecap reads the budget_data, decrypts it, and produces the recap for
// the period (previousMonth, currentMonth, nextMonth) anchored on `now`.
// The previous month is the one that just ended (e.g., running on May 1st
// yields April / May / June).
//
// `others` contains the user's secondary budgets (already filtered by the
// caller, e.g. top 3 by updated_at). Their year-to-date totals are read
// here so we can surface them as compact cards at the end of the email.
func (s *MonthlyRecapService) BuildRecap(
	ctx context.Context,
	user VerifiedUser,
	bs *budgetSummary,
	others []budgetSummary,
	otherBudgetCount int,
	now time.Time,
	campaignID string,
	appURL string,
) (*RecapData, error) {
	raw, err := s.Budget.GetData(ctx, bs.ID)
	if err != nil {
		return nil, fmt.Errorf("get budget data: %w", err)
	}

	// raw is interface{} — re-marshal/unmarshal into our typed payload.
	payload, err := decodeBudgetPayload(raw)
	if err != nil {
		return nil, fmt.Errorf("decode budget payload: %w", err)
	}

	locale := localeForLocation(bs.Location)
	currency := strings.ToUpper(strings.TrimSpace(bs.Currency))
	if currency == "" {
		currency = "EUR"
	}

	prevTime := firstOfMonth(now).AddDate(0, -1, 0)
	currTime := firstOfMonth(now)
	nextTime := firstOfMonth(now).AddDate(0, 1, 0)

	prev := aggregateMonth(payload, prevTime, locale, false)
	curr := aggregateMonth(payload, currTime, locale, true)
	next := aggregateMonth(payload, nextTime, locale, false)

	prev.ProjectNotes = aggregateProjectNotes(payload, prevTime, locale)
	curr.ProjectNotes = aggregateProjectNotes(payload, currTime, locale)
	next.ProjectNotes = aggregateProjectNotes(payload, nextTime, locale)

	projects := aggregateProjects(payload, currTime.Year(), locale)
	yearIncome, yearExpenses := aggregateYearTotals(payload, currTime.Year())

	loginURL := strings.TrimRight(appURL, "/") + "/login"

	displayName := user.Name
	if displayName == "" {
		displayName = friendlyEmailFallback(user.Email)
	}
	budgetName := bs.Name
	if budgetName == "" {
		budgetName = payload.BudgetTitle
	}
	if budgetName == "" {
		budgetName = defaultBudgetName(locale)
	}

	otherSummaries := s.buildOtherBudgetSummaries(ctx, others, currTime.Year(), appURL)

	return &RecapData{
		UserName:         displayName,
		BudgetName:       budgetName,
		Currency:         currency,
		CurrencySymbol:   currencySymbol(currency),
		Locale:           locale,
		Year:             currTime.Year(),
		AppURL:           appURL,
		LoginURL:         loginURL,
		CampaignID:       campaignID,
		PreviousMonth:    prev,
		CurrentMonth:     curr,
		NextMonth:        next,
		YearIncome:       yearIncome,
		YearExpenses:     yearExpenses,
		YearSavings:      yearIncome - yearExpenses,
		Projects:         projects,
		OtherBudgets:      otherSummaries,
		OtherBudgetCount:  otherBudgetCount - 1,
		HiddenBudgetCount: maxInt(0, (otherBudgetCount-1)-len(otherSummaries)),
		GeneratedAt:       now.Format("2006-01-02"),
	}, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// buildOtherBudgetSummaries fetches each secondary budget's data and produces
// a lightweight year-to-date summary. Failures are logged and skipped so a
// single corrupt budget never breaks the whole email.
func (s *MonthlyRecapService) buildOtherBudgetSummaries(
	ctx context.Context,
	others []budgetSummary,
	year int,
	appURL string,
) []BudgetSummary {
	if len(others) == 0 {
		return nil
	}
	out := make([]BudgetSummary, 0, len(others))
	for _, b := range others {
		raw, err := s.Budget.GetData(ctx, b.ID)
		if err != nil {
			utils.SafeWarn("monthly-recap: skip other budget %s: get data: %v", b.ID, err)
			continue
		}
		payload, err := decodeBudgetPayload(raw)
		if err != nil {
			utils.SafeWarn("monthly-recap: skip other budget %s: decode: %v", b.ID, err)
			continue
		}
		income, expenses := aggregateYearTotals(payload, year)

		name := b.Name
		if name == "" {
			name = payload.BudgetTitle
		}
		if name == "" {
			name = defaultBudgetName(localeForLocation(b.Location))
		}

		currency := strings.ToUpper(strings.TrimSpace(b.Currency))
		if currency == "" {
			currency = "EUR"
		}

		out = append(out, BudgetSummary{
			ID:             b.ID,
			Name:           name,
			Currency:       currency,
			CurrencySymbol: currencySymbol(currency),
			YearIncome:     income,
			YearExpenses:   expenses,
			YearSavings:    income - expenses,
			URL:            strings.TrimRight(appURL, "/") + "/budget/" + b.ID,
		})
	}
	return out
}

// ----------------------------------------------------------------------------
// INTERNAL — budget data shape (year-based legacy format)
// ----------------------------------------------------------------------------

type budgetPerson struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Salary    float64 `json:"salary"`
	StartDate string  `json:"startDate,omitempty"`
	EndDate   string  `json:"endDate,omitempty"`
}

type budgetCharge struct {
	ID        string  `json:"id"`
	Label     string  `json:"label"`
	Amount    float64 `json:"amount"`
	StartDate string  `json:"startDate,omitempty"`
	EndDate   string  `json:"endDate,omitempty"`
}

type budgetProject struct {
	ID           string  `json:"id"`
	Label        string  `json:"label"`
	TargetAmount float64 `json:"targetAmount,omitempty"`
	// "Épargne particulière": fixed monthly amount auto-filled into the
	// calendar over an optional start/end window. Allocations are persisted in
	// YearlyData by the client, so aggregation still reads them from there;
	// these fields carry the definition for forward-compatible server logic.
	MonthlyAmount float64 `json:"monthlyAmount,omitempty"`
	StartDate     string  `json:"startDate,omitempty"`
	EndDate       string  `json:"endDate,omitempty"`
}

type budgetYear struct {
	Months          []map[string]float64 `json:"months"`
	Expenses        []map[string]float64 `json:"expenses"`
	MonthComments   []string             `json:"monthComments"`
	ExpenseComments []map[string]string  `json:"expenseComments"`
}

type budgetOneTime struct {
	Amount      float64 `json:"amount"`
	Description string  `json:"description,omitempty"`
}

type budgetPayload struct {
	BudgetTitle    string                       `json:"budgetTitle"`
	CurrentYear    int                          `json:"currentYear"`
	People         []budgetPerson               `json:"people"`
	Charges        []budgetCharge               `json:"charges"`
	Projects       []budgetProject              `json:"projects"`
	YearlyData     map[string]budgetYear        `json:"yearlyData"`
	OneTimeIncomes map[string][]budgetOneTime   `json:"oneTimeIncomes"`
	LockedMonths   map[string]bool              `json:"lockedMonths"`
}

func decodeBudgetPayload(raw interface{}) (*budgetPayload, error) {
	p := &budgetPayload{
		YearlyData:     map[string]budgetYear{},
		OneTimeIncomes: map[string][]budgetOneTime{},
		LockedMonths:   map[string]bool{},
	}
	if raw == nil {
		return p, nil
	}
	bytes, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(bytes, p); err != nil {
		return nil, err
	}
	if p.YearlyData == nil {
		p.YearlyData = map[string]budgetYear{}
	}
	if p.OneTimeIncomes == nil {
		p.OneTimeIncomes = map[string][]budgetOneTime{}
	}
	if p.LockedMonths == nil {
		p.LockedMonths = map[string]bool{}
	}
	return p, nil
}

// ----------------------------------------------------------------------------
// AGGREGATION HELPERS
// ----------------------------------------------------------------------------

// frMonthsCanonical are the keys the UI's importConverter uses inside the
// stored budget data (locked-months map, comments). Both display in French
// and lookup share the same table — keep them aligned.
var frMonthsCanonical = [12]string{
	"Janvier", "Février", "Mars", "Avril", "Mai", "Juin",
	"Juillet", "Août", "Septembre", "Octobre", "Novembre", "Décembre",
}

var enMonthsLabels = [12]string{
	"January", "February", "March", "April", "May", "June",
	"July", "August", "September", "October", "November", "December",
}

func monthLabel(idx int, locale string) string {
	if idx < 0 || idx > 11 {
		return ""
	}
	if locale == "fr" {
		return frMonthsCanonical[idx]
	}
	return enMonthsLabels[idx]
}

func firstOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}

func isPersonActive(p budgetPerson, year int, monthIdx int) bool {
	return isActiveInMonth(p.StartDate, p.EndDate, year, monthIdx)
}

func isChargeActive(c budgetCharge, year int, monthIdx int) bool {
	return isActiveInMonth(c.StartDate, c.EndDate, year, monthIdx)
}

func isActiveInMonth(startISO, endISO string, year, monthIdx int) bool {
	monthStart := time.Date(year, time.Month(monthIdx+1), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, -1)
	if startISO != "" {
		if t, err := time.Parse(time.RFC3339, startISO); err == nil && t.After(monthEnd) {
			return false
		} else if err != nil {
			if t, err := time.Parse("2006-01-02", startISO); err == nil && t.After(monthEnd) {
				return false
			}
		}
	}
	if endISO != "" {
		if t, err := time.Parse(time.RFC3339, endISO); err == nil && t.Before(monthStart) {
			return false
		} else if err != nil {
			if t, err := time.Parse("2006-01-02", endISO); err == nil && t.Before(monthStart) {
				return false
			}
		}
	}
	return true
}

func sumMap(m map[string]float64) float64 {
	total := 0.0
	for _, v := range m {
		total += v
	}
	return total
}

func aggregateMonth(p *budgetPayload, when time.Time, locale string, isCurrent bool) RecapMonth {
	year := when.Year()
	monthIdx := int(when.Month()) - 1

	yearData, hasYear := p.YearlyData[fmt.Sprintf("%d", year)]

	// People income (recurring) for this month.
	baseIncome := 0.0
	for _, person := range p.People {
		if isPersonActive(person, year, monthIdx) {
			baseIncome += person.Salary
		}
	}

	// Recurring charges for this month.
	charges := 0.0
	for _, c := range p.Charges {
		if isChargeActive(c, year, monthIdx) {
			charges += c.Amount
		}
	}

	// One-time income for this month.
	oneTime := 0.0
	if list, ok := p.OneTimeIncomes[fmt.Sprintf("%d", year)]; ok && monthIdx < len(list) {
		oneTime = list[monthIdx].Amount
	}

	allocated := 0.0
	spent := 0.0
	comment := ""
	hasData := false
	if hasYear {
		if monthIdx < len(yearData.Months) {
			allocated = sumMap(yearData.Months[monthIdx])
			if allocated > 0 {
				hasData = true
			}
		}
		if monthIdx < len(yearData.Expenses) {
			spent = sumMap(yearData.Expenses[monthIdx])
			if spent > 0 {
				hasData = true
			}
		}
		if monthIdx < len(yearData.MonthComments) {
			comment = yearData.MonthComments[monthIdx]
		}
	}
	if baseIncome > 0 || charges > 0 || oneTime > 0 {
		hasData = true
	}

	totalIncome := baseIncome + oneTime
	available := totalIncome - charges
	netCashflow := totalIncome - charges - spent

	canonical := frMonthsCanonical[monthIdx]
	isLocked := p.LockedMonths[canonical]

	return RecapMonth{
		Label:             fmt.Sprintf("%s %d", monthLabel(monthIdx, locale), year),
		ShortLabel:        monthLabel(monthIdx, locale),
		Year:              year,
		MonthIdx:          monthIdx,
		BaseIncome:        baseIncome,
		OneTimeIncome:     oneTime,
		TotalIncome:       totalIncome,
		RecurringCharges:  charges,
		ProjectsAllocated: allocated,
		ProjectsSpent:     spent,
		Available:         available,
		NetSavings:        available - allocated,
		NetCashflow:       netCashflow,
		Comment:           comment,
		HasData:           hasData,
		IsLocked:          isLocked,
	}
}

func aggregateProjects(p *budgetPayload, year int, locale string) []RecapProject {
	yearData, ok := p.YearlyData[fmt.Sprintf("%d", year)]
	if !ok {
		return nil
	}

	out := make([]RecapProject, 0, len(p.Projects))
	for _, proj := range p.Projects {
		allocated := 0.0
		spent := 0.0
		for i := 0; i < 12; i++ {
			if i < len(yearData.Months) {
				allocated += yearData.Months[i][proj.ID]
			}
			if i < len(yearData.Expenses) {
				spent += yearData.Expenses[i][proj.ID]
			}
		}
		hasTarget := proj.TargetAmount > 0
		progress := 0.0
		status := "no_target"
		if hasTarget {
			progress = (allocated / proj.TargetAmount) * 100
			if progress > 100 {
				progress = 100
			}
			now := time.Now()
			expectedProgress := float64(int(now.Month())) / 12.0 * 100
			if year < now.Year() {
				expectedProgress = 100
			} else if year > now.Year() {
				expectedProgress = 0
			}
			switch {
			case progress >= expectedProgress+10:
				status = "ahead"
			case progress < expectedProgress-10:
				status = "behind"
			default:
				status = "on_track"
			}
		}
		name := proj.Label
		if name == "" {
			name = defaultProjectName(locale)
		}
		out = append(out, RecapProject{
			Name:         name,
			TargetAmount: proj.TargetAmount,
			AllocatedYTD: allocated,
			SpentYTD:     spent,
			Progress:     progress,
			Status:       status,
			HasTarget:    hasTarget,
		})
	}
	return out
}

// aggregateProjectNotes returns the per-project notes for the given month,
// in the order projects are declared in the budget. Empty notes are skipped.
func aggregateProjectNotes(p *budgetPayload, when time.Time, locale string) []ProjectNote {
	year := when.Year()
	monthIdx := int(when.Month()) - 1

	yearData, ok := p.YearlyData[fmt.Sprintf("%d", year)]
	if !ok || monthIdx >= len(yearData.ExpenseComments) {
		return nil
	}
	commentMap := yearData.ExpenseComments[monthIdx]
	if len(commentMap) == 0 {
		return nil
	}

	out := make([]ProjectNote, 0, len(commentMap))
	for _, proj := range p.Projects {
		note := strings.TrimSpace(commentMap[proj.ID])
		if note == "" {
			continue
		}
		name := proj.Label
		if name == "" {
			name = defaultProjectName(locale)
		}
		out = append(out, ProjectNote{Name: name, Note: note})
	}
	return out
}

func aggregateYearTotals(p *budgetPayload, year int) (income, expenses float64) {
	for i := 0; i < 12; i++ {
		for _, person := range p.People {
			if isPersonActive(person, year, i) {
				income += person.Salary
			}
		}
		for _, c := range p.Charges {
			if isChargeActive(c, year, i) {
				expenses += c.Amount
			}
		}
	}
	if list, ok := p.OneTimeIncomes[fmt.Sprintf("%d", year)]; ok {
		for _, item := range list {
			income += item.Amount
		}
	}
	if yearData, ok := p.YearlyData[fmt.Sprintf("%d", year)]; ok {
		for _, m := range yearData.Expenses {
			expenses += sumMap(m)
		}
	}
	return
}

// ----------------------------------------------------------------------------
// MISC
// ----------------------------------------------------------------------------

// localeForLocation maps a 2-letter ISO country to "fr" for French-speaking
// regions, "en" otherwise. Anything we don't recognise falls back to English
// to keep the recap intelligible.
func localeForLocation(loc string) string {
	switch strings.ToUpper(strings.TrimSpace(loc)) {
	case "FR", "BE", "CH", "LU", "MC", "SN", "CI", "MA", "CD", "CM", "GA", "ML", "NE", "BF", "BJ", "TG":
		return "fr"
	default:
		return "en"
	}
}

func currencySymbol(code string) string {
	switch strings.ToUpper(code) {
	case "USD", "CAD", "AUD", "NZD":
		return "$"
	case "GBP":
		return "£"
	case "CHF":
		return "CHF"
	case "EUR":
		return "€"
	case "XOF", "XAF":
		return "CFA"
	case "MAD":
		return "DH"
	case "JPY":
		return "¥"
	default:
		return code
	}
}

func defaultBudgetName(locale string) string {
	if locale == "fr" {
		return "Votre budget"
	}
	return "Your budget"
}

func defaultProjectName(locale string) string {
	if locale == "fr" {
		return "Projet"
	}
	return "Project"
}

// friendlyEmailFallback returns the local-part of an email, capitalised, to
// use as a name when we don't have one stored.
func friendlyEmailFallback(email string) string {
	at := strings.IndexByte(email, '@')
	if at <= 0 {
		return email
	}
	local := email[:at]
	if local == "" {
		return email
	}
	return strings.ToUpper(local[:1]) + local[1:]
}

// SafeLog wraps utils.SafeInfo so callers don't have to import utils just for
// the package-prefixed logging.
func (s *MonthlyRecapService) SafeLog(format string, args ...any) {
	utils.SafeInfo(format, args...)
}

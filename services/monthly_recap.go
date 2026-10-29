// services/monthly_recap.go
// ============================================================================
// MONTHLY RECAP — prepares the month ahead for a budget and produces the
// template data consumed by campaign_templates/monthly_recap.html.
// ============================================================================
//
// Sent once per month as pay lands, a few days before the month it prepares
// (monthly_recap_schedule.go), to every verified user who did not opt out,
// scoped to their most recently consulted budget. The aggregation runs
// against the stored budget_data (year-based shape + v3 rules).
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
	"github.com/pkg/errors"
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
	// Members' month (Foyer): contribution and pocket money, personal
	// charges included. Amounts only — personal charge labels stay private.
	Members         []RecapMember
	Salaries        float64
	PersonalCharges float64
	// Deep link to this month in the app.
	URL string
}

// ProjectNote is a single per-project note attached to a month, surfaced
// from the budget's expenseComments map.
type ProjectNote struct {
	Name string
	Note string
}

type RecapData struct {
	UserName       string
	BudgetName     string
	Currency       string
	CurrencySymbol string
	Locale         string // "fr" | "en"
	Year           int    // year of the month ahead
	AppURL         string
	LoginURL       string
	CampaignID     string

	// Month is the month the recap prepares: it goes out as pay lands, a few
	// days before it starts. Ending is the month ending as it goes out.
	Month  RecapMonth
	Ending RecapMonth

	// UsesContributions is true when members put only part of their salary
	// in the household pot: income figures are then pot inflows, labelled
	// accordingly in the template.
	UsesContributions bool
	// Method is the split method the contributions of Month follow (they are
	// worked out again whenever a charge, a saving or a pay changes); nil
	// when they are set by hand.
	Method *RecapMethod

	// Changes: what moves from Ending to Month, biggest first.
	Changes []RecapChange
	// Charges: the household charges of Month, heaviest first; the ones not
	// listed are summed up in MoreCharges / MoreChargesAmount.
	Charges           []RecapCharge
	MoreCharges       int
	MoreChargesAmount float64
	// Savings: the pots of Month, with what they will hold at its end.
	Savings []RecapSaving
	// Pot: the general savings (what is left of the pot every month).
	Pot RecapPot

	// BudgetURL opens Month in the app; SplitURL the Foyer split assistant.
	BudgetURL string
	SplitURL  string
	// Tip is a short "did you know" about a feature, rotating every month.
	Tip RecapTip

	// OtherBudgets is a short summary of the user's other budgets for Month
	// (capped in the caller). Empty when the user has a single budget.
	OtherBudgets []BudgetSummary

	// OtherBudgetCount is the total number of secondary budgets the user
	// owns (i.e. total minus the primary one), independent of how many we
	// display in OtherBudgets.
	OtherBudgetCount int

	// HiddenBudgetCount is the number of secondary budgets the user owns
	// beyond what's shown in OtherBudgets (e.g. user has 8 budgets, we show
	// the primary + 3 others, HiddenBudgetCount = 4).
	HiddenBudgetCount int

	// Set per recipient by the sender: the footer's way out of the recap and
	// the account page where emails are chosen.
	UnsubscribeURL string
	PreferencesURL string

	GeneratedAt string
}

// RecapTip is the "did you know" card of the recap.
type RecapTip struct {
	Emoji string
	Title string
	Body  string
	CTA   string
	URL   string
}

// BudgetSummary is the compact per-budget block we surface at the end of the
// recap email when the user owns more than one budget: its month ahead.
type BudgetSummary struct {
	ID             string
	Name           string
	Currency       string
	CurrencySymbol string
	Income         float64
	Charges        float64
	Rest           float64
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
	// Locale is the account's language ("fr" | "en"); empty on accounts made
	// before languages existed, which keep the budget country's language.
	Locale string
}

// budgetSummary is the per-user budget shortlist returned by the SQL.
type budgetSummary struct {
	ID        string
	Name      string
	Location  string
	Currency  string
	UpdatedAt time.Time
}

// ListVerifiedUsers returns the users with email_verified=true who did not
// leave the recap. Use limit > 0 to cap the result set for dry runs.
func (s *MonthlyRecapService) ListVerifiedUsers(ctx context.Context, limit int) ([]VerifiedUser, error) {
	q := `SELECT id::text, email, COALESCE(name, ''), COALESCE(locale, '') FROM users
		WHERE email_verified = TRUE AND email_monthly_recap ORDER BY created_at ASC`
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
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Locale); err != nil {
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

// BuildRecap reads the budget_data, decrypts it, and prepares the recap of
// `month` (any day in it), the month ending being the one before.
//
// `others` contains the user's secondary budgets (already filtered by the
// caller, e.g. top 3 by updated_at). Their month ahead is read here so we can
// surface them as compact cards at the end of the email.
func (s *MonthlyRecapService) BuildRecap(
	ctx context.Context,
	user VerifiedUser,
	bs *budgetSummary,
	others []budgetSummary,
	otherBudgetCount int,
	month time.Time,
	campaignID string,
	appURL string,
) (*RecapData, error) {
	raw, err := s.Budget.GetData(ctx, bs.ID)
	if err != nil {
		return nil, errors.Wrap(err, "get budget data")
	}

	// raw is interface{} — re-marshal/unmarshal into our typed payload.
	payload, err := decodeBudgetPayload(raw)
	if err != nil {
		return nil, errors.Wrap(err, "decode budget payload")
	}

	locale := recapLocale(user, bs.Location)
	currency := strings.ToUpper(strings.TrimSpace(bs.Currency))
	if currency == "" {
		currency = "EUR"
	}
	appURL = strings.TrimRight(appURL, "/")

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

	data := buildRecapData(payload, firstOfMonth(month), locale, appURL+"/budget/"+bs.ID)
	data.UserName = displayName
	data.BudgetName = budgetName
	data.Currency = currency
	data.CurrencySymbol = currencySymbol(currency)
	data.AppURL = appURL
	data.LoginURL = appURL + "/login"
	data.CampaignID = campaignID
	data.Tip = recapTip(locale, firstOfMonth(month), appURL)
	data.OtherBudgets = s.buildOtherBudgetSummaries(ctx, others, firstOfMonth(month), appURL)
	data.OtherBudgetCount = otherBudgetCount - 1
	data.HiddenBudgetCount = maxInt(0, (otherBudgetCount-1)-len(data.OtherBudgets))
	data.GeneratedAt = time.Now().Format("2006-01-02")
	return data, nil
}

// buildRecapData works out everything the recap says about a budget's
// `month` (first day), the month ending being the one before. `budgetURL` is
// the budget's address in the app.
func buildRecapData(p *budgetPayload, month time.Time, locale, budgetURL string) *RecapData {
	ending := month.AddDate(0, -1, 0)
	m := aggregateMonth(p, month, locale, true)
	e := aggregateMonth(p, ending, locale, false)
	m.ProjectNotes = aggregateProjectNotes(p, month, locale)
	monthURL := budgetURL + "/complete/month"
	for _, x := range []*RecapMonth{&m, &e} {
		x.Members = resolveMembers(p, x.Year, x.MonthIdx)
		for _, member := range x.Members {
			x.PersonalCharges += member.PersonalCharges
			x.Salaries += member.Salary
		}
		x.PersonalCharges = roundCents(x.PersonalCharges)
		x.Salaries = roundCents(x.Salaries)
		x.URL = monthURL + "?m=" + makeYM(x.Year, x.MonthIdx)
	}

	charges, more, moreAmount := recapCharges(p, m.Year, m.MonthIdx)
	_, generalNow := recapBalances(p, ending)
	pots, generalAfter := recapBalances(p, month)

	return &RecapData{
		Locale:            locale,
		Year:              m.Year,
		Month:             m,
		Ending:            e,
		UsesContributions: usesContributions(p),
		Method:            splitMethodAt(p, makeYM(m.Year, m.MonthIdx), locale),
		Changes:           recapChanges(p, ending, month, locale),
		Charges:           charges,
		MoreCharges:       more,
		MoreChargesAmount: moreAmount,
		Savings:           recapSavings(p, month, pots, locale),
		Pot:               RecapPot{Now: generalNow, After: generalAfter, Rest: roundCents(generalAfter - generalNow)},
		BudgetURL:         m.URL,
		SplitURL:          budgetURL + "/complete/members#repartition",
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// buildOtherBudgetSummaries fetches each secondary budget's data and sums up
// its month ahead. Failures are logged and skipped so a single corrupt budget
// never breaks the whole email.
func (s *MonthlyRecapService) buildOtherBudgetSummaries(
	ctx context.Context,
	others []budgetSummary,
	month time.Time,
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
		m := aggregateMonth(payload, month, "fr", false)

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
			Income:         m.TotalIncome,
			Charges:        m.RecurringCharges,
			Rest:           m.NetSavings,
			URL:            appURL + "/budget/" + b.ID + "/complete/month?m=" + makeYM(m.Year, m.MonthIdx),
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
	// v3: effective-dated salary, month-only salary, and what the member
	// actually puts in the household pot (absent = the whole salary).
	SalaryHistory         []amountStep       `json:"salaryHistory,omitempty"`
	SalaryOverrides       ymAmounts          `json:"salaryOverrides,omitempty"`
	Contributions         []contributionStep `json:"contributions,omitempty"`
	ContributionOverrides ymAmounts          `json:"contributionOverrides,omitempty"`
}

type budgetCharge struct {
	ID string `json:"id"`
	// Set on personal charges (stored under `personalCharges`): the member
	// whose pocket money pays it.
	OwnerID   string  `json:"ownerId,omitempty"`
	Label     string  `json:"label"`
	Category  string  `json:"category,omitempty"`
	Amount    float64 `json:"amount"`
	StartDate string  `json:"startDate,omitempty"`
	EndDate   string  `json:"endDate,omitempty"`
	// v3: frequency (absent = monthly), due months for "custom", yearly
	// smoothing, effective-dated amounts and month-only overrides.
	Frequency     string       `json:"frequency,omitempty"`
	Months        []int        `json:"months,omitempty"`
	Smooth        bool         `json:"smooth,omitempty"`
	AmountHistory []amountStep `json:"amountHistory,omitempty"`
	Overrides     ymAmounts    `json:"overrides,omitempty"`
}

type budgetProject struct {
	ID           string  `json:"id"`
	Label        string  `json:"label"`
	TargetAmount float64 `json:"targetAmount,omitempty"`
	// "Épargne particulière": fixed monthly amount over an optional
	// start/end window, resolved from the rule like the UI engine does.
	// nil = free saving (amount chosen month by month); set = recurring,
	// even when 0 (the UI tests the presence of the number).
	MonthlyAmount *float64 `json:"monthlyAmount,omitempty"`
	StartDate     string   `json:"startDate,omitempty"`
	EndDate       string   `json:"endDate,omitempty"`
	// v3 (allocations are still mirrored into YearlyData by the client).
	AmountHistory []amountStep `json:"amountHistory,omitempty"`
	Overrides     ymAmounts    `json:"overrides,omitempty"`
}

type budgetYear struct {
	Months          []map[string]float64 `json:"months"`
	Expenses        []map[string]float64 `json:"expenses"`
	MonthComments   []string             `json:"monthComments"`
	ExpenseComments []map[string]string  `json:"expenseComments"`
	// Per-year lock map (month name -> bool). Newer clients store locks here so
	// a lock in one year no longer bleeds into another; older payloads only
	// carry the top-level LockedMonths, used as a fallback.
	LockedMonths map[string]bool `json:"lockedMonths"`
	// v3: frozen values of closed months (index = month, null = none).
	Snapshots []*monthSnapshot `json:"snapshots,omitempty"`
}

type budgetOneTime struct {
	Amount      float64 `json:"amount"`
	Description string  `json:"description,omitempty"`
	// v3: the individual one-off incomes; Amount stays their sum.
	Items []oneOffItem `json:"items,omitempty"`
}

type budgetPayload struct {
	BudgetTitle string         `json:"budgetTitle"`
	CurrentYear int            `json:"currentYear"`
	People      []budgetPerson `json:"people"`
	Charges     []budgetCharge `json:"charges"`
	// Members' personal charges: out of their pocket money, never in the pot.
	PersonalCharges []budgetCharge             `json:"personalCharges"`
	Projects        []budgetProject            `json:"projects"`
	YearlyData      map[string]budgetYear      `json:"yearlyData"`
	OneTimeIncomes  map[string][]budgetOneTime `json:"oneTimeIncomes"`
	LockedMonths    map[string]bool            `json:"lockedMonths"`
	SchemaVersion   int                        `json:"schemaVersion,omitempty"`
	// The household's split methods over time (budget-ui split.ts).
	SplitRules splitRuleList `json:"splitRules,omitempty"`
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

	// Contributions, one-off incomes, charges and savings exactly as the app
	// computes them (frozen snapshot for closed months, rules otherwise).
	v := resolveMonthValues(p, year, monthIdx)
	baseIncome := v.Contributions
	charges := v.Charges
	oneTime := v.OneOff
	allocated := v.Savings

	spent := 0.0
	comment := ""
	hasData := false
	if allocated > 0 {
		hasData = true
	}
	if hasYear {
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

	totalIncome := roundCents(baseIncome + oneTime)
	available := roundCents(totalIncome - charges)
	netCashflow := roundCents(totalIncome - charges - spent)

	canonical := frMonthsCanonical[monthIdx]
	// Prefer the year's own lock map; fall back to the legacy top-level one.
	isLocked := p.LockedMonths[canonical]
	if hasYear && yearData.LockedMonths != nil {
		if v, ok := yearData.LockedMonths[canonical]; ok {
			isLocked = v
		}
	}

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
		NetSavings:        roundCents(available - allocated),
		NetCashflow:       netCashflow,
		Comment:           comment,
		HasData:           hasData,
		IsLocked:          isLocked,
	}
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

// ----------------------------------------------------------------------------
// MISC
// ----------------------------------------------------------------------------

// recapLocale is the language of a user's recap: their account's, else the
// one of their budget's country (accounts made before languages existed).
func recapLocale(user VerifiedUser, location string) string {
	if l := utils.NormalizeLocale(user.Locale); l != "" {
		return l
	}
	return localeForLocation(location)
}

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

// PreviewRecap builds the recap of a stored budget blob outside the database
// (cmd/recap_preview, tests): the recap of `month` for `userName`.
func PreviewRecap(blob interface{}, month time.Time, locale, userName, appURL, budgetID string) (*RecapData, error) {
	p, err := decodeBudgetPayload(blob)
	if err != nil {
		return nil, err
	}
	appURL = strings.TrimRight(appURL, "/")
	data := buildRecapData(p, firstOfMonth(month), locale, appURL+"/budget/"+budgetID)
	data.UserName = userName
	data.BudgetName = p.BudgetTitle
	if data.BudgetName == "" {
		data.BudgetName = defaultBudgetName(locale)
	}
	data.Currency, data.CurrencySymbol = "EUR", "€"
	data.AppURL, data.LoginURL = appURL, appURL+"/login"
	data.CampaignID = RecapCampaignID(month)
	data.Tip = recapTip(locale, firstOfMonth(month), appURL)
	data.PreferencesURL = appURL + "/profile#emails"
	return data, nil
}

// services/monthly_recap_v3.go
// ============================================================================
// MONTHLY RECAP — v3 budget model resolution
// ============================================================================
//
// The UI stores v3 budgets (schemaVersion 3) additively on top of the legacy
// shape: salary ≠ contribution, effective-dated amounts ("à partir de"),
// month-only overrides ("ce mois-ci seulement"), charge frequencies and
// frozen snapshots of closed months. This file mirrors the UI engine
// (budget-ui/src/lib/budget/engine.ts) so that the recap shows the same
// numbers as the app:
//
//   1. closed month with a snapshot → the frozen snapshot;
//   2. otherwise the rules, where an override beats an effective-dated step,
//      which beats the base amount. A rule amount of 0 without an override
//      means "not this month".
//
// Legacy budgets carry none of the v3 fields and resolve exactly as before
// (contribution = salary, every charge monthly, allocations from yearlyData).
// ============================================================================

package services

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

// amountStep is an effective-dated amount: applies from `From` (YYYY-MM)
// until the next step.
type amountStep struct {
	From   string  `json:"from"`
	Amount float64 `json:"amount"`
}

// contributionStep is an effective-dated contribution rule. Value is a fixed
// amount (mode "fixed") or a percentage of the salary (mode "percent").
type contributionStep struct {
	From  string   `json:"from"`
	Mode  string   `json:"mode"`
	Value *float64 `json:"value,omitempty"`
}

// ymAmounts maps YYYY-MM to a month-only amount. Non-numeric values (null)
// are dropped, as the UI ignores them.
type ymAmounts map[string]float64

func (m *ymAmounts) UnmarshalJSON(b []byte) error {
	var raw map[string]interface{}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	out := ymAmounts{}
	for k, v := range raw {
		if f, ok := v.(float64); ok && !math.IsNaN(f) && !math.IsInf(f, 0) {
			out[k] = f
		}
	}
	*m = out
	return nil
}

type oneOffItem struct {
	ID     string  `json:"id"`
	Label  string  `json:"label"`
	Amount float64 `json:"amount"`
}

// UnmarshalJSON accepts the object form and the very old bare-number form
// (`oneTimeIncomes: {"2024": [0, 150, …]}`) instead of failing the recap.
func (o *budgetOneTime) UnmarshalJSON(b []byte) error {
	var n float64
	if err := json.Unmarshal(b, &n); err == nil {
		*o = budgetOneTime{Amount: n}
		return nil
	}
	type plain budgetOneTime
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*o = budgetOneTime(p)
	return nil
}

type snapshotPerson struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Salary       float64 `json:"salary"`
	Contribution float64 `json:"contribution"`
}

// snapshotPersonal is a member's personal charge frozen with a closed month.
// Only the owner and the amount are read: labels never leave the app.
type snapshotPersonal struct {
	OwnerID string  `json:"ownerId"`
	Amount  float64 `json:"amount"`
}

type snapshotCharge struct {
	ID     string  `json:"id"`
	Amount float64 `json:"amount"`
}

type snapshotProject struct {
	ID         string  `json:"id"`
	Allocation float64 `json:"allocation"`
}

// monthSnapshot is the frozen state of a closed month.
type monthSnapshot struct {
	V        int                `json:"v"`
	People   []snapshotPerson   `json:"people"`
	Charges  []snapshotCharge   `json:"charges"`
	Projects []snapshotProject  `json:"projects"`
	OneOffs  []oneOffItem       `json:"oneOffs"`
	Personal []snapshotPersonal `json:"personal,omitempty"`
}

func (s *monthSnapshot) valid() bool {
	return s != nil && s.V == 1 && s.People != nil && s.Charges != nil && s.Projects != nil
}

// monthValues are the resolved totals of one month, as the app shows them.
type monthValues struct {
	Salaries      float64
	Contributions float64
	OneOff        float64
	Charges       float64
	Savings       float64
	Frozen        bool
}

const generalSavingsID = "epargne"

var ymRe = regexp.MustCompile(`^\d{4}-\d{2}$`)

func roundCents(v float64) float64 {
	return math.Round(v*100) / 100
}

func makeYM(year, monthIdx int) string {
	return fmt.Sprintf("%04d-%02d", year, monthIdx+1)
}

// toYM converts a stored date (YYYY-MM, YYYY-MM-DD or RFC3339) to YYYY-MM.
func toYM(date string) string {
	s := strings.TrimSpace(date)
	if s == "" {
		return ""
	}
	if ymRe.MatchString(s) {
		return s
	}
	if len(s) == 10 {
		if _, err := time.Parse("2006-01-02", s); err == nil {
			return s[:7]
		}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Format("2006-01")
	}
	return ""
}

func monthOfYM(ym string) int {
	var y, m int
	if _, err := fmt.Sscanf(ym, "%d-%d", &y, &m); err != nil {
		return 0
	}
	return m
}

func inWindowYM(startDate, endDate, ym string) bool {
	if s := toYM(startDate); s != "" && ym < s {
		return false
	}
	if e := toYM(endDate); e != "" && ym > e {
		return false
	}
	return true
}

// stepAt returns the index of the step in force at ym (the earliest step when
// ym precedes them all), or -1 when there are no steps.
func stepAt(froms []string, ym string) int {
	current := -1
	for i, f := range froms {
		if f <= ym && (current == -1 || f >= froms[current]) {
			current = i
		}
	}
	if current >= 0 || len(froms) == 0 {
		return current
	}
	earliest := 0
	for i, f := range froms {
		if f < froms[earliest] {
			earliest = i
		}
	}
	return earliest
}

func amountAt(steps []amountStep, ym string, base float64) float64 {
	froms := make([]string, len(steps))
	for i, s := range steps {
		froms[i] = s.From
	}
	if i := stepAt(froms, ym); i >= 0 {
		return roundCents(steps[i].Amount)
	}
	return roundCents(base)
}

func override(m ymAmounts, ym string) (float64, bool) {
	v, ok := m[ym]
	return roundCents(v), ok
}

// personValues returns (salary, contribution, active) for a month.
func personValues(p budgetPerson, ym string) (float64, float64, bool) {
	if !inWindowYM(p.StartDate, p.EndDate, ym) {
		return 0, 0, false
	}
	salary, ok := override(p.SalaryOverrides, ym)
	if !ok {
		salary = amountAt(p.SalaryHistory, ym, p.Salary)
	}
	contribution := salary
	froms := make([]string, len(p.Contributions))
	for i, c := range p.Contributions {
		froms[i] = c.From
	}
	if i := stepAt(froms, ym); i >= 0 {
		rule := p.Contributions[i]
		value := 0.0
		if rule.Value != nil {
			value = *rule.Value
		}
		switch rule.Mode {
		case "fixed":
			contribution = roundCents(value)
		case "percent":
			contribution = roundCents(salary * value / 100)
		}
	}
	if v, ok := override(p.ContributionOverrides, ym); ok {
		contribution = v
	}
	return salary, contribution, true
}

func chargeOccurs(c budgetCharge, ym string) bool {
	if !inWindowYM(c.StartDate, c.EndDate, ym) {
		return false
	}
	switch c.Frequency {
	case "", "monthly":
		return true
	case "custom":
		m := monthOfYM(ym)
		for _, x := range c.Months {
			if x == m {
				return true
			}
		}
		return false
	case "yearly":
		if c.Smooth {
			return true
		}
		anchor := toYM(c.StartDate)
		if anchor == "" {
			froms := make([]string, len(c.AmountHistory))
			for i, s := range c.AmountHistory {
				froms[i] = s.From
			}
			if i := stepAt(froms, ym); i >= 0 {
				anchor = c.AmountHistory[i].From
			}
		}
		if anchor == "" {
			return true
		}
		return monthOfYM(anchor) == monthOfYM(ym)
	case "once":
		start := toYM(c.StartDate)
		return start != "" && start == ym
	}
	return true
}

// chargeAmount returns the amount a charge weighs on a month (0 = absent).
func chargeAmount(c budgetCharge, ym string) float64 {
	if !chargeOccurs(c, ym) {
		return 0
	}
	planned := amountAt(c.AmountHistory, ym, c.Amount)
	if c.Frequency == "yearly" && c.Smooth {
		planned = roundCents(planned / 12)
	}
	if v, ok := override(c.Overrides, ym); ok {
		return v
	}
	return planned
}

// projectAllocation returns the allocation of a saving for a month: the rule
// for recurring savings, the stored allocation for free ones.
func projectAllocation(p budgetProject, ym string, stored map[string]float64) float64 {
	if p.MonthlyAmount == nil || math.IsNaN(*p.MonthlyAmount) {
		return roundCents(stored[p.ID])
	}
	if !inWindowYM(p.StartDate, p.EndDate, ym) {
		return 0
	}
	if v, ok := override(p.Overrides, ym); ok {
		return v
	}
	return amountAt(p.AmountHistory, ym, *p.MonthlyAmount)
}

func oneOffTotal(list []budgetOneTime, monthIdx int) float64 {
	if monthIdx >= len(list) {
		return 0
	}
	entry := list[monthIdx]
	if len(entry.Items) > 0 {
		sum := 0.0
		for _, it := range entry.Items {
			sum += it.Amount
		}
		if math.Abs(roundCents(sum)-entry.Amount) < 0.01 {
			return roundCents(sum)
		}
	}
	return roundCents(entry.Amount)
}

// snapshotFor returns the frozen snapshot of a month when it applies: the UI
// only writes snapshots for closed months and ignores them once a month has
// been explicitly reopened (lock = false).
func snapshotFor(p *budgetPayload, year, monthIdx int) *monthSnapshot {
	yd, ok := p.YearlyData[fmt.Sprintf("%d", year)]
	if !ok || monthIdx >= len(yd.Snapshots) {
		return nil
	}
	snap := yd.Snapshots[monthIdx]
	if !snap.valid() {
		return nil
	}
	if lock, ok := yd.LockedMonths[frMonthsCanonical[monthIdx]]; ok && !lock {
		return nil
	}
	return snap
}

// resolveMonthValues computes the totals of a month like the UI engine does.
func resolveMonthValues(p *budgetPayload, year, monthIdx int) monthValues {
	if snap := snapshotFor(p, year, monthIdx); snap != nil {
		v := monthValues{Frozen: true}
		for _, x := range snap.People {
			v.Salaries += x.Salary
			v.Contributions += x.Contribution
		}
		for _, x := range snap.Charges {
			v.Charges += x.Amount
		}
		for _, x := range snap.Projects {
			v.Savings += x.Allocation
		}
		for _, x := range snap.OneOffs {
			v.OneOff += x.Amount
		}
		return v.rounded()
	}

	ym := makeYM(year, monthIdx)
	v := monthValues{}
	for _, person := range p.People {
		if salary, contribution, ok := personValues(person, ym); ok {
			v.Salaries += salary
			v.Contributions += contribution
		}
	}
	for _, c := range p.Charges {
		v.Charges += chargeAmount(c, ym)
	}
	yearKey := fmt.Sprintf("%d", year)
	v.OneOff = oneOffTotal(p.OneTimeIncomes[yearKey], monthIdx)

	var stored map[string]float64
	if yd, ok := p.YearlyData[yearKey]; ok && monthIdx < len(yd.Months) {
		stored = yd.Months[monthIdx]
	}
	for _, proj := range p.Projects {
		if proj.ID == generalSavingsID {
			continue
		}
		v.Savings += projectAllocation(proj, ym, stored)
	}
	return v.rounded()
}

func (v monthValues) rounded() monthValues {
	v.Salaries = roundCents(v.Salaries)
	v.Contributions = roundCents(v.Contributions)
	v.OneOff = roundCents(v.OneOff)
	v.Charges = roundCents(v.Charges)
	v.Savings = roundCents(v.Savings)
	return v
}

// RecapMember is one member's month: salary, what they put in the household
// pot, and their pocket money (salary − contribution), which includes their
// personal charges.
type RecapMember struct {
	Name            string
	Salary          float64
	Contribution    float64
	PocketMoney     float64
	PersonalCharges float64
}

// resolveMembers returns each active member's month like the app's Foyer tab
// (frozen snapshot for closed months, rules otherwise).
func resolveMembers(p *budgetPayload, year, monthIdx int) []RecapMember {
	names := make(map[string]string, len(p.People))
	for _, person := range p.People {
		names[person.ID] = person.Name
	}
	out := []RecapMember{}
	if snap := snapshotFor(p, year, monthIdx); snap != nil {
		personal := map[string]float64{}
		for _, c := range snap.Personal {
			personal[c.OwnerID] += c.Amount
		}
		for _, x := range snap.People {
			name := x.Name
			if name == "" {
				name = names[x.ID]
			}
			out = append(out, newRecapMember(name, x.Salary, x.Contribution, personal[x.ID]))
		}
		return out
	}
	ym := makeYM(year, monthIdx)
	for _, person := range p.People {
		salary, contribution, ok := personValues(person, ym)
		if !ok {
			continue
		}
		own := 0.0
		for _, c := range p.PersonalCharges {
			if c.OwnerID == person.ID {
				own += chargeAmount(c, ym)
			}
		}
		out = append(out, newRecapMember(person.Name, salary, contribution, own))
	}
	return out
}

func newRecapMember(name string, salary, contribution, personal float64) RecapMember {
	return RecapMember{
		Name:            name,
		Salary:          roundCents(salary),
		Contribution:    roundCents(contribution),
		PocketMoney:     roundCents(salary - contribution),
		PersonalCharges: roundCents(personal),
	}
}

// usesContributions reports whether at least one member puts less (or more)
// than their whole salary in the pot, so "income" means "pot inflows".
func usesContributions(p *budgetPayload) bool {
	for _, person := range p.People {
		for _, c := range person.Contributions {
			if c.Mode == "fixed" || c.Mode == "percent" {
				return true
			}
		}
		if len(person.ContributionOverrides) > 0 {
			return true
		}
	}
	return false
}

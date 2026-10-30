// services/monthly_recap_ahead.go
// ============================================================================
// MONTHLY RECAP — the month ahead in detail
// ============================================================================
// The recap goes out as pay lands, a few days before the month it prepares.
// Besides the month's totals (monthly_recap_v3.go), it tells:
//   - what changes from the month ending (a new bill, a yearly bill due, a
//     loan that ends, a raise, a saving that starts…);
//   - where the money goes: the household's charges, heaviest first;
//   - the savings pots and the general savings (what is left of the pot every
//     month), as they will stand at the end of the month — the running totals
//     of the app's engine (BudgetEngine.savingBalance / generalBalance);
//   - the split method the contributions follow, if any.
// Like the rest of the recap it never writes, and personal charges never
// appear: they stay out of the pot.
// ============================================================================

package services

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// RecapCharge is one household charge of the month ahead.
type RecapCharge struct {
	Label   string
	Private bool // its name is private: "Charge privée" on screen
	Emoji   string
	Amount  float64
	// Bar is the width of its bar: its share of the heaviest charge (0..100).
	Bar int
	// Note: "yearly" (the yearly bill is due), "smoothed" (a yearly bill spread
	// over 12 months), "once", "custom" (some months only), "adjusted" (this
	// month only) or "".
	Note string
}

// RecapChange is something that moves between the month ending and the month
// ahead.
type RecapChange struct {
	// Kind: new | due | once | back | end | pause | up | down (charges),
	// saving_new | saving_stop | saving_up | saving_down, oneoff,
	// pay_up | pay_down (a member's contribution).
	Kind    string
	Label   string
	Private bool
	Emoji   string
	// Delta is the change's effect on what is left of the month: + leaves
	// more money in the pot (a bill that drops or ends, a member paying more,
	// a one-off income), − leaves less (a new or higher bill, more savings, a
	// member paying less). Shown green or red by its sign.
	Delta float64
	// Good: Delta > 0, it leaves more money in the pot.
	Good bool
}

// RecapSaving is a savings pot in the month ahead.
type RecapSaving struct {
	Name       string
	Allocation float64 // put aside in the month ahead
	Balance    float64 // in the pot at the end of the month ahead, as planned
	Target     float64
	HasTarget  bool
	Progress   int // 0..100, of the target
}

// RecapPot is the general savings: what is left of the pot every month.
type RecapPot struct {
	Now   float64 // at the end of the month ending
	After float64 // at the end of the month ahead, as planned
	Rest  float64 // what the month ahead adds to it (After − Now)
}

// RecapMethod is the household's split method in force in the month ahead.
type RecapMethod struct {
	Method string // prorata | equal | reste | all
	Margin int
	Since  string // label of the month it applies from
}

// recapMaxCharges is how many charges the recap lists; the others are summed up.
const recapMaxCharges = 6

// recapMaxChanges is how many changes the recap lists, biggest first.
const recapMaxChanges = 6

type splitRuleEntry struct {
	From   string   `json:"from"`
	Method *string  `json:"method"`
	Margin *float64 `json:"margin,omitempty"`
}

// splitRuleList reads the stored rules leniently: a malformed entry (or list)
// is dropped rather than failing the whole recap.
type splitRuleList []splitRuleEntry

func (l *splitRuleList) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		*l = nil
		return nil
	}
	out := splitRuleList{}
	for _, r := range raw {
		var e splitRuleEntry
		if json.Unmarshal(r, &e) == nil {
			out = append(out, e)
		}
	}
	*l = out
	return nil
}

// splitMethodAt returns the split method contributions follow in `ym`, like
// the app's splitRuleAt (nil when none, or when it was stopped).
func splitMethodAt(p *budgetPayload, ym string, locale string) *RecapMethod {
	var current *splitRuleEntry
	for i := range p.SplitRules {
		r := &p.SplitRules[i]
		if !ymRe.MatchString(r.From) || r.From > ym {
			continue
		}
		if current == nil || r.From >= current.From {
			current = r
		}
	}
	if current == nil || current.Method == nil {
		return nil
	}
	switch *current.Method {
	case "prorata", "equal", "reste", "all":
	default:
		return nil
	}
	margin := 0
	if current.Margin != nil {
		margin = int(*current.Margin)
	}
	var y, m int
	fmt.Sscanf(current.From, "%d-%d", &y, &m)
	return &RecapMethod{Method: *current.Method, Margin: margin, Since: fmt.Sprintf("%s %d", monthLabel(m-1, locale), y)}
}

// chargeAmounts returns what each household charge weighs on a month (frozen
// snapshot for closed months, rules otherwise); absent charges are left out.
func chargeAmounts(p *budgetPayload, year, monthIdx int) map[string]float64 {
	out := map[string]float64{}
	if snap := snapshotFor(p, year, monthIdx); snap != nil {
		for _, c := range snap.Charges {
			if c.Amount != 0 {
				out[c.ID] = roundCents(out[c.ID] + c.Amount)
			}
		}
		return out
	}
	ym := makeYM(year, monthIdx)
	for _, c := range p.Charges {
		if v := chargeAmount(c, ym); v != 0 {
			out[c.ID] = v
		}
	}
	return out
}

// projectAllocations returns what each saving gets in a month (frozen
// snapshot for closed months, rules or stored amounts otherwise).
func projectAllocations(p *budgetPayload, year, monthIdx int) map[string]float64 {
	out := map[string]float64{}
	if snap := snapshotFor(p, year, monthIdx); snap != nil {
		for _, x := range snap.Projects {
			out[x.ID] = roundCents(out[x.ID] + x.Allocation)
		}
		return out
	}
	ym := makeYM(year, monthIdx)
	var stored map[string]float64
	if yd, ok := p.YearlyData[fmt.Sprintf("%d", year)]; ok && monthIdx < len(yd.Months) {
		stored = yd.Months[monthIdx]
	}
	for _, proj := range p.Projects {
		if proj.ID == generalSavingsID {
			continue
		}
		out[proj.ID] = projectAllocation(proj, ym, stored)
	}
	return out
}

func monthSpent(p *budgetPayload, year, monthIdx int) map[string]float64 {
	if yd, ok := p.YearlyData[fmt.Sprintf("%d", year)]; ok && monthIdx < len(yd.Expenses) {
		return yd.Expenses[monthIdx]
	}
	return nil
}

// firstRecapMonth is January of the oldest stored year (the app's
// BudgetEngine.firstMonth), never after `year`.
func firstRecapMonth(p *budgetPayload, year int) time.Time {
	first := year
	for k := range p.YearlyData {
		var y int
		if _, err := fmt.Sscanf(k, "%d", &y); err == nil && y > 1900 && y < first {
			first = y
		}
	}
	return time.Date(first, 1, 1, 0, 0, 0, 0, time.UTC)
}

// recapBalances returns, at the end of `until`, the balance of every savings
// pot and of the general savings, as the app's engine computes them.
func recapBalances(p *budgetPayload, until time.Time) (pots map[string]float64, general float64) {
	first := firstRecapMonth(p, until.Year())
	// A recurring saving started before the oldest stored year counts from
	// January of the year it started.
	starts := map[string]time.Time{}
	for _, proj := range p.Projects {
		starts[proj.ID] = first
		if proj.MonthlyAmount != nil {
			if s := toYM(proj.StartDate); s != "" {
				var y int
				fmt.Sscanf(s, "%d", &y)
				if y > 1900 && y < first.Year() {
					starts[proj.ID] = time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
				}
			}
		}
	}
	from := first
	for _, s := range starts {
		if s.Before(from) {
			from = s
		}
	}
	end := time.Date(until.Year(), until.Month(), 1, 0, 0, 0, 0, time.UTC)
	pots = map[string]float64{}
	for t := from; !t.After(end); t = t.AddDate(0, 1, 0) {
		y, i := t.Year(), int(t.Month())-1
		alloc := projectAllocations(p, y, i)
		spent := monthSpent(p, y, i)
		for _, proj := range p.Projects {
			if proj.ID == generalSavingsID || t.Before(starts[proj.ID]) {
				continue
			}
			pots[proj.ID] += alloc[proj.ID] - spent[proj.ID]
		}
		if t.Before(first) {
			continue
		}
		v := resolveMonthValues(p, y, i)
		general += v.Contributions + v.OneOff - v.Charges - v.Savings - spent[generalSavingsID]
	}
	for k, v := range pots {
		pots[k] = roundCents(v)
	}
	return pots, roundCents(general)
}

var categoryEmojis = map[string]string{
	"HOUSING": "🏠", "ENERGY": "⚡", "INTERNET": "🌐", "MOBILE": "📱", "FOOD": "🛒",
	"CHILDREN": "🧸", "EDUCATION": "🎒", "TRANSPORT": "🚗", "INSURANCE_HOME": "🛡️",
	"INSURANCE_AUTO": "🛡️", "INSURANCE_HEALTH": "🩺", "INSURANCE": "🛡️", "LOAN": "🏦",
	"TAXES": "🧾", "SUBSCRIPTION": "🔁", "LEISURE_STREAMING": "📺", "LEISURE_SPORT": "🏋️",
	"LEISURE": "🎟️", "BANK": "🏦", "HEALTH": "🩺",
}

func categoryEmoji(category string) string {
	if e, ok := categoryEmojis[strings.ToUpper(strings.TrimSpace(category))]; ok {
		return e
	}
	return "🧾"
}

// privateChargeLabel is the name a private charge carries in the shared data
// (budget-ui PRIVATE_CHARGE_LABEL): the real one stays with its owner.
const privateChargeLabel = "Charge privée"

// recapCharges lists the household charges of a month, heaviest first, and
// sums up the ones left out.
func recapCharges(p *budgetPayload, year, monthIdx int) (list []RecapCharge, more int, moreAmount float64) {
	amounts := chargeAmounts(p, year, monthIdx)
	ym := makeYM(year, monthIdx)
	for _, c := range p.Charges {
		a, ok := amounts[c.ID]
		if !ok || a <= 0 {
			continue
		}
		note := ""
		switch c.Frequency {
		case "yearly":
			if c.Smooth {
				note = "smoothed"
			} else {
				note = "yearly"
			}
		case "once":
			note = "once"
		case "custom":
			note = "custom"
		}
		if v, ok := override(c.Overrides, ym); ok && v != roundCents(amountAt(c.AmountHistory, ym, c.Amount)) {
			note = "adjusted"
		}
		list = append(list, RecapCharge{
			Label: c.Label, Private: c.Label == privateChargeLabel, Emoji: categoryEmoji(c.Category),
			Amount: a, Note: note,
		})
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Amount > list[j].Amount })
	if len(list) > recapMaxCharges {
		for _, c := range list[recapMaxCharges:] {
			more++
			moreAmount += c.Amount
		}
		list = list[:recapMaxCharges]
	}
	if len(list) > 0 {
		heaviest := list[0].Amount
		for i := range list {
			list[i].Bar = int(math.Max(4, math.Round(list[i].Amount/heaviest*100)))
		}
	}
	return list, more, roundCents(moreAmount)
}

// recapChanges tells what moves between `ending` and `month`, biggest first.
func recapChanges(p *budgetPayload, ending, month time.Time, locale string) []RecapChange {
	ey, ei := ending.Year(), int(ending.Month())-1
	my, mi := month.Year(), int(month.Month())-1
	endingYM, monthYM := makeYM(ey, ei), makeYM(my, mi)
	out := []RecapChange{}

	before, after := chargeAmounts(p, ey, ei), chargeAmounts(p, my, mi)
	for _, c := range p.Charges {
		a, b := before[c.ID], after[c.ID]
		if math.Abs(b-a) < 0.005 {
			continue
		}
		kind := ""
		switch {
		case a == 0:
			switch {
			case c.Frequency == "once":
				kind = "once"
			case c.Frequency == "yearly" && !c.Smooth:
				kind = "due"
			case toYM(c.StartDate) == monthYM:
				kind = "new"
			default:
				kind = "back"
			}
		case b == 0:
			if e := toYM(c.EndDate); e != "" && e <= endingYM {
				kind = "end"
			} else {
				kind = "pause"
			}
		case b > a:
			kind = "up"
		default:
			kind = "down"
		}
		out = append(out, RecapChange{
			Kind: kind, Label: c.Label, Private: c.Label == privateChargeLabel, Emoji: categoryEmoji(c.Category),
			Delta: roundCents(a - b), Good: b < a,
		})
	}

	sBefore, sAfter := projectAllocations(p, ey, ei), projectAllocations(p, my, mi)
	for _, proj := range p.Projects {
		if proj.ID == generalSavingsID {
			continue
		}
		a, b := sBefore[proj.ID], sAfter[proj.ID]
		if math.Abs(b-a) < 0.005 {
			continue
		}
		kind := "saving_up"
		switch {
		case a == 0:
			kind = "saving_new"
		case b == 0:
			kind = "saving_stop"
		case b < a:
			kind = "saving_down"
		}
		name := proj.Label
		if name == "" {
			name = defaultProjectName(locale)
		}
		out = append(out, RecapChange{Kind: kind, Label: name, Emoji: "🎯", Delta: roundCents(a - b), Good: b < a})
	}

	was := map[string]float64{}
	for _, m := range resolveMembers(p, ey, ei) {
		was[m.ID] = m.Contribution
	}
	for _, m := range resolveMembers(p, my, mi) {
		a, ok := was[m.ID]
		if !ok || math.Abs(m.Contribution-a) < 0.005 {
			continue
		}
		kind := "pay_up"
		if m.Contribution < a {
			kind = "pay_down"
		}
		out = append(out, RecapChange{Kind: kind, Label: m.Name, Emoji: "👤", Delta: roundCents(m.Contribution - a), Good: m.Contribution > a})
	}

	if list, ok := p.OneTimeIncomes[fmt.Sprintf("%d", my)]; ok && mi < len(list) {
		entry := list[mi]
		if len(entry.Items) > 0 {
			for _, it := range entry.Items {
				if it.Amount != 0 {
					out = append(out, RecapChange{Kind: "oneoff", Label: it.Label, Emoji: "💶", Delta: roundCents(it.Amount), Good: it.Amount > 0})
				}
			}
		} else if entry.Amount != 0 {
			out = append(out, RecapChange{Kind: "oneoff", Label: entry.Description, Emoji: "💶", Delta: roundCents(entry.Amount), Good: entry.Amount > 0})
		}
	}

	sort.SliceStable(out, func(i, j int) bool { return math.Abs(out[i].Delta) > math.Abs(out[j].Delta) })
	if len(out) > recapMaxChanges {
		out = out[:recapMaxChanges]
	}
	return out
}

// recapSavings lists the savings pots of the month ahead with what they will
// hold at its end.
func recapSavings(p *budgetPayload, month time.Time, pots map[string]float64, locale string) []RecapSaving {
	alloc := projectAllocations(p, month.Year(), int(month.Month())-1)
	out := []RecapSaving{}
	for _, proj := range p.Projects {
		if proj.ID == generalSavingsID {
			continue
		}
		a, balance := alloc[proj.ID], pots[proj.ID]
		if a <= 0 && balance <= 0 {
			continue
		}
		name := proj.Label
		if name == "" {
			name = defaultProjectName(locale)
		}
		s := RecapSaving{Name: name, Allocation: a, Balance: balance, Target: proj.TargetAmount, HasTarget: proj.TargetAmount > 0}
		if s.HasTarget {
			s.Progress = int(math.Min(100, math.Max(0, math.Round(balance/proj.TargetAmount*100))))
		}
		out = append(out, s)
	}
	return out
}

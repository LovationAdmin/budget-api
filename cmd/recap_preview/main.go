// cmd/recap_preview/main.go
// ============================================================================
// Renders the monthly recap of the demo household (services/testdata/
// recap_demo.json) and writes the HTML and its text part to disk.
// Run with: go run ./cmd/recap_preview -locale fr -month 2026-11 -out /tmp
// ============================================================================

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/LovationAdmin/budget-api/services"
	"github.com/LovationAdmin/budget-api/utils"
)

func main() {
	locale := flag.String("locale", "fr", "fr | en")
	month := flag.String("month", "2026-11", "month the recap prepares (YYYY-MM)")
	blobPath := flag.String("budget", "services/testdata/recap_demo.json", "stored budget blob (JSON)")
	out := flag.String("out", ".", "output directory")
	flag.Parse()

	m, err := time.Parse("2006-01", *month)
	fail(err)
	raw, err := os.ReadFile(*blobPath)
	fail(err)
	var blob map[string]interface{}
	fail(json.Unmarshal(raw, &blob))
	if *blobPath == "services/testdata/recap_demo.json" {
		demoMonthAhead(blob)
	}

	data, err := services.PreviewRecap(blob, m, *locale, "Camille", "https://budgetfamille.com", "demo")
	fail(err)
	data.UnsubscribeURL = utils.UnsubscribeLinksFor("00000000-0000-0000-0000-000000000000", utils.EmailListMonthlyRecap).Page
	data.OtherBudgets = []services.BudgetSummary{{Name: "Studio Lyon", CurrencySymbol: "€", Income: 720, Charges: 540, Rest: 180, URL: "https://budgetfamille.com/budget/b2/complete/month"}}
	data.OtherBudgetCount = 1

	subject, html, err := utils.RenderMonthlyRecapEmail(*locale, data.Month.ShortLabel, data.BudgetName, data)
	fail(err)
	base := filepath.Join(*out, "recap_"+*locale)
	fail(os.WriteFile(base+".html", []byte(html), 0o644))
	fail(os.WriteFile(base+".txt", []byte(utils.PlainTextOf(html)), 0o644))
	fmt.Fprintf(os.Stderr, "SUBJECT: %s\nwrote %s.html (%d KB) and %s.txt\n", subject, base, len(html)/1024, base)
}

// demoMonthAhead gives the demo household what a real one carries: the split
// method in force and the amounts the app worked out with it for November
// (the yearly property tax falls due), plus a note for the month.
func demoMonthAhead(blob map[string]interface{}) {
	blob["splitRules"] = []interface{}{map[string]interface{}{"from": "2026-09", "method": "prorata", "margin": 0, "basis": "month"}}
	steps := map[string][]interface{}{
		"c": {step("2026-01", 1700), step("2026-11", 2041), step("2026-12", 1481)},
		"m": {step("2026-01", 1150), step("2026-11", 1531), step("2026-12", 1111)},
	}
	for _, p := range blob["people"].([]interface{}) {
		person := p.(map[string]interface{})
		person["contributions"] = steps[person["id"].(string)]
	}
	blob["yearlyData"] = map[string]interface{}{"2026": map[string]interface{}{
		"monthComments":   []interface{}{"", "", "", "", "", "", "", "", "", "", "Anniversaire de Léa le 14 : prévoir le gâteau 🎂", ""},
		"expenseComments": []interface{}{},
	}}
}

func step(from string, value float64) map[string]interface{} {
	return map[string]interface{}{"from": from, "mode": "fixed", "value": value}
}

func fail(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

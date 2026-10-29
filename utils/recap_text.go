// utils/recap_text.go
// ============================================================================
// MONTHLY RECAP — the words, in each language
// ============================================================================
// One template (campaign_templates/monthly_recap.html) for every language:
// its words come from here through the `t` (text) and `th` (text with
// markup, arguments escaped) template functions. A missing key fails the
// render, so a language can never ship half translated.
// ============================================================================

package utils

import (
	"fmt"
	"html/template"
	"strings"
	"time"
)

var recapText = map[string]map[string]string{
	LocaleFR: {
		"subject":            "Votre mois %s est prêt · %s",
		"title":              "Votre mois %s — Budget Famille",
		"preheader.members":  "%s au pot commun. Reste prévu : %s.",
		"preheader.plain":    "Charges %s, épargne %s, reste prévu %s : tout %s en un coup d’œil.",
		"hero.pill":          "Du 1ᵉʳ au %d %s",
		"hero.hello":         "Bonjour %s,",
		"hero.title":         "%s est prêt.",
		"hero.lead":          "Les salaires arrivent : voici ce que prévoit <strong style=\"color:#ffffff;\">%s</strong> pour le mois qui commence.",
		"hero.rest":          "Reste prévu en fin de mois",
		"hero.covered":       "✓ Charges et épargne couvertes",
		"hero.short":         "Il manque %s pour tout couvrir",
		"hero.inflow.pot":    "Entrées du pot commun",
		"hero.inflow.income": "Revenus du mois",
		"hero.oneoff":        "dont %s de revenus ponctuels",
		"legend.charges":     "Charges",
		"legend.savings":     "Épargne",
		"legend.rest":        "Reste",
		"legend.short":       "Manque",

		"members.title":       "Les virements du mois",
		"members.lead.pot":    "Ce que chacun verse au pot commun en %s, dès que le salaire tombe.",
		"members.lead.salary": "Les salaires qui font vivre le budget en %s.",
		"members.pays":        "à verser",
		"members.salary":      "salaire",
		"members.pocket":      "Argent de poche : %s",
		"members.personal":    "dont %s de charges perso",
		"members.total":       "Total au pot commun",
		"members.and":         "et",
		"method.prorata":      "Au prorata des salaires",
		"method.equal":        "À parts égales",
		"method.reste":        "Même argent de poche pour chacun",
		"method.all":          "Tout le salaire au pot commun",
		"method.margin":       "marge %d %%",
		"method.live":         "Recalculé tout seul dès qu’une charge, une épargne ou un salaire change.",
		"method.manual":       "Montants fixés à la main.",
		"method.manual_cta":   "Les calculer automatiquement",

		"changes.title":            "Ce qui change en %s",
		"changes.lead":             "Par rapport à %s.",
		"changes.none":             "Rien ne bouge par rapport à %s : mêmes charges, même épargne. 👌",
		"changes.effect":           "Effet sur le reste du mois : %s par rapport à %s.",
		"changes.kind.new":         "Nouvelle charge",
		"changes.kind.due":         "Échéance annuelle",
		"changes.kind.once":        "Dépense ponctuelle",
		"changes.kind.back":        "Revient ce mois-ci",
		"changes.kind.end":         "Terminée, plus rien à payer",
		"changes.kind.pause":       "Pas ce mois-ci",
		"changes.kind.up":          "En hausse",
		"changes.kind.down":        "En baisse",
		"changes.kind.saving_new":  "Nouvelle épargne",
		"changes.kind.saving_stop": "Épargne en pause",
		"changes.kind.saving_up":   "Épargne en hausse",
		"changes.kind.saving_down": "Épargne en baisse",
		"changes.kind.oneoff":      "Revenu ponctuel",
		"changes.kind.pay_up":      "Verse plus au pot commun",
		"changes.kind.pay_down":    "Verse moins au pot commun",
		"changes.oneoff":           "Revenu ponctuel",

		"charges.title":         "Où va l’argent",
		"charges.lead":          "Les charges du foyer en %s, des plus lourdes aux plus légères.",
		"charges.note.yearly":   "Échéance annuelle",
		"charges.note.smoothed": "Lissée sur 12 mois",
		"charges.note.once":     "Une seule fois",
		"charges.note.custom":   "Certains mois",
		"charges.note.adjusted": "Ajustée ce mois-ci",
		"charges.more1":         "+ 1 autre charge",
		"charges.moreN":         "+ %d autres charges",
		"charges.total":         "Total des charges",
		"charges.private":       "Charge privée",
		"charges.none":          "Aucune charge prévue en %s.",

		"savings.title": "Votre épargne",
		"savings.lead":  "Ce que chaque projet aura de côté fin %s.",
		"savings.month": "+%s ce mois-ci",
		"savings.of":    "%s sur %s · %d %%",
		"savings.saved": "%s de côté",
		"savings.done":  "Objectif atteint 🎉",

		"pot.title": "Votre épargne générale",
		"pot.lead":  "Ce qui reste du pot commun chaque mois s’y accumule.",
		"pot.now":   "Fin %s",
		"pot.after": "Fin %s (prévu)",
		"pot.low":   "Elle passerait sous zéro : un coup d’œil à la répartition s’impose.",

		"notes.title": "Vos notes pour %s",
		"cta.open":    "Ouvrir %s dans l’app",
		"cta.split":   "Ajuster la répartition",
		"tip.eyebrow": "💡 Le saviez-vous ?",

		"others.title":   "Vos autres budgets",
		"others.lead":    "Leur mois %s.",
		"others.line":    "Entrées %s · Charges %s · Reste",
		"others.open":    "Ouvrir →",
		"others.hidden1": "Et 1 autre budget dans l’app.",
		"others.hiddenN": "Et %d autres budgets dans l’app.",

		"sign.reply": "Une question, une idée ? Répondez à cet e-mail : je lis tout, personnellement.",
		"sign.role":  "Fondateur de Budget Famille",

		"footer.why":         "Vous recevez ce récap chaque mois, quand les salaires tombent, parce que vous avez un compte Budget Famille.",
		"footer.unsubscribe": "Ne plus recevoir le récap mensuel",
		"footer.prefs":       "Gérer mes e-mails",
	},
	LocaleEN: {
		"subject":            "Your %s is ready · %s",
		"title":              "Your %s — Budget Famille",
		"preheader.members":  "%s into the shared pot. Left at month end: %s.",
		"preheader.plain":    "Bills %s, savings %s, left %s: all of %s at a glance.",
		"hero.pill":          "%[2]s 1 – %[1]d",
		"hero.hello":         "Hi %s,",
		"hero.title":         "%s is ready.",
		"hero.lead":          "Pay day is here: this is what <strong style=\"color:#ffffff;\">%s</strong> has planned for the month ahead.",
		"hero.rest":          "Left at the end of the month",
		"hero.covered":       "✓ Bills and savings covered",
		"hero.short":         "%s short of covering everything",
		"hero.inflow.pot":    "Into the shared pot",
		"hero.inflow.income": "Income this month",
		"hero.oneoff":        "incl. %s one-off income",
		"legend.charges":     "Bills",
		"legend.savings":     "Savings",
		"legend.rest":        "Left",
		"legend.short":       "Short",

		"members.title":       "This month’s transfers",
		"members.lead.pot":    "What each of you pays into the shared pot in %s, as soon as pay lands.",
		"members.lead.salary": "The pay that funds the budget in %s.",
		"members.pays":        "to transfer",
		"members.salary":      "pay",
		"members.pocket":      "Spending money: %s",
		"members.personal":    "incl. %s personal bills",
		"members.total":       "Total into the pot",
		"members.and":         "and",
		"method.prorata":      "In proportion to pay",
		"method.equal":        "Equal shares",
		"method.reste":        "Same spending money for everyone",
		"method.all":          "All pay into the shared pot",
		"method.margin":       "%d%% margin",
		"method.live":         "Worked out again as soon as a bill, a saving or a pay changes.",
		"method.manual":       "Amounts set by hand.",
		"method.manual_cta":   "Work them out automatically",

		"changes.title":            "What changes in %s",
		"changes.lead":             "Compared with %s.",
		"changes.none":             "Nothing moves compared with %s: same bills, same savings. 👌",
		"changes.effect":           "Effect on what’s left: %s compared with %s.",
		"changes.kind.new":         "New bill",
		"changes.kind.due":         "Yearly bill due",
		"changes.kind.once":        "One-off expense",
		"changes.kind.back":        "Back this month",
		"changes.kind.end":         "Over, nothing more to pay",
		"changes.kind.pause":       "Not this month",
		"changes.kind.up":          "Going up",
		"changes.kind.down":        "Going down",
		"changes.kind.saving_new":  "New savings",
		"changes.kind.saving_stop": "Savings paused",
		"changes.kind.saving_up":   "Saving more",
		"changes.kind.saving_down": "Saving less",
		"changes.kind.oneoff":      "One-off income",
		"changes.kind.pay_up":      "Pays more into the pot",
		"changes.kind.pay_down":    "Pays less into the pot",
		"changes.oneoff":           "One-off income",

		"charges.title":         "Where the money goes",
		"charges.lead":          "The household’s bills in %s, biggest first.",
		"charges.note.yearly":   "Yearly bill",
		"charges.note.smoothed": "Spread over 12 months",
		"charges.note.once":     "Just once",
		"charges.note.custom":   "Some months",
		"charges.note.adjusted": "Adjusted this month",
		"charges.more1":         "+ 1 more bill",
		"charges.moreN":         "+ %d more bills",
		"charges.total":         "Total bills",
		"charges.private":       "Private expense",
		"charges.none":          "No bills planned in %s.",

		"savings.title": "Your savings",
		"savings.lead":  "What each pot will hold at the end of %s.",
		"savings.month": "+%s this month",
		"savings.of":    "%s of %s · %d%%",
		"savings.saved": "%s saved",
		"savings.done":  "Goal reached 🎉",

		"pot.title": "Your general savings",
		"pot.lead":  "Whatever is left in the shared pot each month adds up here.",
		"pot.now":   "End of %s",
		"pot.after": "End of %s (planned)",
		"pot.low":   "It would drop below zero: worth a look at the split.",

		"notes.title": "Your notes for %s",
		"cta.open":    "Open %s in the app",
		"cta.split":   "Adjust the split",
		"tip.eyebrow": "💡 Did you know?",

		"others.title":   "Your other budgets",
		"others.lead":    "Their %s.",
		"others.line":    "In %s · Bills %s · Left",
		"others.open":    "Open →",
		"others.hidden1": "And 1 more budget in the app.",
		"others.hiddenN": "And %d more budgets in the app.",

		"sign.reply": "A question, an idea? Just reply: I read everything myself.",
		"sign.role":  "Founder of Budget Famille",

		"footer.why":         "You get this recap every month as pay lands, because you have a Budget Famille account.",
		"footer.unsubscribe": "Stop the monthly recap",
		"footer.prefs":       "Manage my emails",
	},
}

// recapT returns the text of `key` in `locale`, formatted with `args`.
func recapT(locale, key string, args ...interface{}) (string, error) {
	words, ok := recapText[locale]
	if !ok {
		words = recapText[LocaleEN]
	}
	s, ok := words[key]
	if !ok {
		return "", fmt.Errorf("recap text %q missing in %q", key, locale)
	}
	if len(args) == 0 {
		return s, nil
	}
	return fmt.Sprintf(s, args...), nil
}

// recapFuncs are the template helpers of the recap, bound to its language.
func recapFuncs(locale string) template.FuncMap {
	funcs := monthlyRecapFuncs()
	funcs["t"] = func(key string, args ...interface{}) (string, error) { return recapT(locale, key, args...) }
	// th: our own markup around escaped arguments.
	funcs["th"] = func(key string, args ...interface{}) (template.HTML, error) {
		safe := make([]interface{}, len(args))
		for i, a := range args {
			if s, ok := a.(string); ok {
				safe[i] = template.HTMLEscapeString(s)
			} else {
				safe[i] = a
			}
		}
		s, err := recapT(locale, key, safe...)
		return template.HTML(s), err
	}
	// deM: "de novembre" / "d’avril" in French, the bare name in English.
	funcs["deM"] = func(name string) string {
		if locale == LocaleFR {
			return deMonth(name)
		}
		return name
	}
	// mname: a month name inside a sentence ("novembre", "November").
	funcs["mname"] = func(name string) string {
		if locale == LocaleFR {
			return strings.ToLower(name)
		}
		return name
	}
	funcs["signedMoney"] = signedMoney
	// pos: the amount shows as positive or zero once rounded to the unit.
	funcs["pos"] = func(amount float64) bool { return amount > -0.5 }
	funcs["daysIn"] = func(year, monthIdx int) int {
		return time.Date(year, time.Month(monthIdx+2), 0, 0, 0, 0, 0, time.UTC).Day()
	}
	funcs["initial"] = func(name string) string {
		for _, r := range strings.TrimSpace(name) {
			return strings.ToUpper(string(r))
		}
		return "?"
	}
	// avatar: colours of a member's initial (our own CSS).
	funcs["avatar"] = func(i int) template.CSS { return template.CSS(avatarTones[i%len(avatarTones)]) }
	funcs["utm"] = func(link, campaign, content string) string { return withUTM(link, campaign, content) }
	funcs["sub"] = func(a, b float64) float64 { return a - b }
	funcs["neg"] = func(a float64) float64 { return -a }
	// joinNames: "Camille 1 700 € et Mehdi 1 150 €".
	funcs["joinNames"] = func(parts []string) string {
		and, _ := recapT(locale, "members.and")
		switch len(parts) {
		case 0:
			return ""
		case 1:
			return parts[0]
		}
		return strings.Join(parts[:len(parts)-1], ", ") + " " + and + " " + parts[len(parts)-1]
	}
	funcs["list"] = func() []string { return nil }
	funcs["append"] = func(l []string, s string) []string { return append(l, s) }
	return funcs
}

// avatarTones: background and text of each member's initial.
var avatarTones = []string{
	"background-color:#e0f2fe; color:#075985;",
	"background-color:#ede9fe; color:#5b21b6;",
	"background-color:#fef3c7; color:#92400e;",
	"background-color:#dcfce7; color:#166534;",
	"background-color:#ffe4e6; color:#9f1239;",
}

// signedMoney: "+120 €", "−80 €", "0 €".
func signedMoney(amount float64, symbol string) string {
	if amount >= 0.5 {
		return "+" + formatMoney(amount, symbol)
	}
	return formatMoney(amount, symbol)
}

// withUTM tags a link of the recap, keeping its #fragment last.
func withUTM(link, campaign, content string) string {
	frag := ""
	if i := strings.IndexByte(link, '#'); i >= 0 {
		link, frag = link[:i], link[i:]
	}
	sep := "?"
	if strings.Contains(link, "?") {
		sep = "&"
	}
	return link + sep + "utm_source=email&utm_medium=monthly_recap&utm_campaign=" + campaign + "&utm_content=" + content + frag
}

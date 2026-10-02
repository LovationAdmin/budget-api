// services/recap_tips.go
// ============================================================================
// MONTHLY RECAP — "did you know" tips
// ============================================================================
// One short tip per recap, rotating with the month, so every email teaches
// one feature that makes the app more useful.
// ============================================================================

package services

import "time"

type tipText struct {
	Emoji, Title, Body, CTA, Path string
}

var recapTipsFR = []tipText{
	{"🧾", "Les charges perso", "Un impôt, un envoi d'argent à la famille ? Ajoutez-le en charge perso : il se déduit de votre argent de poche, jamais du pot commun. En privé, son nom n'est visible que par vous.", "Ajouter une charge perso", "/dashboard"},
	{"🎯", "Un objectif, une date", "Indiquez combien il vous faut et pour quand : Budget Famille calcule le montant à mettre de côté chaque mois et vérifie qu'il est tenable.", "Créer une cagnotte", "/dashboard"},
	{"⚖️", "Une répartition juste", "50/50, au prorata des salaires ou même argent de poche : l'assistant « Répartir le pot commun » calcule la part de chacun sur le mois réel.", "Répartir le pot commun", "/dashboard"},
	{"🤖", "Le Budget IA", "Décrivez votre foyer et vos objectifs : l'IA propose une répartition et un plan d'épargne. Rien ne change tant que vous ne validez pas.", "Essayer le Budget IA", "/dashboard"},
	{"🌙", "Le mode sombre", "Plus doux le soir, plus lisible au soleil : choisissez clair, sombre ou automatique depuis le menu de votre compte.", "Ouvrir Budget Famille", "/dashboard"},
	{"↩️", "Le droit à l'erreur", "Chaque modification s'enregistre en une seconde, et le bouton « Annuler » la défait aussi vite. Les mois clôturés, eux, ne bougent plus.", "Ouvrir mon budget", "/dashboard"},
}

var recapTipsEN = []tipText{
	{"🧾", "Personal charges", "A tax or money sent to family? Add it as a personal charge: it comes out of your spending money, never out of the shared pot. Set it private and only you can see its name.", "Add a personal charge", "/dashboard"},
	{"🎯", "A goal, a date", "Say how much you need and by when: Budget Famille works out the monthly amount and checks it fits your budget.", "Create a savings pot", "/dashboard"},
	{"⚖️", "A fair split", "50/50, in proportion to salaries or equal spending money: the split assistant works out everyone's share on the real month.", "Split the pot", "/dashboard"},
	{"🤖", "AI Budget", "Describe your household and goals: the AI suggests a split and a savings plan. Nothing changes until you approve it.", "Try AI Budget", "/dashboard"},
	{"🌙", "Dark mode", "Easier on the eyes at night: pick light, dark or automatic from your account menu.", "Open Budget Famille", "/dashboard"},
	{"↩️", "Undo anything", "Every change is saved in a second, and « Undo » reverts it just as fast. Closed months never move.", "Open my budget", "/dashboard"},
}

// recapTip returns the tip for the month the recap is sent in.
func recapTip(locale string, sentAt time.Time, appURL string) RecapTip {
	list := recapTipsEN
	if locale == "fr" {
		list = recapTipsFR
	}
	t := list[(int(sentAt.Month())+sentAt.Year()*12)%len(list)]
	return RecapTip{Emoji: t.Emoji, Title: t.Title, Body: t.Body, CTA: t.CTA, URL: appURL + t.Path}
}

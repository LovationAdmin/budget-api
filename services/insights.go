package services

import (
    "budget-api/models"
    "strings"
)

func AnalyzeCharge(charge models.Charge) *models.Suggestion {
    label := strings.ToLower(charge.Label)
    amount := charge.Amount

    // Règle 1 : Téléphonie Mobile
    // On détecte les mots clés ou la catégorie Bridge si dispo
    if strings.Contains(label, "mobile") || strings.Contains(label, "forfait") || strings.Contains(label, "sfr") || strings.Contains(label, "orange") {
        if amount > 30.00 {
            return &models.Suggestion{
                Type:             "MOBILE_OFFER",
                Title:            "Optimisation Mobile",
                Message:          "Votre forfait semble cher (> 30€). Il existe des offres dès 10€/mois sans engagement.",
                PotentialSavings: (amount - 15.00) * 12, // Économie annuelle estimée
                ActionLink:       "https://lien-comparateur-ou-affiliation.com/mobile",
                CanBeContacted:   true, // On peut proposer l'option courtier ici
            }
        }
    }

    // Règle 2 : Assurance Habitation (Exemple)
    if strings.Contains(label, "assurance") || strings.Contains(label, "axa") || strings.Contains(label, "macif") {
        if amount > 50.00 {
            return &models.Suggestion{
                Type:             "INSURANCE_OFFER",
                Title:            "Assurance Habitation",
                Message:          "Comparez votre assurance. Vous pourriez économiser en gardant les mêmes garanties.",
                PotentialSavings: (amount * 0.20) * 12, // Hypothèse de 20% d'économie
                ActionLink:       "https://lien-comparateur.com/assurance",
                CanBeContacted:   false, // Juste un lien web, pas d'appel
            }
        }
    }

    return nil
}
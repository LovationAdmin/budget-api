package services

import (
	"budget-api/models"
	"fmt"
	"strings"
)

type SuggestionService struct{}

func NewSuggestionService() *SuggestionService {
	return &SuggestionService{}
}

// Logic pour déterminer si une dépense mérite une affiliation
func (s *SuggestionService) AnalyzeCharges(charges []models.Charge) []models.Suggestion {
	var suggestions []models.Suggestion

	// 1. Groupement pour les dépenses "Foyer" (ex: Energie, Internet)
	// On veut savoir si le foyer paie trop cher au total, même si c'est divisé en deux prélèvements
	householdCategories := map[string]float64{
		"ENERGY":   0,
		"INTERNET": 0,
		"INSURANCE": 0, // Souvent habitation = foyer
	}

	// 2. Analyse Individuelle (ex: Mobile, Sport)
	// On analyse chaque ligne séparément
	for _, c := range charges {
		category := strings.ToUpper(c.Category)
		
		// Si c'est une catégorie foyer, on cumule d'abord
		if _, isHousehold := householdCategories[category]; isHousehold {
			householdCategories[category] += c.Amount
		} else {
			// Analyse immédiate pour les charges individuelles
			if sugg := s.evaluateIndividualCharge(c); sugg != nil {
				suggestions = append(suggestions, *sugg)
			}
		}
	}

	// 3. Analyse des totaux Foyer
	for cat, totalAmount := range householdCategories {
		if sugg := s.evaluateHouseholdTotal(cat, totalAmount); sugg != nil {
			suggestions = append(suggestions, *sugg)
		}
	}

	return suggestions
}

func (s *SuggestionService) evaluateIndividualCharge(c models.Charge) *models.Suggestion {
	// MOBILE : Si > 25€, c'est cher aujourd'hui
	if c.Category == "MOBILE" && c.Amount > 25 {
		return &models.Suggestion{
			ID:               "sug_" + c.ID,
			ChargeID:         c.ID,
			Type:             "MOBILE_SWITCH",
			Title:            "Forfait Mobile Optimisable",
			Message:          fmt.Sprintf("Vous payez %.0f€/mois. Des forfaits 50Go existent dès 10€.", c.Amount),
			PotentialSavings: (c.Amount - 10) * 12,
			ActionLink:       "https://www.ariase.com/mobile", // À remplacer par lien tracké
			CanBeContacted:   false,
		}
	}
	// LOAN : Si > 500€, vérifier l'assurance emprunteur
	if c.Category == "LOAN" && c.Amount > 500 {
		return &models.Suggestion{
			ID:               "sug_" + c.ID,
			ChargeID:         c.ID,
			Type:             "LOAN_INSURANCE",
			Title:            "Assurance Emprunteur",
			Message:          "Sur un gros crédit, changer d'assurance peut rapporter gros.",
			PotentialSavings: 1500, // Estimation
			ActionLink:       "https://www.meilleurtaux.com/",
			CanBeContacted:   true,
		}
	}
	return nil
}

func (s *SuggestionService) evaluateHouseholdTotal(category string, amount float64) *models.Suggestion {
	// ENERGY : Si foyer > 120€ / mois
	if category == "ENERGY" && amount > 120 {
		return &models.Suggestion{
			ID:               "sug_household_energy",
			Type:             "ENERGY_SWITCH",
			Title:            "Facture Énergie Élevée",
			Message:          fmt.Sprintf("Le foyer paie %.0f€/mois. Comparez les fournisseurs.", amount),
			PotentialSavings: (amount * 0.15) * 12, // ~15% d'économie
			ActionLink:       "https://www.papernest.com/energie/",
			CanBeContacted:   true,
		}
	}
	// INTERNET : Si > 45€ / mois
	if category == "INTERNET" && amount > 45 {
		return &models.Suggestion{
			ID:               "sug_household_net",
			Type:             "BOX_SWITCH",
			Title:            "Box Internet",
			Message:          "Plus de 45€/mois ? La fibre commence à 20€ la première année.",
			PotentialSavings: (amount - 25) * 12,
			ActionLink:       "https://www.ariase.com/box",
			CanBeContacted:   false,
		}
	}
	return nil
}
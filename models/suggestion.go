package models

// Une suggestion d'économie liée à une charge spécifique
type Suggestion struct {
    ID              string  `json:"id"`
    ChargeID        string  `json:"charge_id"`        // L'ID de la dépense concernée
    Type            string  `json:"type"`             // ex: "MOBILE_OFFER", "ENERGY_OFFER"
    Title           string  `json:"title"`            // ex: "Forfait mobile élevé"
    Message         string  `json:"message"`          // ex: "Vous payez 70€/mois. La moyenne est à 20€."
    PotentialSavings float64 `json:"potential_savings"` // ex: 600.00 (par an)
    ActionLink      string  `json:"action_link"`      // Lien d'affiliation direct (ex: Sosh)
    CanBeContacted  bool    `json:"can_be_contacted"` // Si TRUE, affiche le bouton "Être rappelé"
}
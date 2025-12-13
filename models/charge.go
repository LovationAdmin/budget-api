package models

// Charge représente une dépense récurrente ou une transaction à catégoriser
type Charge struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	Amount   float64 `json:"amount"`
	Category string  `json:"category"` // ex: "ENERGY", "MOBILE"
}
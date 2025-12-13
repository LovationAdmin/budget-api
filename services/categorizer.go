package services

import (
	"context"
	"database/sql"
	"log"
	"strings"
)

type CategorizerService struct {
	db *sql.DB
	ai *AICategorizer
}

func NewCategorizerService(db *sql.DB) *CategorizerService {
	return &CategorizerService{
		db: db,
		ai: NewAICategorizer(),
	}
}

// --- STATIC DICTIONARY (Gratuit) ---
var staticRules = map[string]string{
	// ENERGIE
	"edf": "ENERGY", "engie": "ENERGY", "total energie": "ENERGY", "totalenergies": "ENERGY",
	"eni": "ENERGY", "ilek": "ENERGY", "sowee": "ENERGY", "veolia": "ENERGY", "suez": "ENERGY",
	
	// TELECOM
	"orange": "INTERNET", "sosh": "MOBILE", "sfr": "INTERNET", "red by sfr": "MOBILE",
	"bouygues": "INTERNET", "bbox": "INTERNET", "free": "INTERNET", "free mobile": "MOBILE",
	
	// ASSURANCE
	"axa": "INSURANCE", "allianz": "INSURANCE", "macif": "INSURANCE", "maif": "INSURANCE",
	"matmut": "INSURANCE", "groupama": "INSURANCE", "maaf": "INSURANCE", "alan": "INSURANCE",
	
	// BANQUE
	"boursorama": "BANK", "boursobank": "BANK", "revolut": "BANK", "n26": "BANK",
	"bnp": "BANK", "societe generale": "BANK", "credit agricole": "BANK", "lcl": "BANK",
	
	// LOISIRS
	"netflix": "LEISURE", "spotify": "LEISURE", "deezer": "LEISURE", "apple": "LEISURE",
	"disney": "LEISURE", "prime video": "LEISURE", "basic fit": "LEISURE", "fitness park": "LEISURE",
	
	// ALIMENTATION
	"leclerc": "FOOD", "carrefour": "FOOD", "auchan": "FOOD", "intermarche": "FOOD",
	"lidl": "FOOD", "aldi": "FOOD", "monoprix": "FOOD", "franprix": "FOOD", "uber eats": "FOOD",
	
	// TRANSPORT
	"sncf": "TRANSPORT", "ratp": "TRANSPORT", "uber": "TRANSPORT", "bolt": "TRANSPORT",
	"total access": "TRANSPORT", "shell": "TRANSPORT", "vinci": "TRANSPORT",
}

// GetCategory détermine la catégorie
func (s *CategorizerService) GetCategory(ctx context.Context, rawLabel string) (string, error) {
	// 1. Normalisation
	normalizedLabel := strings.ToLower(strings.TrimSpace(rawLabel))
	if normalizedLabel == "" {
		return "OTHER", nil
	}

	// 2. Règles Statiques
	if category, exists := staticRules[normalizedLabel]; exists {
		return category, nil
	}
	for key, cat := range staticRules {
		if strings.Contains(normalizedLabel, key) {
			return cat, nil
		}
	}

	// 3. Cache DB
	var dbCategory string
	err := s.db.QueryRowContext(ctx, 
		"SELECT category FROM label_mappings WHERE normalized_label = $1", 
		normalizedLabel).Scan(&dbCategory)

	if err == nil {
		return dbCategory, nil
	}

	// 4. Appel Claude AI
	log.Printf("[Categorizer] Calling Claude AI for '%s'...", normalizedLabel)
	aiCategory, err := s.ai.PredictCategory(rawLabel)
	
	if err != nil {
		log.Printf("[Categorizer] AI Error: %v", err)
		return "OTHER", nil 
	}

	// 5. Sauvegarde Cache
	go func(lbl, cat string) {
		_, err := s.db.Exec(
			"INSERT INTO label_mappings (normalized_label, category, source) VALUES ($1, $2, 'AI') ON CONFLICT (normalized_label) DO NOTHING",
			lbl, cat,
		)
		if err != nil {
			log.Printf("[Categorizer] Failed to cache: %v", err)
		}
	}(normalizedLabel, aiCategory)

	return aiCategory, nil
}
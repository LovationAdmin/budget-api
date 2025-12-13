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

// --- STATIC DICTIONARY (Optimisation des coûts) ---
// Liste exhaustive des services les plus courants en France
var staticRules = map[string]string{
	// ENERGIE
	"edf": "ENERGY", "engie": "ENERGY", "total energie": "ENERGY", "totalenergies": "ENERGY",
	"eni": "ENERGY", "ilek": "ENERGY", "vattenfall": "ENERGY", "sowee": "ENERGY",
	"veolia": "ENERGY", "suez": "ENERGY", "eau de paris": "ENERGY", "butagaz": "ENERGY",
	"ohm energie": "ENERGY", "mint energie": "ENERGY", "happ-e": "ENERGY",

	// TELECOM (MOBILE & INTERNET)
	"orange": "INTERNET", "sosh": "MOBILE", "sfr": "INTERNET", "red by sfr": "MOBILE",
	"bouygues": "INTERNET", "bbox": "INTERNET", "free": "INTERNET", "free mobile": "MOBILE",
	"freemobile": "MOBILE", "prixtel": "MOBILE", "nrj mobile": "MOBILE", "la poste mobile": "MOBILE",
	"coryolis": "MOBILE", "syma": "MOBILE", "lebara": "MOBILE", "starlink": "INTERNET",

	// ASSURANCE
	"axa": "INSURANCE", "allianz": "INSURANCE", "macif": "INSURANCE", "maif": "INSURANCE",
	"matmut": "INSURANCE", "groupama": "INSURANCE", "maaf": "INSURANCE", "gan": "INSURANCE",
	"generali": "INSURANCE", "alan": "INSURANCE", "april": "INSURANCE", "mma": "INSURANCE",
	"direct assurance": "INSURANCE", "olivier assurance": "INSURANCE", "pacifica": "INSURANCE",
	"cnp assurances": "INSURANCE", "ag2r": "INSURANCE", "malakoff humanis": "INSURANCE",

	// BANQUE / CREDIT
	"boursorama": "BANK", "boursobank": "BANK", "revolut": "BANK", "n26": "BANK",
	"bnp paribas": "BANK", "societe generale": "BANK", "credit agricole": "BANK",
	"lcl": "BANK", "credit mutuel": "BANK", "banque postale": "BANK", "hello bank": "BANK",
	"fortuneo": "BANK", "monabanq": "BANK", "cetelem": "LOAN", "cofidis": "LOAN", "sofinco": "LOAN",

	// LOISIRS / STREAMING
	"netflix": "LEISURE", "spotify": "LEISURE", "deezer": "LEISURE", "apple music": "LEISURE",
	"disney+": "LEISURE", "disney plus": "LEISURE", "prime video": "LEISURE", "amazon prime": "LEISURE",
	"canal+": "LEISURE", "canal plus": "LEISURE", "beinsport": "LEISURE", "rmc sport": "LEISURE",
	"basic fit": "LEISURE", "fitness park": "LEISURE", "keep cool": "LEISURE", "neoness": "LEISURE",
	"ugc": "LEISURE", "gaumont": "LEISURE", "pathe": "LEISURE", "steam": "LEISURE", "playstation": "LEISURE",

	// ALIMENTATION
	"leclerc": "FOOD", "e.leclerc": "FOOD", "carrefour": "FOOD", "auchan": "FOOD",
	"intermarche": "FOOD", "super u": "FOOD", "lidl": "FOOD", "aldi": "FOOD",
	"monoprix": "FOOD", "franprix": "FOOD", "casino": "FOOD", "picard": "FOOD",
	"grand frais": "FOOD", "biocoop": "FOOD", "naturalia": "FOOD", "uber eats": "FOOD",
	"deliveroo": "FOOD", "just eat": "FOOD", "hellofresh": "FOOD",

	// TRANSPORT
	"sncf": "TRANSPORT", "ratp": "TRANSPORT", "tgv": "TRANSPORT", "ouigo": "TRANSPORT",
	"blablacar": "TRANSPORT", "uber": "TRANSPORT", "bolt": "TRANSPORT", "heetch": "TRANSPORT",
	"total access": "TRANSPORT", "total": "TRANSPORT", "esso": "TRANSPORT", "bp": "TRANSPORT", "shell": "TRANSPORT",
	"vinci autoroutes": "TRANSPORT", "aprr": "TRANSPORT", "sanef": "TRANSPORT",

	// SHOPPING / E-COMMERCE
	"amazon": "SHOPPING", "cdiscount": "SHOPPING", "fnac": "SHOPPING", "darty": "SHOPPING",
	"boulanger": "SHOPPING", "zalando": "SHOPPING", "vinted": "SHOPPING", "shein": "SHOPPING",
	"zara": "SHOPPING", "h&m": "SHOPPING", "uniqlo": "SHOPPING", "decathlon": "SHOPPING",
	"leroy merlin": "HOUSING", "castorama": "HOUSING", "ikea": "HOUSING",

	// LOGEMENT
	"foncia": "HOUSING", "citya": "HOUSING", "nexity": "HOUSING", "century 21": "HOUSING",
}

// GetCategory détermine la catégorie d'un libellé
func (s *CategorizerService) GetCategory(ctx context.Context, rawLabel string) (string, error) {
	// 1. Normalisation
	normalizedLabel := strings.ToLower(strings.TrimSpace(rawLabel))
	if normalizedLabel == "" {
		return "OTHER", nil
	}

	// 2. Vérification Règles Statiques (0€, 0ms)
	// a) Correspondance exacte
	if category, exists := staticRules[normalizedLabel]; exists {
		return category, nil
	}
	// b) Correspondance partielle ("prélèvement edf")
	for key, cat := range staticRules {
		if strings.Contains(normalizedLabel, key) {
			return cat, nil
		}
	}

	// 3. Vérification Cache DB (0€, ~5ms)
	var dbCategory string
	err := s.db.QueryRowContext(ctx, 
		"SELECT category FROM label_mappings WHERE normalized_label = $1", 
		normalizedLabel).Scan(&dbCategory)

	if err == nil {
		return dbCategory, nil
	}

	// 4. Appel IA (Payant, ~500ms)
	log.Printf("[Categorizer] Cache miss for '%s', calling AI...", normalizedLabel)
	aiCategory, err := s.ai.PredictCategory(rawLabel)
	
	// En cas d'échec IA, on renvoie OTHER mais on ne plante pas
	if err != nil {
		log.Printf("[Categorizer] AI Error: %v", err)
		return "OTHER", nil 
	}

	// 5. Sauvegarde asynchrone dans le dictionnaire global
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
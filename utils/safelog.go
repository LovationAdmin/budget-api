// utils/safelog.go
// ============================================================================
// SAFE LOGGING - Masque les données sensibles en production
// ============================================================================
// Ce module fournit des fonctions de logging qui masquent automatiquement
// les informations personnelles et financières en environnement de production.
// ============================================================================

package utils

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
)

// ============================================================================
// CONFIGURATION
// ============================================================================

var (
	// IsProduction détermine si on est en mode production
	// En production, les données sensibles sont masquées
	IsProduction = os.Getenv("GIN_MODE") == "release" ||
		os.Getenv("ENVIRONMENT") == "production" ||
		os.Getenv("ENV") == "production"

	// LogLevel permet de filtrer les logs (DEBUG, INFO, WARN, ERROR)
	LogLevel = getLogLevel()
)

// Niveaux de log
const (
	LogLevelDebug = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)

func getLogLevel() int {
	level := strings.ToUpper(os.Getenv("LOG_LEVEL"))
	switch level {
	case "DEBUG":
		return LogLevelDebug
	case "WARN", "WARNING":
		return LogLevelWarn
	case "ERROR":
		return LogLevelError
	default:
		return LogLevelInfo
	}
}

// ============================================================================
// PATTERNS DE MASQUAGE
// ============================================================================

var (
	// Pattern pour emails
	emailRegex = regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)

	// Pattern pour montants avec devise
	amountWithCurrencyRegex = regexp.MustCompile(`\b\d+([.,]\d{1,2})?\s*(€|EUR|CHF|GBP|USD|£|\$)\b`)

	// Pattern pour montants seuls (nombres décimaux qui ressemblent à des montants)
	amountRegex = regexp.MustCompile(`\b\d{2,}([.,]\d{1,2})?\b`)

	// Pattern pour IBAN
	ibanRegex = regexp.MustCompile(`[A-Z]{2}\d{2}[A-Z0-9]{10,30}`)

	// Pattern pour numéros de carte bancaire (formats courants)
	cardRegex = regexp.MustCompile(`\b\d{4}[\s-]?\d{4}[\s-]?\d{4}[\s-]?\d{4}\b`)

	// Pattern pour numéros de téléphone
	phoneRegex = regexp.MustCompile(`(\+\d{1,3}[\s.-]?)?\(?\d{2,4}\)?[\s.-]?\d{2,4}[\s.-]?\d{2,4}[\s.-]?\d{0,4}`)

	// Pattern pour UUIDs complets
	uuidRegex = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
)

// ============================================================================
// FONCTIONS DE MASQUAGE
// ============================================================================

// MaskString masque les données sensibles dans une chaîne
func MaskString(input string) string {
	if !IsProduction {
		return input
	}

	result := input

	// Masquer les emails
	result = emailRegex.ReplaceAllString(result, "***@***.***")

	// Masquer les IBANs
	result = ibanRegex.ReplaceAllString(result, "****IBAN****")

	// Masquer les numéros de carte
	result = cardRegex.ReplaceAllString(result, "****-****-****-****")

	// Masquer les montants avec devise
	result = amountWithCurrencyRegex.ReplaceAllString(result, "***€")

	// Masquer les UUIDs (raccourcir)
	result = uuidRegex.ReplaceAllStringFunc(result, func(uuid string) string {
		if len(uuid) > 8 {
			return uuid[:8] + "..."
		}
		return "***"
	})

	return result
}

// MaskAmount masque un montant financier
func MaskAmount(amount float64) string {
	if IsProduction {
		return "***"
	}
	return fmt.Sprintf("%.2f", amount)
}

// MaskID masque partiellement un ID (garde les 8 premiers caractères)
func MaskID(id string) string {
	if !IsProduction {
		return id
	}
	if len(id) <= 8 {
		return "***"
	}
	return id[:8] + "..."
}

// MaskEmail masque un email
func MaskEmail(email string) string {
	if !IsProduction {
		return email
	}
	return "***@***.***"
}

// ============================================================================
// FONCTIONS DE LOGGING SÉCURISÉES
// ============================================================================

// SafeLog log un message en masquant les données sensibles
func SafeLog(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	maskedMessage := MaskString(message)
	log.Print(maskedMessage)
}

// SafeLogf est un alias pour SafeLog (compatibilité Printf)
func SafeLogf(format string, args ...interface{}) {
	SafeLog(format, args...)
}

// SafeDebug log un message de debug (seulement si LOG_LEVEL=DEBUG)
func SafeDebug(format string, args ...interface{}) {
	if LogLevel > LogLevelDebug {
		return
	}
	message := fmt.Sprintf(format, args...)
	maskedMessage := MaskString(message)
	log.Printf("[DEBUG] %s", maskedMessage)
}

// SafeInfo log un message d'information
func SafeInfo(format string, args ...interface{}) {
	if LogLevel > LogLevelInfo {
		return
	}
	message := fmt.Sprintf(format, args...)
	maskedMessage := MaskString(message)
	log.Printf("[INFO] %s", maskedMessage)
}

// SafeWarn log un message d'avertissement
func SafeWarn(format string, args ...interface{}) {
	if LogLevel > LogLevelWarn {
		return
	}
	message := fmt.Sprintf(format, args...)
	maskedMessage := MaskString(message)
	log.Printf("[WARN] %s", maskedMessage)
}

// SafeError log un message d'erreur
func SafeError(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	maskedMessage := MaskString(message)
	log.Printf("[ERROR] %s", maskedMessage)
}

// ============================================================================
// FONCTIONS DE LOGGING MÉTIER SPÉCIFIQUES
// ============================================================================

// LogBudgetAction log une action sur un budget sans exposer les données sensibles
func LogBudgetAction(action string, budgetID string, userID string) {
	if IsProduction {
		log.Printf("[Budget] %s - Budget: %s User: %s",
			action,
			MaskID(budgetID),
			MaskID(userID))
	} else {
		log.Printf("[Budget] %s - Budget: %s User: %s",
			action,
			budgetID,
			userID)
	}
}

// LogBankingAction log une action bancaire sans exposer les données sensibles
func LogBankingAction(action string, connectionID string, userID string) {
	if IsProduction {
		log.Printf("[Banking] %s - Connection: %s User: %s",
			action,
			MaskID(connectionID),
			MaskID(userID))
	} else {
		log.Printf("[Banking] %s - Connection: %s User: %s",
			action,
			connectionID,
			userID)
	}
}

// LogAIAnalysis log une analyse IA sans exposer les montants
func LogAIAnalysis(action string, category string, country string, chargeCount int) {
	log.Printf("[AI] %s - Category: %s Country: %s Charges: %d",
		action,
		category,
		country,
		chargeCount)
}

// LogAuthAction log une action d'authentification
func LogAuthAction(action string, email string, success bool) {
	status := "SUCCESS"
	if !success {
		status = "FAILED"
	}

	if IsProduction {
		log.Printf("[Auth] %s - Email: %s Status: %s",
			action,
			MaskEmail(email),
			status)
	} else {
		log.Printf("[Auth] %s - Email: %s Status: %s",
			action,
			email,
			status)
	}
}

// LogAPIRequest log une requête API (sans données sensibles dans le body)
func LogAPIRequest(method string, path string, userID string, statusCode int, duration string) {
	if IsProduction {
		// En production, masquer les IDs dans les paths aussi
		maskedPath := uuidRegex.ReplaceAllStringFunc(path, func(uuid string) string {
			if len(uuid) > 8 {
				return uuid[:8] + "..."
			}
			return "***"
		})
		log.Printf("[API] %s %s - User: %s Status: %d Duration: %s",
			method,
			maskedPath,
			MaskID(userID),
			statusCode,
			duration)
	} else {
		log.Printf("[API] %s %s - User: %s Status: %d Duration: %s",
			method,
			path,
			userID,
			statusCode,
			duration)
	}
}

// LogWebSocket log une action WebSocket
func LogWebSocket(action string, budgetID string, userID string) {
	if IsProduction {
		log.Printf("[WS] %s - Budget: %s User: %s",
			action,
			MaskID(budgetID),
			MaskID(userID))
	} else {
		log.Printf("[WS] %s - Budget: %s User: %s",
			action,
			budgetID,
			userID)
	}
}

// ============================================================================
// FONCTIONS UTILITAIRES
// ============================================================================

// GetEnvMode retourne le mode d'environnement actuel
func GetEnvMode() string {
	if IsProduction {
		return "production"
	}
	return "development"
}

// LogStartup log les informations de démarrage de l'application
func LogStartup(appName string, version string, port string) {
	log.Printf("🚀 %s v%s starting...", appName, version)
	log.Printf("   Mode: %s", GetEnvMode())
	log.Printf("   Port: %s", port)
	log.Printf("   Log Level: %d", LogLevel)
	if IsProduction {
		log.Printf("   ⚠️  Production mode: Sensitive data will be masked in logs")
	}
}
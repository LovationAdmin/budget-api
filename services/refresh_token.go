// services/refresh_token.go
// ============================================================================
// REFRESH TOKEN SERVICE
// ============================================================================
// Tokens opaques (pas des JWT) :
//   - 256 bits d'entropie générés via crypto/rand
//   - Stockés en base sous forme de hash SHA-256 (le serveur ne stocke jamais
//     le token en clair — fuite DB = fuite des hashes inutilisables)
//   - Rotation à chaque refresh : l'ancien est révoqué, un nouveau est émis
//   - TTL : 7 jours par défaut
//   - Audit trail : user_agent + ip_address pour traçabilité
//
// L'access token (JWT) reste court (15 min). Le refresh token sert UNIQUEMENT
// à obtenir un nouvel access token sans demander à l'utilisateur de se
// reconnecter.
// ============================================================================

package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	// 32 octets = 256 bits = ~43 caractères en base64url. Largement suffisant
	// pour résister à un brute-force.
	refreshTokenBytes = 32

	// Durée de vie d'un refresh token. 7 jours = compromis confort UX (l'user
	// ne se reconnecte qu'une fois par semaine) vs sécurité (un token volé
	// est révoqué automatiquement après 7j).
	RefreshTokenTTL = 7 * 24 * time.Hour
)

// Erreurs sentinelles : les handlers peuvent les comparer avec errors.Is
var (
	ErrRefreshTokenNotFound = errors.New("refresh token not found")
	ErrRefreshTokenRevoked  = errors.New("refresh token revoked")
	ErrRefreshTokenExpired  = errors.New("refresh token expired")
)

type RefreshTokenService struct {
	db *sql.DB
}

func NewRefreshTokenService(db *sql.DB) *RefreshTokenService {
	return &RefreshTokenService{db: db}
}

// Issue génère un nouveau refresh token pour l'utilisateur, stocke son hash
// en base, et retourne le token en CLAIR (à envoyer au client une seule fois,
// puis jamais relogger côté serveur).
func (s *RefreshTokenService) Issue(ctx context.Context, userID, userAgent, ipAddress string) (string, error) {
	raw := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	plaintext := base64.RawURLEncoding.EncodeToString(raw)
	hash := hashToken(plaintext)

	id := uuid.New().String()
	expiresAt := time.Now().Add(RefreshTokenTTL)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at, created_at, user_agent, ip_address)
		VALUES ($1, $2, $3, $4, NOW(), $5, $6)
	`, id, userID, hash, expiresAt, userAgent, ipAddress)
	if err != nil {
		return "", fmt.Errorf("insert refresh token: %w", err)
	}

	return plaintext, nil
}

// Validate vérifie qu'un token est actif (non révoqué, non expiré) et
// retourne l'userID associé. En cas d'échec, retourne une erreur sentinelle
// pour que l'appelant puisse distinguer les cas.
func (s *RefreshTokenService) Validate(ctx context.Context, token string) (string, error) {
	hash := hashToken(token)

	var userID string
	var expiresAt time.Time
	var revokedAt sql.NullTime

	err := s.db.QueryRowContext(ctx, `
		SELECT user_id, expires_at, revoked_at FROM refresh_tokens WHERE token_hash = $1
	`, hash).Scan(&userID, &expiresAt, &revokedAt)

	if err == sql.ErrNoRows {
		return "", ErrRefreshTokenNotFound
	}
	if err != nil {
		return "", fmt.Errorf("query refresh token: %w", err)
	}

	if revokedAt.Valid {
		return "", ErrRefreshTokenRevoked
	}
	if time.Now().After(expiresAt) {
		return "", ErrRefreshTokenExpired
	}

	return userID, nil
}

// Revoke marque un refresh token précis comme révoqué. Idempotent : ne fait
// rien si déjà révoqué (pas d'erreur).
func (s *RefreshTokenService) Revoke(ctx context.Context, token string) error {
	hash := hashToken(token)
	_, err := s.db.ExecContext(ctx, `
		UPDATE refresh_tokens SET revoked_at = NOW()
		WHERE token_hash = $1 AND revoked_at IS NULL
	`, hash)
	return err
}

// RevokeAllForUser révoque tous les refresh tokens actifs d'un utilisateur.
// Utilisé pour le logout-all (déconnexion sur tous les appareils).
func (s *RefreshTokenService) RevokeAllForUser(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE refresh_tokens SET revoked_at = NOW()
		WHERE user_id = $1 AND revoked_at IS NULL
	`, userID)
	return err
}

// CleanupExpired supprime les tokens qui ont expiré il y a plus de 30 jours.
// On garde une fenêtre d'audit pour pouvoir investiguer en cas de problème
// de sécurité, puis on nettoie pour ne pas faire grossir la table indéfiniment.
//
// À appeler périodiquement (goroutine schedulée dans main.go, comme
// scheduleCacheCleaning).
func (s *RefreshTokenService) CleanupExpired(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM refresh_tokens WHERE expires_at < NOW() - INTERVAL '30 days'
	`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CountActiveForUser retourne le nombre de tokens actifs pour un user.
// Utile pour exposer une page "Sessions actives" plus tard.
func (s *RefreshTokenService) CountActiveForUser(ctx context.Context, userID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM refresh_tokens
		WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > NOW()
	`, userID).Scan(&count)
	return count, err
}

// hashToken : SHA-256 hex. Pas besoin de bcrypt/argon ici car le token est
// déjà un secret aléatoire de 256 bits (pas une entrée user-controlled).
// SHA-256 est rapide ET suffisant : un attaquant qui obtiendrait un hash
// devrait casser AES-256 + brute-forcer 256 bits, ce qui est infaisable.
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

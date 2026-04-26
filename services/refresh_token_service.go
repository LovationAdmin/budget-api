// services/refresh_token_service.go
// ============================================================================
// REFRESH TOKEN SERVICE
// ============================================================================
// Implémente le flow OAuth 2.1 "Refresh Token Rotation with Reuse Detection".
//
// Principes :
//   1. Chaque refresh émet un NOUVEAU refresh token et révoque l'ancien.
//   2. Tous les tokens issus d'un même login partagent un "family_id".
//   3. Si un refresh token déjà révoqué est présenté → l'attaquant l'a réutilisé,
//      on révoque TOUTE la famille pour forcer la re-authentification.
//   4. Le token brut n'est JAMAIS stocké en base : on stocke un SHA-256 hash.
//      Les tokens étant des UUID v4 (122 bits d'entropie), SHA-256 est suffisant
//      sans bcrypt (qui ralentirait inutilement chaque refresh).
//
// Sécurité au repos :
//   - DB leak → l'attaquant ne peut pas reconstituer les tokens
//   - Network leak → SameSite=None + Secure + httpOnly + CORS allowlist
//   - XSS → impossible d'accéder au refresh (httpOnly)
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

// ============================================================================
// ERRORS
// ============================================================================

var (
	ErrRefreshTokenNotFound = errors.New("refresh token not found")
	ErrRefreshTokenExpired  = errors.New("refresh token expired")
	ErrRefreshTokenRevoked  = errors.New("refresh token revoked")
	ErrRefreshTokenReused   = errors.New("refresh token reuse detected")
)

// ============================================================================
// MODEL
// ============================================================================

// RefreshToken représente une ligne de la table refresh_tokens.
// Le champ TokenHash est le SHA-256 hex du token brut envoyé au client.
type RefreshToken struct {
	ID         string
	UserID     string
	FamilyID   string
	TokenHash  string
	IssuedAt   time.Time
	ExpiresAt  time.Time
	RevokedAt  sql.NullTime
	ReplacedBy sql.NullString
	UserAgent  sql.NullString
	IPAddress  sql.NullString
}

// IsRevoked retourne true si le token a été révoqué.
func (r *RefreshToken) IsRevoked() bool {
	return r.RevokedAt.Valid
}

// IsExpired retourne true si le token a expiré.
func (r *RefreshToken) IsExpired(now time.Time) bool {
	return now.After(r.ExpiresAt)
}

// ============================================================================
// SERVICE
// ============================================================================

// RefreshTokenService gère le cycle de vie des refresh tokens.
type RefreshTokenService struct {
	db       *sql.DB
	lifetime time.Duration
}

// NewRefreshTokenService crée le service. Si lifetime est zéro, 7 jours par défaut.
func NewRefreshTokenService(db *sql.DB, lifetime time.Duration) *RefreshTokenService {
	if lifetime <= 0 {
		lifetime = 7 * 24 * time.Hour
	}
	return &RefreshTokenService{db: db, lifetime: lifetime}
}

// Lifetime retourne la durée de vie configurée (utile pour fixer le Max-Age du cookie).
func (s *RefreshTokenService) Lifetime() time.Duration {
	return s.lifetime
}

// ============================================================================
// HELPERS PRIVÉS
// ============================================================================

// generateRawToken génère un token brut cryptographiquement sûr.
// Format : 32 bytes random encodés en base64 URL-safe (43 caractères).
func generateRawToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken retourne le SHA-256 hex du token brut.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// ============================================================================
// API PUBLIQUE
// ============================================================================

// Issue crée un nouveau refresh token pour un nouveau login (nouvelle famille).
// Retourne le token BRUT (à mettre dans le cookie) et le model persisté.
func (s *RefreshTokenService) Issue(
	ctx context.Context,
	userID string,
	userAgent, ipAddress string,
) (rawToken string, model *RefreshToken, err error) {
	familyID := uuid.New().String()
	return s.issueInFamily(ctx, userID, familyID, userAgent, ipAddress)
}

// Rotate échange un refresh token valide contre un nouveau (même famille).
// Détecte la réutilisation et révoque toute la famille en cas d'attaque.
//
// Retourne le NOUVEAU token brut (à mettre dans le cookie) et son model.
// En cas d'erreur de réutilisation, retourne ErrRefreshTokenReused — l'appelant
// doit alors invalider la session côté client (clear cookie) et obliger un re-login.
func (s *RefreshTokenService) Rotate(
	ctx context.Context,
	rawToken string,
	userAgent, ipAddress string,
) (newRawToken string, newModel *RefreshToken, userID string, err error) {
	tokenHash := hashToken(rawToken)
	now := time.Now()

	// 1. Lookup
	var rt RefreshToken
	row := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, family_id, token_hash, issued_at, expires_at,
		       revoked_at, replaced_by, user_agent, ip_address
		FROM refresh_tokens
		WHERE token_hash = $1
	`, tokenHash)

	err = row.Scan(
		&rt.ID, &rt.UserID, &rt.FamilyID, &rt.TokenHash, &rt.IssuedAt, &rt.ExpiresAt,
		&rt.RevokedAt, &rt.ReplacedBy, &rt.UserAgent, &rt.IPAddress,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, "", ErrRefreshTokenNotFound
	}
	if err != nil {
		return "", nil, "", fmt.Errorf("query refresh token: %w", err)
	}

	// 2. Reuse detection
	// Si le token a été révoqué ET remplacé, c'est qu'on a déjà fait un refresh.
	// Le présenter à nouveau = quelqu'un l'a volé. On révoque toute la famille.
	if rt.IsRevoked() && rt.ReplacedBy.Valid {
		_ = s.RevokeFamily(ctx, rt.FamilyID, "reuse_detected")
		return "", nil, "", ErrRefreshTokenReused
	}

	// 3. Expiration / révocation simple
	if rt.IsExpired(now) {
		return "", nil, "", ErrRefreshTokenExpired
	}
	if rt.IsRevoked() {
		return "", nil, "", ErrRefreshTokenRevoked
	}

	// 4. Issue le nouveau (même famille) puis marque l'ancien comme remplacé
	newRaw, newRT, err := s.issueInFamily(ctx, rt.UserID, rt.FamilyID, userAgent, ipAddress)
	if err != nil {
		return "", nil, "", err
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE refresh_tokens
		SET revoked_at = $1, replaced_by = $2
		WHERE id = $3
	`, now, newRT.ID, rt.ID)
	if err != nil {
		return "", nil, "", fmt.Errorf("revoke old refresh token: %w", err)
	}

	return newRaw, newRT, rt.UserID, nil
}

// Revoke révoque un refresh token spécifique (utilisé au logout).
// Idempotent : pas d'erreur si le token n'existe pas ou est déjà révoqué.
func (s *RefreshTokenService) Revoke(ctx context.Context, rawToken string) error {
	tokenHash := hashToken(rawToken)
	_, err := s.db.ExecContext(ctx, `
		UPDATE refresh_tokens
		SET revoked_at = NOW()
		WHERE token_hash = $1 AND revoked_at IS NULL
	`, tokenHash)
	if err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	return nil
}

// RevokeFamily révoque tous les tokens d'une famille (rotation chain).
// Utilisé en cas de réutilisation détectée.
func (s *RefreshTokenService) RevokeFamily(ctx context.Context, familyID, reason string) error {
	_ = reason // disponible pour log futur
	_, err := s.db.ExecContext(ctx, `
		UPDATE refresh_tokens
		SET revoked_at = NOW()
		WHERE family_id = $1 AND revoked_at IS NULL
	`, familyID)
	if err != nil {
		return fmt.Errorf("revoke family: %w", err)
	}
	return nil
}

// RevokeAllForUser révoque tous les refresh tokens actifs d'un utilisateur.
// Utilisé par "logout all devices" et au changement de mot de passe.
func (s *RefreshTokenService) RevokeAllForUser(ctx context.Context, userID string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE refresh_tokens
		SET revoked_at = NOW()
		WHERE user_id = $1 AND revoked_at IS NULL
	`, userID)
	if err != nil {
		return 0, fmt.Errorf("revoke all for user: %w", err)
	}
	rows, _ := res.RowsAffected()
	return rows, nil
}

// CleanupExpired supprime les tokens expirés depuis plus de 30 jours.
// On garde 30j d'historique pour l'analyse forensique en cas d'incident.
func (s *RefreshTokenService) CleanupExpired(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM refresh_tokens
		WHERE expires_at < NOW() - INTERVAL '30 days'
	`)
	if err != nil {
		return 0, fmt.Errorf("cleanup expired: %w", err)
	}
	rows, _ := res.RowsAffected()
	return rows, nil
}

// ============================================================================
// INTERNE
// ============================================================================

func (s *RefreshTokenService) issueInFamily(
	ctx context.Context,
	userID, familyID, userAgent, ipAddress string,
) (string, *RefreshToken, error) {
	rawToken, err := generateRawToken()
	if err != nil {
		return "", nil, err
	}

	model := &RefreshToken{
		ID:        uuid.New().String(),
		UserID:    userID,
		FamilyID:  familyID,
		TokenHash: hashToken(rawToken),
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(s.lifetime),
		UserAgent: sql.NullString{String: userAgent, Valid: userAgent != ""},
		IPAddress: sql.NullString{String: ipAddress, Valid: ipAddress != ""},
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO refresh_tokens (id, user_id, family_id, token_hash, issued_at, expires_at, user_agent, ip_address)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, model.ID, model.UserID, model.FamilyID, model.TokenHash, model.IssuedAt, model.ExpiresAt, model.UserAgent, model.IPAddress)
	if err != nil {
		return "", nil, fmt.Errorf("insert refresh token: %w", err)
	}

	return rawToken, model, nil
}

// services/private_items.go
// ============================================================================
// PRIVATE ITEMS — per-user, per-budget secrets that other members never see.
// ============================================================================
//
// The budget blob is shared by every member of a budget. Anything a member
// marks as private (e.g. the name and category of a personal charge) is kept
// here instead: one encrypted payload per (budget, user, item), readable only
// by that user. Other members only see a placeholder in the shared blob.
// ============================================================================

package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"

	"github.com/LovationAdmin/budget-api/utils"
	"github.com/pkg/errors"
)

// MaxPrivatePayloadBytes caps a private item (a label, a category, a note).
const MaxPrivatePayloadBytes = 4096

var privateItemIDRe = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,100}$`)

// ErrInvalidPrivateItem is returned for a bad item id or payload.
var ErrInvalidPrivateItem = errors.New("invalid private item")

type PrivateItemService struct {
	DB *sql.DB
}

func NewPrivateItemService(db *sql.DB) *PrivateItemService {
	return &PrivateItemService{DB: db}
}

// ValidatePrivateItem checks an item id and its JSON payload.
func ValidatePrivateItem(itemID string, payload json.RawMessage) error {
	if !privateItemIDRe.MatchString(itemID) {
		return errors.Wrap(ErrInvalidPrivateItem, "item id")
	}
	if len(payload) == 0 || len(payload) > MaxPrivatePayloadBytes || !json.Valid(payload) {
		return errors.Wrap(ErrInvalidPrivateItem, "payload")
	}
	return nil
}

// List returns the caller's private items for a budget (item id -> payload).
func (s *PrivateItemService) List(ctx context.Context, budgetID, userID string) (map[string]json.RawMessage, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT item_id, payload FROM private_items WHERE budget_id = $1 AND user_id = $2`, budgetID, userID)
	if err != nil {
		return nil, errors.Wrap(err, "query private items")
	}
	defer rows.Close()

	out := map[string]json.RawMessage{}
	for rows.Next() {
		var itemID, encrypted string
		if err := rows.Scan(&itemID, &encrypted); err != nil {
			return nil, errors.Wrap(err, "scan private item")
		}
		plain, err := utils.Decrypt(encrypted)
		if err != nil {
			// A row we cannot decrypt is skipped rather than failing the whole list.
			utils.SafeWarn("private-items: skip undecryptable item %s", itemID)
			continue
		}
		out[itemID] = json.RawMessage(plain)
	}
	return out, errors.Wrap(rows.Err(), "iterate private items")
}

// Put creates or replaces one private item (encrypted at rest).
func (s *PrivateItemService) Put(ctx context.Context, budgetID, userID, itemID string, payload json.RawMessage) error {
	if err := ValidatePrivateItem(itemID, payload); err != nil {
		return err
	}
	encrypted, err := utils.Encrypt(payload)
	if err != nil {
		return errors.Wrap(err, "encrypt private item")
	}
	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO private_items (budget_id, user_id, item_id, payload, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (budget_id, user_id, item_id) DO UPDATE SET payload = EXCLUDED.payload, updated_at = NOW()`,
		budgetID, userID, itemID, encrypted)
	return errors.Wrap(err, "upsert private item")
}

// Delete removes one private item of the caller.
func (s *PrivateItemService) Delete(ctx context.Context, budgetID, userID, itemID string) error {
	_, err := s.DB.ExecContext(ctx,
		`DELETE FROM private_items WHERE budget_id = $1 AND user_id = $2 AND item_id = $3`, budgetID, userID, itemID)
	return errors.Wrap(err, "delete private item")
}

// ExportForUser returns every private item of a user, by budget (GDPR export).
func (s *PrivateItemService) ExportForUser(ctx context.Context, userID string) (map[string]map[string]json.RawMessage, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT budget_id::text, item_id, payload FROM private_items WHERE user_id = $1`, userID)
	if err != nil {
		return nil, errors.Wrap(err, "query private items for export")
	}
	defer rows.Close()
	out := map[string]map[string]json.RawMessage{}
	for rows.Next() {
		var budgetID, itemID, encrypted string
		if err := rows.Scan(&budgetID, &itemID, &encrypted); err != nil {
			return nil, errors.Wrap(err, "scan private item for export")
		}
		plain, err := utils.Decrypt(encrypted)
		if err != nil {
			continue
		}
		if out[budgetID] == nil {
			out[budgetID] = map[string]json.RawMessage{}
		}
		out[budgetID][itemID] = json.RawMessage(plain)
	}
	return out, errors.Wrap(rows.Err(), "iterate private items for export")
}

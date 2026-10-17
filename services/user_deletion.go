// services/user_deletion.go
// Deleting a user: most tables follow with ON DELETE CASCADE (own budgets,
// memberships, tokens…). Three references have no ON DELETE rule and would
// block the delete; they are cleared first, in the same transaction.

package services

import (
	"context"
	"database/sql"
)

// ClearUserReferences detaches the user from the rows that outlive them:
// edits and history in budgets shared with others, invitations they sent.
func ClearUserReferences(ctx context.Context, tx *sql.Tx, userID string) error {
	for _, q := range []string{
		`UPDATE budget_data SET updated_by = NULL WHERE updated_by = $1`,
		`UPDATE audit_logs SET user_id = NULL WHERE user_id = $1`,
		`UPDATE invitations SET invited_by = NULL WHERE invited_by = $1`,
	} {
		if _, err := tx.ExecContext(ctx, q, userID); err != nil {
			return err
		}
	}
	return nil
}

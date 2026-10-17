// services/unverified_cleanup.go
// ============================================================================
// Accounts whose email was never confirmed: they cannot sign in, so they only
// hold an address. 23 days after signup the account gets an email with a
// fresh confirmation link; 7 days after that email, if still unconfirmed, it
// is deleted. No account is deleted without that email having gone out, and
// none that shares a budget with someone else (old accounts, from before
// sign-in required a confirmed email, may own a budget).
// ============================================================================

package services

import (
	"context"
	"database/sql"
	"time"

	"github.com/LovationAdmin/budget-api/utils"
	"github.com/google/uuid"
)

const (
	// UnverifiedWarningCampaign marks the warning in email_campaign_sends.
	UnverifiedWarningCampaign = "unverified_deletion_warning"
	unverifiedNoticeDays      = 7
	unverifiedSendThrottle    = 150 * time.Millisecond
)

// The account shares no budget with another person.
const notSharingBudget = `
	NOT EXISTS (
		SELECT 1 FROM budget_members mine
		JOIN budget_members other ON other.budget_id = mine.budget_id AND other.user_id <> mine.user_id
		WHERE mine.user_id = u.id
	)
	AND NOT EXISTS (
		SELECT 1 FROM budgets b
		JOIN budget_members other ON other.budget_id = b.id AND other.user_id <> b.owner_id
		WHERE b.owner_id = u.id
	)`

type unverifiedMailer interface {
	SendUnverifiedExpiryEmail(toEmail, userName, token, locale string, deleteFrom time.Time) error
}

type UnverifiedCleanup struct {
	DB     *sql.DB
	Mailer unverifiedMailer
}

type UnverifiedCleanupResult struct {
	Warned, WarnFailed, Deleted, DeleteFailed int
}

// Run sends the warnings that are due, then deletes the accounts whose
// warning is at least 7 days old. Safe to run again: each account is warned
// once.
func (s *UnverifiedCleanup) Run(ctx context.Context) (UnverifiedCleanupResult, error) {
	var res UnverifiedCleanupResult
	if err := s.warn(ctx, &res); err != nil {
		return res, err
	}
	return res, s.delete(ctx, &res)
}

type unverifiedAccount struct {
	ID, Email, Name, Locale string
}

func (s *UnverifiedCleanup) warn(ctx context.Context, res *UnverifiedCleanupResult) error {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT u.id::text, u.email, COALESCE(u.name, ''), COALESCE(u.locale, '')
		FROM users u
		WHERE NOT u.email_verified
		  AND u.created_at < NOW() - INTERVAL '23 days'
		  AND NOT EXISTS (
			SELECT 1 FROM email_campaign_sends s
			WHERE s.campaign_id = $1 AND s.user_id = u.id AND s.status = 'sent'
		  )
		  AND`+notSharingBudget+`
		ORDER BY u.created_at`, UnverifiedWarningCampaign)
	if err != nil {
		return err
	}
	var due []unverifiedAccount
	for rows.Next() {
		var a unverifiedAccount
		if err := rows.Scan(&a.ID, &a.Email, &a.Name, &a.Locale); err != nil {
			rows.Close()
			return err
		}
		due = append(due, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	deleteFrom := time.Now().AddDate(0, 0, unverifiedNoticeDays)
	for _, a := range due {
		if err := s.warnOne(ctx, a, deleteFrom); err != nil {
			res.WarnFailed++
			utils.SafeWarn("unverified-cleanup: warning failed for %s: %v", a.ID, err)
			s.record(ctx, a.ID, "failed", err.Error())
		} else {
			res.Warned++
			s.record(ctx, a.ID, "sent", "")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(unverifiedSendThrottle):
		}
	}
	return nil
}

// warnOne replaces the account's confirmation link with one valid until the
// deletion, and emails it.
func (s *UnverifiedCleanup) warnOne(ctx context.Context, a unverifiedAccount, deleteFrom time.Time) error {
	token := uuid.New().String()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, `DELETE FROM email_verification_tokens WHERE user_id = $1`, a.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO email_verification_tokens (user_id, token, expires_at, created_at)
		VALUES ($1, $2, NOW() + make_interval(days => $3), NOW())`, a.ID, token, unverifiedNoticeDays); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.Mailer.SendUnverifiedExpiryEmail(a.Email, a.Name, token, a.Locale, deleteFrom)
}

func (s *UnverifiedCleanup) record(ctx context.Context, userID, status, errMsg string) {
	if _, err := s.DB.ExecContext(ctx, `
		INSERT INTO email_campaign_sends (campaign_id, user_id, status, error_message)
		VALUES ($1, $2, $3, NULLIF($4, ''))
		ON CONFLICT (campaign_id, user_id) DO UPDATE
		SET status = EXCLUDED.status, error_message = EXCLUDED.error_message, created_at = NOW()`,
		UnverifiedWarningCampaign, userID, status, errMsg); err != nil {
		utils.SafeWarn("unverified-cleanup: recording the warning for %s failed: %v", userID, err)
	}
}

func (s *UnverifiedCleanup) delete(ctx context.Context, res *UnverifiedCleanupResult) error {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT u.id::text
		FROM users u
		JOIN email_campaign_sends s
		  ON s.user_id = u.id AND s.campaign_id = $1 AND s.status = 'sent'
		WHERE NOT u.email_verified
		  AND u.created_at < NOW() - INTERVAL '30 days'
		  AND s.created_at <= NOW() - make_interval(days => $2)
		  AND`+notSharingBudget, UnverifiedWarningCampaign, unverifiedNoticeDays)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// One by one, re-checked at the last moment: an account confirmed in the
	// meantime stays; one failure doesn't stop the others.
	for _, id := range ids {
		deleted, err := s.deleteOne(ctx, id)
		if err != nil {
			res.DeleteFailed++
			utils.SafeWarn("unverified-cleanup: deleting %s failed: %v", id, err)
		} else if deleted {
			res.Deleted++
			utils.SafeInfo("unverified-cleanup: deleted unconfirmed account %s", id)
		}
	}
	return nil
}

// deleteOne deletes the account; its own budgets go with it (ON DELETE
// CASCADE). See ClearUserReferences.
func (s *UnverifiedCleanup) deleteOne(ctx context.Context, id string) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback() //nolint:errcheck
	if err := ClearUserReferences(ctx, tx, id); err != nil {
		return false, err
	}
	r, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = $1 AND NOT email_verified`, id)
	if err != nil {
		return false, err
	}
	n, _ := r.RowsAffected()
	return n > 0, tx.Commit()
}

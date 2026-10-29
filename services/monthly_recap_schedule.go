// services/monthly_recap_schedule.go
// ============================================================================
// MONTHLY RECAP — when it goes out
// ============================================================================
// Pay lands between the 27th and the end of the month: that is when a
// household sorts out the month ahead, so that is when the recap of that
// month goes out (the 27th at 09:00 Europe/Paris by default).
//
// The window stays open until the month starts: an instance that boots inside
// it (deploy, restart in the middle of the send) sends what is missing, the
// sends already recorded being skipped.
// ============================================================================

package services

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// DefaultRecapSendDay is the day of the month the recap of the next month
// goes out. MONTHLY_RECAP_DAY overrides it (1 to 28).
const DefaultRecapSendDay = 27

const recapSendHour = 9

// RecapSendDay reads MONTHLY_RECAP_DAY, falling back to the default.
func RecapSendDay() int {
	if n, err := strconv.Atoi(os.Getenv("MONTHLY_RECAP_DAY")); err == nil && n >= 1 && n <= 28 {
		return n
	}
	return DefaultRecapSendDay
}

func recapSendTime(year int, month time.Month, day int, loc *time.Location) time.Time {
	return time.Date(year, month, day, recapSendHour, 0, 0, 0, loc)
}

// NextRecapRun is the next send time strictly after `from`.
func NextRecapRun(from time.Time, day int, loc *time.Location) time.Time {
	from = from.In(loc)
	t := recapSendTime(from.Year(), from.Month(), day, loc)
	if !t.After(from) {
		t = recapSendTime(from.Year(), from.Month()+1, day, loc)
	}
	return t
}

// RecapWindow reports whether `now` is between the send time of its month and
// the start of the next month, and returns that next month (the one the
// recap prepares).
func RecapWindow(now time.Time, day int, loc *time.Location) (time.Time, bool) {
	now = now.In(loc)
	if now.Before(recapSendTime(now.Year(), now.Month(), day, loc)) {
		return time.Time{}, false
	}
	return RecapTargetMonth(now), true
}

// RecapTargetMonth is the first day of the month after `t`: the month a
// recap sent at `t` prepares.
func RecapTargetMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location())
}

// RecapCampaignID identifies the recap of a month: monthly_recap_YYYY_MM,
// YYYY_MM being the month the recap prepares.
func RecapCampaignID(month time.Time) string {
	return fmt.Sprintf("monthly_recap_%04d_%02d", month.Year(), int(month.Month()))
}

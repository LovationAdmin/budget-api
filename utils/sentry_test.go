package utils

import "testing"

func TestSentryRelease(t *testing.T) {
	cases := []struct {
		name, release, commit, want string
	}{
		{"explicit SENTRY_RELEASE wins", "custom@1", "405d5e9698857f5137000e96ab277f689a6a30cc", "custom@1"},
		{"deployed commit on Render", "", "405d5e9698857f5137000e96ab277f689a6a30cc", "budget-api@405d5e9"},
		{"version outside Render", "", "", "budget-api@2.4.1"},
		{"malformed commit falls back to version", "", "abc", "budget-api@2.4.1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("SENTRY_RELEASE", c.release)
			t.Setenv("RENDER_GIT_COMMIT", c.commit)
			if got := sentryRelease("2.4.1"); got != c.want {
				t.Errorf("sentryRelease() = %q, want %q", got, c.want)
			}
		})
	}
}

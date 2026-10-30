package model

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// DateLayout is the only accepted deadline format.
const DateLayout = "2006-01-02"

// ParseDeadline parses a YYYY-MM-DD string as local midnight.
//
// This deliberately uses ParseInLocation rather than time.Parse: time.Parse
// returns UTC, which reproduces the off-by-one-day bug that the web app's
// parseDateLocal() exists to avoid.
func ParseDeadline(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation(DateLayout, s, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// ParseDeadlineStrict validates and canonicalizes a user-supplied deadline.
// Unlike ParseDeadline, it reports an error instead of returning false so flag
// handlers can reject "tomorrow" or "2026-11-2" at the boundary.
func ParseDeadlineStrict(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	t, err := time.ParseInLocation(DateLayout, s, time.Local)
	if err != nil {
		return "", fmt.Errorf("invalid deadline %q: expected YYYY-MM-DD", s)
	}
	return t.Format(DateLayout), nil
}

// FormatDeadline renders a time as YYYY-MM-DD in its own location.
func FormatDeadline(t time.Time) string { return t.Format(DateLayout) }

// StartOfDay truncates t to local midnight.
func StartOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
}

// DaysUntil returns the whole-day difference between a deadline and now, both
// floored to local midnight. The rounding mirrors calculateUrgency() and keeps
// DST-shortened days from shifting the result.
func DaysUntil(deadline string, now time.Time) (int, bool) {
	target, ok := ParseDeadline(deadline)
	if !ok {
		return 0, false
	}
	diff := target.Sub(StartOfDay(now))
	return int(math.Round(float64(diff) / float64(24*time.Hour))), true
}

// Urgency returns the same bands as calculateUrgency(): "Overdue", "Due Today",
// "Due Tomorrow", "This Week", or "" when the deadline is absent, unparseable,
// or more than a week away.
func Urgency(deadline string, now time.Time) string {
	days, ok := DaysUntil(deadline, now)
	if !ok {
		return ""
	}
	switch {
	case days < 0:
		return "Overdue"
	case days == 0:
		return "Due Today"
	case days == 1:
		return "Due Tomorrow"
	case days >= 2 && days <= 7:
		return "This Week"
	default:
		return ""
	}
}

// WithinDays reports whether the deadline falls in [today, today+days].
// Items without a parseable deadline are excluded, matching RiskBanner.
func WithinDays(deadline string, now time.Time, days int) bool {
	d, ok := DaysUntil(deadline, now)
	if !ok {
		return false
	}
	return d >= 0 && d <= days
}

// IsOverdue reports whether the deadline is strictly before today.
func IsOverdue(deadline string, now time.Time) bool {
	d, ok := DaysUntil(deadline, now)
	return ok && d < 0
}

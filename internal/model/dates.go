package model

import (
	"fmt"
	"math"
	"strconv"
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

// ParseDeadlineInput accepts the friendly forms people actually type and
// canonicalizes them to YYYY-MM-DD. Supported forms are ISO dates, today,
// tomorrow, weekday names (fri), relative days (+3d, in 3 days, 3d), next week,
// and month-day forms such as "Nov 2" or "2 November".
func ParseDeadlineInput(s string, now time.Time) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if t, err := time.ParseInLocation(DateLayout, s, time.Local); err == nil {
		return t.Format(DateLayout), nil
	}

	today := StartOfDay(now)
	lower := strings.ToLower(s)
	switch lower {
	case "today", "tod", "now":
		return FormatDeadline(today), nil
	case "tomorrow", "tmrw", "tmr", "tom":
		return FormatDeadline(today.AddDate(0, 0, 1)), nil
	case "next week":
		return FormatDeadline(today.AddDate(0, 0, 7)), nil
	}

	if days, ok := parseRelativeDays(lower); ok {
		return FormatDeadline(today.AddDate(0, 0, days)), nil
	}
	if days, ok := parseWeekday(lower, today); ok {
		return FormatDeadline(today.AddDate(0, 0, days)), nil
	}
	if t, ok := parseMonthDay(lower, today); ok {
		return FormatDeadline(t), nil
	}

	return "", fmt.Errorf("invalid deadline %q: try YYYY-MM-DD, tomorrow, fri, +3d, or next week", s)
}

func parseRelativeDays(s string) (int, bool) {
	var raw string
	switch {
	case strings.HasPrefix(s, "+"):
		raw = strings.TrimSuffix(strings.TrimPrefix(s, "+"), "d")
	case strings.HasPrefix(s, "in "):
		raw = strings.TrimPrefix(s, "in ")
		raw = strings.TrimSuffix(strings.TrimSuffix(raw, " days"), " day")
		raw = strings.TrimSpace(raw)
	case strings.HasSuffix(s, "d"):
		raw = strings.TrimSuffix(s, "d")
	default:
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

var weekdayAliases = map[string]time.Weekday{
	"sun": time.Sunday, "sunday": time.Sunday,
	"mon": time.Monday, "monday": time.Monday,
	"tue": time.Tuesday, "tues": time.Tuesday, "tuesday": time.Tuesday,
	"wed": time.Wednesday, "weds": time.Wednesday, "wednesday": time.Wednesday,
	"thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday, "thursday": time.Thursday,
	"fri": time.Friday, "friday": time.Friday,
	"sat": time.Saturday, "saturday": time.Saturday,
}

func parseWeekday(s string, today time.Time) (int, bool) {
	s = strings.TrimSpace(strings.TrimPrefix(s, "next "))
	wd, ok := weekdayAliases[s]
	if !ok {
		return 0, false
	}
	delta := (int(wd) - int(today.Weekday()) + 7) % 7
	return delta, true
}

func parseMonthDay(s string, today time.Time) (time.Time, bool) {
	cleaned := strings.ReplaceAll(s, ",", " ")
	cleaned = strings.Join(strings.Fields(cleaned), " ")

	for _, layout := range []string{"Jan 2 2006", "January 2 2006", "2 Jan 2006", "2 January 2006"} {
		if t, err := time.ParseInLocation(layout, cleaned, time.Local); err == nil {
			return t, true
		}
	}
	for _, layout := range []string{"Jan 2", "January 2", "2 Jan", "2 January"} {
		if t, err := time.ParseInLocation(layout, cleaned, time.Local); err == nil {
			t = time.Date(today.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
			if t.Before(today) {
				t = t.AddDate(1, 0, 0)
			}
			return t, true
		}
	}
	return time.Time{}, false
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

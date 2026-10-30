package model

import (
	"testing"
	"time"
)

func TestParseDeadlineInput(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local)
	friday := now
	delta := (int(time.Friday) - int(now.Weekday()) + 7) % 7
	friday = friday.AddDate(0, 0, delta)

	cases := map[string]string{
		"2026-12-01":  "2026-12-01",
		"today":       "2026-11-02",
		"tomorrow":    "2026-11-03",
		"next week":   "2026-11-09",
		"+3d":         "2026-11-05",
		"+3":          "2026-11-05",
		"in 3 days":   "2026-11-05",
		"3d":          "2026-11-05",
		"fri":         friday.Format(DateLayout),
		"friday":      friday.Format(DateLayout),
		"Nov 2":       "2026-11-02",
		"2 November":  "2026-11-02",
		"Jan 2, 2027": "2027-01-02",
	}
	for in, want := range cases {
		got, err := ParseDeadlineInput(in, now)
		if err != nil {
			t.Errorf("ParseDeadlineInput(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseDeadlineInput(%q) = %q, want %q", in, got, want)
		}
	}

	// A month/day without a year that has already passed rolls to next year.
	later := time.Date(2026, 12, 1, 12, 0, 0, 0, time.Local)
	if got, err := ParseDeadlineInput("Nov 2", later); err != nil || got != "2027-11-02" {
		t.Errorf("past month/day = %q, %v; want 2027-11-02", got, err)
	}

	for _, bad := range []string{"2026-11-2", "blursday", "11/02/2026"} {
		if _, err := ParseDeadlineInput(bad, now); err == nil {
			t.Errorf("ParseDeadlineInput(%q) should fail", bad)
		}
	}
}

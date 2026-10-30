package capture

import (
	"testing"
	"time"

	"github.com/P-Baranwal/clerify/internal/model"
)

func TestParseQuickCapture(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local)

	got, ok := Parse("essay due friday, about 5h", now)
	if !ok {
		t.Fatal("expected a capture")
	}
	if got.Title != "essay" {
		t.Errorf("title = %q", got.Title)
	}
	if got.Type != model.TypeEssay {
		t.Errorf("type = %q", got.Type)
	}
	if got.EstimatedHours == nil || *got.EstimatedHours != 5 {
		t.Errorf("hours = %v", got.EstimatedHours)
	}
	if got.Due == nil {
		t.Fatal("deadline missing")
	}
	due, valid := model.ParseDeadline(*got.Due)
	if !valid || due.Weekday() != time.Friday {
		t.Errorf("due = %q, want a Friday", *got.Due)
	}

	got, ok = Parse("Write report due 2026-11-05", now)
	if !ok || got.Title != "Write report" || got.Due == nil || *got.Due != "2026-11-05" {
		t.Errorf("ISO capture = %+v, ok=%v", got, ok)
	}
	if got.EstimatedHours != nil {
		t.Errorf("hours should be nil, got %v", *got.EstimatedHours)
	}

	got, ok = Parse("Call mom tomorrow", now)
	if !ok || got.Title != "Call mom" || got.Due == nil || *got.Due != "2026-11-03" {
		t.Errorf("relative capture = %+v, ok=%v", got, ok)
	}

	got, ok = Parse("fix login bug by fri for 2 hours", now)
	if !ok || got.Title != "fix login bug" {
		t.Errorf("coding capture = %+v, ok=%v", got, ok)
	}
	if got.Type != model.TypeCoding || got.EstimatedHours == nil || *got.EstimatedHours != 2 {
		t.Errorf("coding fields = %+v", got)
	}
}

func TestParseLeavesPlainTitlesAlone(t *testing.T) {
	now := time.Now()
	for _, text := range []string{"Write report", "Read chapter 3", "Plan the launch", "Fix 3d printer"} {
		if got, ok := Parse(text, now); ok {
			t.Errorf("Parse(%q) unexpectedly matched: %+v", text, got)
		}
	}
}

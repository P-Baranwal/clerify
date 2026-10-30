package model

import (
	"testing"
	"time"
)

func TestNormalizeItemClampsLikeTheOracle(t *testing.T) {
	// These exact expectations come from the web app's e2e backup test:
	// difficulty 0 clamps up to 1, while estimatedHours and priorityScore stay 0.
	got := NormalizeItem(Item{Title: "x", Difficulty: 0, EstimatedHours: 0, PriorityScore: 0, Status: "active"})

	if got.Difficulty != 1 {
		t.Errorf("difficulty = %d, want 1", got.Difficulty)
	}
	if got.EstimatedHours != 0 {
		t.Errorf("estimatedHours = %v, want 0", got.EstimatedHours)
	}
	if got.PriorityScore != 0 {
		t.Errorf("priorityScore = %d, want 0", got.PriorityScore)
	}
	if got.Status != StatusTodo {
		t.Errorf("status = %q, want todo (legacy active alias)", got.Status)
	}
}

func TestNormalizeItemDefaults(t *testing.T) {
	got := NormalizeItem(Item{})
	if got.Title != "Untitled" {
		t.Errorf("title = %q", got.Title)
	}
	if got.Type != TypeOther {
		t.Errorf("type = %q", got.Type)
	}
	if got.Difficulty != MinDifficulty {
		t.Errorf("difficulty = %d", got.Difficulty)
	}
	if got.Checklist != nil {
		t.Errorf("checklist should stay nil")
	}
}

func TestNormalizeItemChecklistAssignsIDs(t *testing.T) {
	got := NormalizeItem(Item{Checklist: []ChecklistItem{{Text: "one"}, {Text: "  two  "}, {Text: "   "}}})
	if len(got.Checklist) != 2 {
		t.Fatalf("checklist length = %d, want 2", len(got.Checklist))
	}
	for _, c := range got.Checklist {
		if c.ID == "" {
			t.Errorf("checklist item %q has no id", c.Text)
		}
	}
	if got.Checklist[1].Text != "two" {
		t.Errorf("text not trimmed: %q", got.Checklist[1].Text)
	}
}

func TestNormalizeItemROIOverridesPriority(t *testing.T) {
	impact := 6
	got := NormalizeItem(Item{ImpactScore: &impact, EstimatedHours: 2, PriorityScore: 99})
	if got.PriorityScore != 30 {
		t.Errorf("priority = %d, want 30 (6/2*10)", got.PriorityScore)
	}
	if got.PriorityReasoning != "ROI Priority: 6 impact vs 2h estimated effort." {
		t.Errorf("reasoning = %q", got.PriorityReasoning)
	}
}

func TestNormalizeItemROIClampsAtHundred(t *testing.T) {
	impact := 10
	got := NormalizeItem(Item{ImpactScore: &impact, EstimatedHours: 0.25})
	if got.PriorityScore != MaxPriority {
		t.Errorf("priority = %d, want %d", got.PriorityScore, MaxPriority)
	}
}

func TestNormalizeItemKeepsInactiveBoostValues(t *testing.T) {
	reason := "later"
	score := 70
	got := NormalizeItem(Item{Boost: Boost{Active: false, Reason: &reason, BoostedPriorityScore: &score}, PriorityScore: 20})
	if got.EffectiveScore() != 20 {
		t.Errorf("effective score = %d, want 20", got.EffectiveScore())
	}
	if got.Boost.Reason == nil || *got.Boost.Reason != "later" {
		t.Errorf("inactive boost reason should be preserved")
	}
}

func TestEffectiveScoreUsesBoostWhenActive(t *testing.T) {
	score := 88
	it := Item{PriorityScore: 10, Boost: Boost{Active: true, BoostedPriorityScore: &score}}
	if it.EffectiveScore() != 88 {
		t.Errorf("effective score = %d, want 88", it.EffectiveScore())
	}
}

func TestCanonicalPreset(t *testing.T) {
	cases := map[string]string{
		"Deadline First":   PresetDeadlineFirst,
		"deadline-first":   PresetDeadlineFirst,
		"Easy First":       PresetEasiestFirst,
		"Hard First":       PresetDifficultyFirst,
		"Balanced":         PresetBalanced,
		"":                 PresetBalanced,
		"something else":   PresetBalanced,
		"Difficulty-first": PresetDifficultyFirst,
	}
	for in, want := range cases {
		if got := CanonicalPreset(in); got != want {
			t.Errorf("CanonicalPreset(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeProfile(t *testing.T) {
	got := NormalizeProfile(Profile{PriorityPreset: "Easy First", AvailableHoursPerDay: 0, Theme: "neon"})
	if got.PriorityPreset != PresetEasiestFirst {
		t.Errorf("preset = %q", got.PriorityPreset)
	}
	if got.AvailableHoursPerDay != DefaultAvailableHours {
		t.Errorf("hours = %d", got.AvailableHoursPerDay)
	}
	if got.Theme != ThemeSystem {
		t.Errorf("theme = %q", got.Theme)
	}
	if over := NormalizeProfile(Profile{AvailableHoursPerDay: 99}); over.AvailableHoursPerDay != MaxAvailable {
		t.Errorf("hours clamp = %d", over.AvailableHoursPerDay)
	}
}

func TestUrgencyBands(t *testing.T) {
	now := time.Date(2026, 10, 26, 15, 30, 0, 0, time.Local)
	cases := []struct {
		deadline string
		want     string
	}{
		{"2026-10-25", "Overdue"},
		{"2026-10-26", "Due Today"},
		{"2026-10-27", "Due Tomorrow"},
		{"2026-10-28", "This Week"},
		{"2026-11-02", "This Week"},
		{"2026-11-03", ""},
		{"", ""},
		{"not-a-date", ""},
	}
	for _, c := range cases {
		if got := Urgency(c.deadline, now); got != c.want {
			t.Errorf("Urgency(%q) = %q, want %q", c.deadline, got, c.want)
		}
	}
}

func TestDaysUntilIgnoresTimeOfDay(t *testing.T) {
	now := time.Date(2026, 10, 26, 23, 59, 0, 0, time.Local)
	days, ok := DaysUntil("2026-10-27", now)
	if !ok || days != 1 {
		t.Errorf("DaysUntil = %d, %v; want 1, true", days, ok)
	}
}

func TestReconcileChecklistPreservesProgress(t *testing.T) {
	existing := []ChecklistItem{
		{ID: "a", Text: "Read paper", Done: true},
		{ID: "b", Text: "Write draft", Done: false},
		{ID: "c", Text: "Drop me", Done: false},
	}
	got := ReconcileChecklist(existing, []string{"Write draft", "Read paper", "Add citations"})
	if len(got) != 3 {
		t.Fatalf("length = %d", len(got))
	}
	if got[0].ID != "b" || got[0].Done {
		t.Errorf("Write draft lost its id or done flag: %+v", got[0])
	}
	if got[1].ID != "a" || !got[1].Done {
		t.Errorf("Read paper lost its done flag: %+v", got[1])
	}
	if got[2].ID == "" || got[2].Text != "Add citations" {
		t.Errorf("new item not created: %+v", got[2])
	}
}

func TestParseStatusStrict(t *testing.T) {
	cases := map[string]string{
		"todo":        StatusTodo,
		"active":      StatusTodo,
		"in_progress": StatusInProgress,
		"doing":       StatusInProgress,
		"blocked":     StatusBlocked,
		"done":        StatusDone,
		"completed":   StatusDone,
		"donee":       "",
	}
	for in, want := range cases {
		got, err := ParseStatus(in)
		if want == "" {
			if err == nil {
				t.Errorf("ParseStatus(%q) should fail", in)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("ParseStatus(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestStrictParsers(t *testing.T) {
	if got, err := ParseType("essay"); err != nil || got != TypeEssay {
		t.Errorf("ParseType(essay) = %q, %v", got, err)
	}
	if _, err := ParseType("nonsense"); err == nil {
		t.Error("ParseType should reject a typo")
	}
	if got, err := ParsePreset("Deadline First"); err != nil || got != PresetDeadlineFirst {
		t.Errorf("ParsePreset = %q, %v", got, err)
	}
	if _, err := ParsePreset("whatever"); err == nil {
		t.Error("ParsePreset should reject a typo")
	}
	if got, err := ParseTheme("dark"); err != nil || got != ThemeDark {
		t.Errorf("ParseTheme = %q, %v", got, err)
	}
	if _, err := ParseTheme("neon"); err == nil {
		t.Error("ParseTheme should reject a typo")
	}
	if err := ValidateHours(0); err == nil {
		t.Error("ValidateHours(0) should fail")
	}
	if err := ValidateHours(99); err == nil {
		t.Error("ValidateHours(99) should fail")
	}
}

func TestParseDeadlineStrict(t *testing.T) {
	if got, err := ParseDeadlineStrict("2026-11-02"); err != nil || got != "2026-11-02" {
		t.Errorf("valid = %q, %v", got, err)
	}
	for _, bad := range []string{"2026-11-2", "tomorrow", "2026-02-30"} {
		if _, err := ParseDeadlineStrict(bad); err == nil {
			t.Errorf("ParseDeadlineStrict(%q) should fail", bad)
		}
	}
}

func TestCompletedAtLifecycle(t *testing.T) {
	completed := NowRFC3339(time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local))
	done := NormalizeItem(Item{Title: "x", Status: StatusDone, CompletedAt: &completed})
	if done.CompletedAt == nil || *done.CompletedAt != completed {
		t.Errorf("done item lost completedAt: %+v", done.CompletedAt)
	}
	todo := NormalizeItem(Item{Title: "x", Status: StatusTodo, CompletedAt: &completed})
	if todo.CompletedAt != nil {
		t.Errorf("non-done item kept completedAt: %+v", todo.CompletedAt)
	}
}

func TestCompletionTimeFallsBackToUpdatedAt(t *testing.T) {
	it := Item{Status: StatusDone, UpdatedAt: "2026-10-21T09:00:00Z"}
	if got := it.CompletionTime(); !got.Equal(time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("fallback = %v", got)
	}
}

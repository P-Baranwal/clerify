package model

import (
	"math"
	"strconv"
	"strings"

	"github.com/P-Baranwal/clerify/internal/format"
)

// NormalizeItem clamps and fills an item so that persisted data is always
// well-formed. The numeric rules mirror normalizeAssignment()/normalizeTask()
// in the web app's model.js: values are clamped, not replaced, so a stored 0
// survives where the oracle keeps it (estimatedHours, priorityScore) and
// becomes the floor where the oracle clamps (difficulty).
func NormalizeItem(in Item) Item {
	out := in

	out.ID = strings.TrimSpace(out.ID)
	out.Title = stringOr(out.Title, "Untitled")
	out.Type = NormalizeType(out.Type)
	out.Status = NormalizeStatus(out.Status)
	out.Description = strings.TrimSpace(out.Description)
	out.RawContent = strings.TrimSpace(out.RawContent)
	out.Notes = strings.TrimSpace(out.Notes)

	out.Deadline = normalizeDeadline(out.Deadline)

	out.Difficulty = clampInt(out.Difficulty, MinDifficulty, MaxDifficulty)
	out.DifficultyReasoning = strings.TrimSpace(out.DifficultyReasoning)
	out.EstimatedHours = clampFloat(out.EstimatedHours, 0, math.Inf(1))
	out.EstimatedHoursReasoning = strings.TrimSpace(out.EstimatedHoursReasoning)
	out.ActualHours = clampFloat(out.ActualHours, 0, math.Inf(1))
	out.PriorityScore = clampInt(out.PriorityScore, MinPriority, MaxPriority)
	out.PriorityReasoning = strings.TrimSpace(out.PriorityReasoning)

	if out.ImpactScore != nil {
		v := clampInt(*out.ImpactScore, MinImpact, MaxImpact)
		out.ImpactScore = &v
		// normalizeTask() recomputes priority from ROI whenever an impact
		// score is present, discarding the AI-assigned priority.
		hoursFloor := math.Max(out.EstimatedHours, 0.5)
		score := clampInt(int(math.Round((float64(v)/hoursFloor)*10)), MinPriority, MaxPriority)
		out.PriorityScore = score
		out.PriorityReasoning = "ROI Priority: " + strconv.Itoa(v) + " impact vs " +
			format.Number(out.EstimatedHours) + "h estimated effort."
	}

	out.Boost = normalizeBoost(out.Boost)
	out.Checklist = normalizeChecklist(out.Checklist)
	out.ProjectID = normalizeOptionalID(out.ProjectID)
	out.BlockerNote = normalizeNote(out.BlockerNote)

	out.CompletedAt = normalizeNote(out.CompletedAt)
	if out.Status != StatusDone {
		out.CompletedAt = nil
	}

	return out
}

// NormalizeProfile mirrors normalizeProfile() with the cloud fields removed.
func NormalizeProfile(p Profile) Profile {
	out := p
	out.Skills = strings.TrimSpace(out.Skills)
	out.PriorityPreset = CanonicalPreset(out.PriorityPreset)
	out.CustomPriorityRule = strings.TrimSpace(out.CustomPriorityRule)
	if out.AvailableHoursPerDay == 0 {
		out.AvailableHoursPerDay = DefaultAvailableHours
	}
	out.AvailableHoursPerDay = clampInt(out.AvailableHoursPerDay, MinAvailable, MaxAvailable)
	out.DefaultProjectID = normalizeOptionalID(out.DefaultProjectID)
	switch out.Theme {
	case ThemeLight, ThemeDark, ThemeSystem:
	default:
		out.Theme = ThemeSystem
	}
	return out
}

// NormalizeProject mirrors normalizeProject().
func NormalizeProject(p Project) Project {
	out := p
	out.ID = strings.TrimSpace(out.ID)
	out.Title = stringOr(out.Title, "Untitled Project")
	out.Brief = strings.TrimSpace(out.Brief)
	if !IsValidProjectStatus(out.Status) {
		out.Status = ProjectActive
	}
	return out
}

// NewItem builds an item with the defaults the web app's manual path uses.
func NewItem(title string) Item {
	return Item{
		ID:             NewID(),
		Title:          stringOr(title, "Untitled"),
		Type:           TypeOther,
		Status:         StatusTodo,
		Difficulty:     DefaultDifficulty,
		EstimatedHours: DefaultEstimatedHours,
		PriorityScore:  DefaultPriority,
		Boost:          Boost{Active: false, Reason: nil, BoostedPriorityScore: nil},
	}
}

func normalizeDeadline(d *string) *string {
	if d == nil {
		return nil
	}
	v := strings.TrimSpace(*d)
	if v == "" {
		return nil
	}
	return &v
}

func normalizeOptionalID(id *string) *string {
	if id == nil {
		return nil
	}
	v := strings.TrimSpace(*id)
	if v == "" {
		return nil
	}
	return &v
}

func normalizeNote(note *string) *string {
	if note == nil {
		return nil
	}
	v := strings.TrimSpace(*note)
	if v == "" {
		return nil
	}
	return &v
}

func normalizeBoost(b Boost) Boost {
	out := b
	if out.Reason != nil {
		v := strings.TrimSpace(*out.Reason)
		if v == "" {
			out.Reason = nil
		} else {
			out.Reason = &v
		}
	}
	if out.BoostedPriorityScore != nil {
		v := clampInt(*out.BoostedPriorityScore, MinPriority, MaxPriority)
		out.BoostedPriorityScore = &v
	}
	return out
}

func normalizeChecklist(in []ChecklistItem) []ChecklistItem {
	if in == nil {
		return nil
	}
	out := make([]ChecklistItem, 0, len(in))
	for _, c := range in {
		text := strings.TrimSpace(c.Text)
		if text == "" {
			continue
		}
		id := strings.TrimSpace(c.ID)
		if id == "" {
			id = NewID()
		}
		out = append(out, ChecklistItem{ID: id, Text: text, Done: c.Done})
	}
	return out
}

// ReconcileChecklist preserves ids and done flags for items whose text is
// unchanged, matching the reconciliation rule in Add.svelte.
func ReconcileChecklist(existing []ChecklistItem, lines []string) []ChecklistItem {
	used := make([]bool, len(existing))
	out := make([]ChecklistItem, 0, len(lines))
	for _, raw := range lines {
		text := strings.TrimSpace(raw)
		if text == "" {
			continue
		}
		found := -1
		for i, c := range existing {
			if !used[i] && c.Text == text {
				found = i
				break
			}
		}
		if found >= 0 {
			used[found] = true
			out = append(out, existing[found])
			continue
		}
		out = append(out, ChecklistItem{ID: NewID(), Text: text})
	}
	return out
}

func stringOr(v, fallback string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return fallback
	}
	return v
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func clampFloat(v, min, max float64) float64 {
	if math.IsNaN(v) {
		return min
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

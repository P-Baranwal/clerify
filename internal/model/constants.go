package model

import (
	"fmt"
	"strings"
)

// SchemaVersion is the current on-disk store schema version.
const SchemaVersion = 2

// Item types.
const (
	TypeEssay    = "Essay"
	TypeCoding   = "Coding"
	TypeMath     = "Math"
	TypeResearch = "Research"
	TypeOther    = "Other"
)

// Types is the closed set of item types. Mirrors TYPES in constants.js.
var Types = []string{TypeEssay, TypeCoding, TypeMath, TypeResearch, TypeOther}

// Statuses. "active" is a legacy alias for StatusTodo.
const (
	StatusTodo       = "todo"
	StatusInProgress = "in_progress"
	StatusBlocked    = "blocked"
	StatusDone       = "done"
)

// Statuses is the closed set of item statuses.
var Statuses = []string{StatusTodo, StatusInProgress, StatusBlocked, StatusDone}

// Priority presets, in the canonical hyphenated spelling.
const (
	PresetDeadlineFirst   = "Deadline-first"
	PresetDifficultyFirst = "Difficulty-first"
	PresetEasiestFirst    = "Easiest-first"
	PresetBalanced        = "Balanced"
)

// Presets is the closed set of priority presets.
var Presets = []string{PresetDeadlineFirst, PresetDifficultyFirst, PresetEasiestFirst, PresetBalanced}

// Project statuses.
const (
	ProjectActive = "active"
	ProjectDone   = "done"
)

// Numeric bounds and defaults. These mirror DIFFICULTY, PRIORITY and IMPACT
// in constants.js plus the fallbacks in model.js.
const (
	DefaultDifficulty     = 5
	DefaultPriority       = 50
	DefaultEstimatedHours = 1
	DefaultAvailableHours = 6

	MinDifficulty = 1
	MaxDifficulty = 10
	MinPriority   = 0
	MaxPriority   = 100
	MinImpact     = 1
	MaxImpact     = 10
	MinAvailable  = 1
	MaxAvailable  = 16
)

// Themes for the TUI.
const (
	ThemeSystem = "system"
	ThemeLight  = "light"
	ThemeDark   = "dark"
)

// NormalizeStatus maps any input to a canonical status. Legacy "active" and
// empty values become "todo", which is what the web app's kanban understands.
func NormalizeStatus(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case StatusTodo, "active", "":
		return StatusTodo
	case StatusInProgress, "in-progress", "inprogress", "started", "doing":
		return StatusInProgress
	case StatusBlocked, "block":
		return StatusBlocked
	case StatusDone, "complete", "completed":
		return StatusDone
	default:
		return StatusTodo
	}
}

// IsValidType reports whether t is one of Types.
func IsValidType(t string) bool {
	for _, v := range Types {
		if v == t {
			return true
		}
	}
	return false
}

// NormalizeType maps any input to one of Types, defaulting to TypeOther.
func NormalizeType(t string) string {
	t = strings.TrimSpace(t)
	if IsValidType(t) {
		return t
	}
	// Case-insensitive match, so "essay" and "ESSAY" both work.
	for _, v := range Types {
		if strings.EqualFold(v, t) {
			return v
		}
	}
	return TypeOther
}

// CanonicalPreset maps user input (including the space-separated labels the
// web app's Settings screen emits, which that screen fails to persist) to a
// canonical preset value. Unknown values fall back to Balanced.
func CanonicalPreset(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	key := strings.NewReplacer(" ", "-", "_", "-").Replace(s)
	switch key {
	case "deadline-first", "deadline", "panic", "panic-mode":
		return PresetDeadlineFirst
	case "difficulty-first", "difficulty", "hard-first", "frog", "frog-eating":
		return PresetDifficultyFirst
	case "easiest-first", "easiest", "easy-first", "momentum":
		return PresetEasiestFirst
	case "balanced", "":
		return PresetBalanced
	default:
		return PresetBalanced
	}
}

// ParseStatus is the strict counterpart to NormalizeStatus. It accepts the
// documented aliases and rejects typos instead of silently falling back.
func ParseStatus(s string) (string, error) {
	raw := strings.ToLower(strings.TrimSpace(s))
	switch raw {
	case StatusTodo, "active", "":
		return StatusTodo, nil
	case StatusInProgress, "in-progress", "inprogress", "started", "doing":
		return StatusInProgress, nil
	case StatusBlocked, "block":
		return StatusBlocked, nil
	case StatusDone, "complete", "completed":
		return StatusDone, nil
	default:
		return "", fmt.Errorf("unknown status %q (use todo, doing, in_progress, blocked, or done)", s)
	}
}

// ParseType is the strict counterpart to NormalizeType.
func ParseType(s string) (string, error) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return TypeOther, nil
	}
	for _, v := range Types {
		if strings.EqualFold(v, raw) {
			return v, nil
		}
	}
	return "", fmt.Errorf("unknown type %q (use Essay, Coding, Math, Research, or Other)", s)
}

// ParsePreset is the strict counterpart to CanonicalPreset.
func ParsePreset(s string) (string, error) {
	raw := strings.ToLower(strings.TrimSpace(s))
	key := strings.NewReplacer(" ", "-", "_", "-").Replace(raw)
	switch key {
	case "deadline-first", "deadline", "panic", "panic-mode":
		return PresetDeadlineFirst, nil
	case "difficulty-first", "difficulty", "hard-first", "frog", "frog-eating":
		return PresetDifficultyFirst, nil
	case "easiest-first", "easiest", "easy-first", "momentum":
		return PresetEasiestFirst, nil
	case "balanced", "":
		return PresetBalanced, nil
	default:
		return "", fmt.Errorf("unknown priority preset %q (use Deadline-first, Difficulty-first, Easiest-first, or Balanced)", s)
	}
}

// ParseTheme is the strict counterpart to NormalizeProfile's theme handling.
func ParseTheme(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case ThemeSystem, "":
		return ThemeSystem, nil
	case ThemeLight:
		return ThemeLight, nil
	case ThemeDark:
		return ThemeDark, nil
	default:
		return "", fmt.Errorf("unknown theme %q (use system, light, or dark)", s)
	}
}

// ValidateHours checks the documented available-hours range.
func ValidateHours(v int) error {
	if v < MinAvailable || v > MaxAvailable {
		return fmt.Errorf("hours must be between %d and %d, got %d", MinAvailable, MaxAvailable, v)
	}
	return nil
}

// IsValidProjectStatus reports whether s is an accepted project status.
func IsValidProjectStatus(s string) bool {
	return s == ProjectActive || s == ProjectDone
}

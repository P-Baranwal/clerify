package model

import (
	"strings"
)

// SchemaVersion is the current on-disk store schema version.
const SchemaVersion = 1

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
	case StatusInProgress, "in-progress", "inprogress", "started":
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

// IsValidProjectStatus reports whether s is an accepted project status.
func IsValidProjectStatus(s string) bool {
	return s == ProjectActive || s == ProjectDone
}

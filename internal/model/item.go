package model

import "time"

// Item is the unified records type. It replaces both Assignment and Task from
// the web app: optional fields are omitted from JSON when empty.
type Item struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Type     string  `json:"type,omitempty"`
	Status   string  `json:"status"`
	Deadline *string `json:"deadline,omitempty"`

	Description string `json:"description,omitempty"`
	RawContent  string `json:"rawContent,omitempty"`
	Notes       string `json:"notes,omitempty"`

	Difficulty              int     `json:"difficulty"`
	DifficultyReasoning     string  `json:"difficultyReasoning,omitempty"`
	EstimatedHours          float64 `json:"estimatedHours"`
	EstimatedHoursReasoning string  `json:"estimatedHoursReasoning,omitempty"`
	ActualHours             float64 `json:"actualHours"`
	ImpactScore             *int    `json:"impactScore,omitempty"`
	PriorityScore           int     `json:"priorityScore"`
	PriorityReasoning       string  `json:"priorityReasoning,omitempty"`

	Boost     Boost           `json:"boost"`
	Checklist []ChecklistItem `json:"checklist,omitempty"`

	ProjectID   *string `json:"projectId,omitempty"`
	BlockerNote *string `json:"blockerNote,omitempty"`

	CreatedAt  string  `json:"createdAt"`
	UpdatedAt  string  `json:"updatedAt"`
	AnalyzedAt *string `json:"analyzedAt,omitempty"`
}

// Boost is a temporary AI re-score that overrides PriorityScore while active.
type Boost struct {
	Active               bool    `json:"active"`
	Reason               *string `json:"reason"`
	BoostedPriorityScore *int    `json:"boostedPriorityScore"`
}

// ChecklistItem is a single checklist entry.
type ChecklistItem struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Done bool   `json:"done"`
}

// EffectiveScore mirrors mapScore() in stores.js.
func (i Item) EffectiveScore() int {
	if i.Boost.Active && i.Boost.BoostedPriorityScore != nil {
		return *i.Boost.BoostedPriorityScore
	}
	return i.PriorityScore
}

// IsDone reports whether the item is complete.
func (i Item) IsDone() bool { return i.Status == StatusDone }

// HasProject reports whether the item belongs to a project.
func (i Item) HasProject() bool { return i.ProjectID != nil && *i.ProjectID != "" }

// Clone returns a deep copy, used for journal snapshots and undo.
func (i Item) Clone() Item {
	out := i
	if i.Deadline != nil {
		v := *i.Deadline
		out.Deadline = &v
	}
	if i.ImpactScore != nil {
		v := *i.ImpactScore
		out.ImpactScore = &v
	}
	if i.ProjectID != nil {
		v := *i.ProjectID
		out.ProjectID = &v
	}
	if i.BlockerNote != nil {
		v := *i.BlockerNote
		out.BlockerNote = &v
	}
	if i.AnalyzedAt != nil {
		v := *i.AnalyzedAt
		out.AnalyzedAt = &v
	}
	if i.Boost.Reason != nil {
		v := *i.Boost.Reason
		out.Boost.Reason = &v
	}
	if i.Boost.BoostedPriorityScore != nil {
		v := *i.Boost.BoostedPriorityScore
		out.Boost.BoostedPriorityScore = &v
	}
	if i.Checklist != nil {
		out.Checklist = make([]ChecklistItem, len(i.Checklist))
		copy(out.Checklist, i.Checklist)
	}
	return out
}

// Project groups items.
type Project struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Brief     string `json:"brief,omitempty"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// Profile holds the user's AI-scoring context. All cloud/tier fields from the
// web app are gone; the API key lives in config.json instead.
type Profile struct {
	Skills               string  `json:"skills"`
	PriorityPreset       string  `json:"priorityPreset"`
	CustomPriorityRule   string  `json:"customPriorityRule"`
	AvailableHoursPerDay int     `json:"availableHoursPerDay"`
	DefaultProjectID     *string `json:"defaultProjectId"`
	Theme                string  `json:"theme"`
}

// PlanBlock is one scheduled block inside a day.
type PlanBlock struct {
	Task  string  `json:"task"`
	Hours float64 `json:"hours"`
	Slot  string  `json:"slot"`
}

// PlanDay is one day of the weekly plan.
type PlanDay struct {
	Day        string      `json:"day"`
	Date       *string     `json:"date"`
	Blocks     []PlanBlock `json:"blocks"`
	TotalHours float64     `json:"totalHours"`
	Note       string      `json:"note,omitempty"`
}

// Plan is the cached weekly plan. Unlike the web app, it is persisted.
type Plan struct {
	GeneratedAt      string    `json:"generatedAt"`
	MaxItemUpdatedAt string    `json:"maxItemUpdatedAt,omitempty"`
	Days             []PlanDay `json:"days"`
}

// Meta is bookkeeping that is not user data.
type Meta struct {
	DeviceID  string `json:"deviceId"`
	CreatedAt string `json:"createdAt"`
}

// Store is the root document persisted in store.json.
type Store struct {
	SchemaVersion int       `json:"schemaVersion"`
	Profile       Profile   `json:"profile"`
	Items         []Item    `json:"items"`
	Projects      []Project `json:"projects"`
	Plan          *Plan     `json:"plan,omitempty"`
	Meta          Meta      `json:"meta"`
}

// NowRFC3339 returns the timestamp format used throughout the store.
func NowRFC3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

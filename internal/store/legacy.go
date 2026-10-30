package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/P-Baranwal/clerify/internal/model"
)

// IsLegacyExport reports whether the document is a web-app backup.
func IsLegacyExport(data []byte) bool {
	var probe struct {
		ExportVersion *float64 `json:"_exportVersion"`
	}
	if json.Unmarshal(data, &probe) != nil {
		return false
	}
	return probe.ExportVersion != nil
}

// IsNativeStore reports whether the document is a store.json file.
func IsNativeStore(data []byte) bool {
	var probe struct {
		SchemaVersion *int `json:"schemaVersion"`
	}
	if json.Unmarshal(data, &probe) != nil {
		return false
	}
	return probe.SchemaVersion != nil
}

// LegacyResult is the converted document plus anything worth telling the user.
type LegacyResult struct {
	Store    model.Store
	Warnings []string
}

// rawCollection accepts either the object map or the array form that
// storage.importAll() tolerates.
type rawCollection map[string]json.RawMessage

func (c *rawCollection) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		*c = rawCollection{}
		return nil
	}
	switch trimmed[0] {
	case '{':
		var m map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &m); err != nil {
			return err
		}
		*c = m
	case '[':
		var arr []json.RawMessage
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return err
		}
		m := rawCollection{}
		for i, raw := range arr {
			var probe struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(raw, &probe)
			id := probe.ID
			if id == "" {
				id = fmt.Sprintf("index-%d", i)
			}
			m[id] = raw
		}
		*c = m
	default:
		return fmt.Errorf("collection is neither an object nor an array")
	}
	return nil
}

type legacyDocument struct {
	ExportVersion *float64      `json:"_exportVersion"`
	Profile       legacyProfile `json:"profile"`
	Theme         string        `json:"theme"`
	Assignments   rawCollection `json:"assignments"`
	Tasks         rawCollection `json:"tasks"`
	Projects      rawCollection `json:"projects"`
	AssignmentIdx []string      `json:"assignmentIndex"`
	TaskIdx       []string      `json:"taskIndex"`
	ProjectIdx    []string      `json:"projectIndex"`
}

type legacyProfile struct {
	Skills               string  `json:"skills"`
	PriorityPreset       string  `json:"priorityPreset"`
	CustomPriorityRule   string  `json:"customPriorityRule"`
	AvailableHoursPerDay *int    `json:"availableHoursPerDay"`
	DefaultProjectID     *string `json:"defaultProjectId"`
	Theme                string  `json:"theme"`
	Mode                 string  `json:"mode"`
	Tier                 string  `json:"tier"`
	Subscription         string  `json:"subscription"`
	UseProxy             *bool   `json:"useProxy"`
	TeamID               *string `json:"teamId"`
	CalendarFeedToken    *string `json:"calendarFeedToken"`
}

type legacyItem struct {
	ID                      string            `json:"id"`
	Title                   string            `json:"title"`
	Type                    string            `json:"type"`
	Description             string            `json:"description"`
	RawContent              string            `json:"rawContent"`
	Status                  string            `json:"status"`
	Deadline                *string           `json:"deadline"`
	Difficulty              *float64          `json:"difficulty"`
	DifficultyReasoning     string            `json:"difficultyReasoning"`
	EstimatedHours          *float64          `json:"estimatedHours"`
	EstimatedHoursReasoning string            `json:"estimatedHoursReasoning"`
	TimeReasoning           string            `json:"timeReasoning"`
	ActualHours             *float64          `json:"actualHours"`
	ImpactScore             *float64          `json:"impactScore"`
	PriorityScore           *float64          `json:"priorityScore"`
	PriorityReasoning       string            `json:"priorityReasoning"`
	Boost                   *legacyBoost      `json:"boost"`
	Checklist               []json.RawMessage `json:"checklist"`
	ProjectID               *string           `json:"projectId"`
	BlockerNote             *string           `json:"blockerNote"`
	Notes                   string            `json:"notes"`
	CreatedAt               string            `json:"createdAt"`
	UpdatedAt               string            `json:"updatedAt"`
	AnalyzedAt              string            `json:"analyzedAt"`
	EntityType              string            `json:"entityType"`
}

type legacyBoost struct {
	Active               bool     `json:"active"`
	Reason               *string  `json:"reason"`
	BoostedPriorityScore *float64 `json:"boostedPriorityScore"`
}

type legacyProject struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	ClientContext string `json:"clientContext"`
	Status        string `json:"status"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

// ParseLegacy converts a web-app export into the unified store.
func ParseLegacy(data []byte) (*LegacyResult, error) {
	var doc legacyDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse legacy export: %w", err)
	}
	if doc.ExportVersion == nil || *doc.ExportVersion < 1 {
		return nil, fmt.Errorf("not a web-app export (missing _exportVersion)")
	}

	res := &LegacyResult{}
	seen := make(map[string]bool)

	convert := func(coll rawCollection, index []string, kind string) {
		for _, id := range orderedIDs(index, coll) {
			raw, ok := coll[id]
			if !ok {
				continue
			}
			var li legacyItem
			if err := json.Unmarshal(raw, &li); err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("skipped malformed %s %s", kind, id))
				continue
			}
			newID := strings.TrimSpace(li.ID)
			if newID == "" {
				newID = id
			}
			if seen[newID] {
				original := newID
				newID = model.NewID()
				res.Warnings = append(res.Warnings,
					fmt.Sprintf("duplicate id %s in the %s collection; reassigned to %s", original, kind, newID))
			}
			seen[newID] = true
			it := model.NormalizeItem(li.toModel(newID))
			if it.Deadline != nil {
				due, derr := model.ParseDeadlineStrict(*it.Deadline)
				if derr != nil {
					res.Warnings = append(res.Warnings, fmt.Sprintf("%s %s: %v", kind, newID, derr))
					it.Deadline = nil
				} else if due == "" {
					it.Deadline = nil
				} else {
					it.Deadline = &due
				}
			}
			res.Store.Items = append(res.Store.Items, it)
		}
	}

	convert(doc.Assignments, doc.AssignmentIdx, "assignment")
	convert(doc.Tasks, doc.TaskIdx, "task")
	res.Store.Projects = legacyProjects(doc.Projects, doc.ProjectIdx)

	res.Store.Profile = model.NormalizeProfile(model.Profile{
		Skills:               doc.Profile.Skills,
		PriorityPreset:       doc.Profile.PriorityPreset,
		CustomPriorityRule:   doc.Profile.CustomPriorityRule,
		AvailableHoursPerDay: intFromIntPtr(doc.Profile.AvailableHoursPerDay, model.DefaultAvailableHours),
		DefaultProjectID:     doc.Profile.DefaultProjectID,
		Theme:                firstNonEmpty(doc.Theme, doc.Profile.Theme, model.ThemeSystem),
	})
	res.Store.SchemaVersion = model.SchemaVersion
	res.Store.Meta = model.Meta{DeviceID: model.NewID(), CreatedAt: model.NowRFC3339(time.Now())}
	if res.Store.Items == nil {
		res.Store.Items = []model.Item{}
	}
	if res.Store.Projects == nil {
		res.Store.Projects = []model.Project{}
	}
	return res, nil
}

func (li legacyItem) toModel(id string) model.Item {
	it := model.Item{
		ID:                      id,
		Title:                   strings.TrimSpace(li.Title),
		Status:                  model.NormalizeStatus(li.Status),
		Description:             strings.TrimSpace(li.Description),
		RawContent:              strings.TrimSpace(li.RawContent),
		Notes:                   strings.TrimSpace(li.Notes),
		Deadline:                li.Deadline,
		Difficulty:              intFromFloatPtr(li.Difficulty, model.DefaultDifficulty),
		DifficultyReasoning:     strings.TrimSpace(li.DifficultyReasoning),
		EstimatedHours:          floatOr(li.EstimatedHours, model.DefaultEstimatedHours),
		EstimatedHoursReasoning: firstNonEmpty(li.EstimatedHoursReasoning, li.TimeReasoning),
		ActualHours:             floatOr(li.ActualHours, 0),
		PriorityScore:           intFromFloatPtr(li.PriorityScore, model.DefaultPriority),
		PriorityReasoning:       strings.TrimSpace(li.PriorityReasoning),
		ProjectID:               li.ProjectID,
		BlockerNote:             li.BlockerNote,
		CreatedAt:               strings.TrimSpace(li.CreatedAt),
		UpdatedAt:               strings.TrimSpace(li.UpdatedAt),
	}
	if strings.TrimSpace(li.Type) != "" {
		it.Type = model.NormalizeType(li.Type)
	}
	if li.ImpactScore != nil {
		v := int(math.Round(*li.ImpactScore))
		it.ImpactScore = &v
	}
	if li.Boost != nil {
		it.Boost = model.Boost{Active: li.Boost.Active, Reason: li.Boost.Reason}
		if li.Boost.BoostedPriorityScore != nil {
			v := int(math.Round(*li.Boost.BoostedPriorityScore))
			it.Boost.BoostedPriorityScore = &v
		}
	}
	if strings.TrimSpace(li.AnalyzedAt) != "" {
		v := strings.TrimSpace(li.AnalyzedAt)
		it.AnalyzedAt = &v
	}
	for _, raw := range li.Checklist {
		if c, ok := legacyChecklistItem(raw); ok {
			it.Checklist = append(it.Checklist, c)
		}
	}
	return it
}

func legacyChecklistItem(raw json.RawMessage) (model.ChecklistItem, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return model.ChecklistItem{}, false
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return model.ChecklistItem{}, false
		}
		text = strings.TrimSpace(text)
		if text == "" {
			return model.ChecklistItem{}, false
		}
		return model.ChecklistItem{ID: model.NewID(), Text: text}, true
	}
	var obj struct {
		ID   string `json:"id"`
		Text string `json:"text"`
		Done bool   `json:"done"`
	}
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return model.ChecklistItem{}, false
	}
	text := strings.TrimSpace(obj.Text)
	if text == "" {
		return model.ChecklistItem{}, false
	}
	id := strings.TrimSpace(obj.ID)
	if id == "" {
		id = model.NewID()
	}
	return model.ChecklistItem{ID: id, Text: text, Done: obj.Done}, true
}

func legacyProjects(coll rawCollection, index []string) []model.Project {
	if coll == nil {
		return []model.Project{}
	}
	out := make([]model.Project, 0, len(coll))
	for _, id := range orderedIDs(index, coll) {
		raw, ok := coll[id]
		if !ok {
			continue
		}
		var lp legacyProject
		if err := json.Unmarshal(raw, &lp); err != nil {
			continue
		}
		pid := strings.TrimSpace(lp.ID)
		if pid == "" {
			pid = id
		}
		out = append(out, model.NormalizeProject(model.Project{
			ID:        pid,
			Title:     lp.Title,
			Brief:     lp.ClientContext,
			Status:    lp.Status,
			CreatedAt: strings.TrimSpace(lp.CreatedAt),
			UpdatedAt: strings.TrimSpace(lp.UpdatedAt),
		}))
	}
	return out
}

// orderedIDs uses the exported index order, then appends any records the index
// forgot about (sorted, for determinism).
func orderedIDs(index []string, coll rawCollection) []string {
	out := make([]string, 0, len(coll))
	seen := make(map[string]bool)
	for _, id := range index {
		if _, ok := coll[id]; ok && !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	rest := make([]string, 0)
	for id := range coll {
		if !seen[id] {
			rest = append(rest, id)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

func intFromFloatPtr(v *float64, fallback int) int {
	if v == nil || math.IsNaN(*v) {
		return fallback
	}
	return int(math.Round(*v))
}

func intFromIntPtr(v *int, fallback int) int {
	if v == nil {
		return fallback
	}
	return *v
}

func floatOr(v *float64, fallback float64) float64 {
	if v == nil || math.IsNaN(*v) {
		return fallback
	}
	return *v
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

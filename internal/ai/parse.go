package ai

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/P-Baranwal/clerify/internal/model"
)

// AnalysisResult is the normal form of the analysis contract. It matches
// normalizeAnalysisResult() in the web app's contract.js, which is deliberately
// more forgiving than the persisted-model normalizer.
type AnalysisResult struct {
	Title                   string
	Type                    string
	Deadline                *string
	Difficulty              int
	DifficultyReasoning     string
	EstimatedHours          float64
	EstimatedHoursReasoning string
	PriorityScore           int
	PriorityReasoning       string
	Checklist               []string
}

// WBSItem is one generated sub-task.
type WBSItem struct {
	Title          string
	EstimatedHours float64
	ImpactScore    int
}

// ExtractedTask is the natural-language extraction result.
type ExtractedTask struct {
	Title          string
	Type           string
	Deadline       *string
	EstimatedHours *float64
	Weight         *string
	Notes          *string
}

// ParseAnalysis parses the JSON object an analysis call returns.
func ParseAnalysis(raw string) (AnalysisResult, error) {
	var obj map[string]any
	if err := json.Unmarshal([]byte(jsonCandidate(raw, '{')), &obj); err != nil {
		return AnalysisResult{}, &ParseError{Message: "Failed to parse JSON from AI response."}
	}

	out := AnalysisResult{
		Title:                   asStringValue(obj["title"]),
		Type:                    model.NormalizeType(asStringValue(obj["type"])),
		Deadline:                asStringPtr(obj["deadline"]),
		DifficultyReasoning:     asStringValue(obj["difficultyReasoning"]),
		EstimatedHoursReasoning: asStringValue(obj["estimatedHoursReasoning"]),
		PriorityReasoning:       asStringValue(obj["priorityReasoning"]),
		Checklist:               checklistStrings(obj["checklist"]),
	}
	if out.Title == "" {
		out.Title = "Untitled"
	}

	// parseInt(x) || 5 — a zero or unparseable value falls back, then clamps.
	difficulty := model.DefaultDifficulty
	if v, ok := asIntValue(obj["difficulty"]); ok && v != 0 {
		difficulty = v
	}
	out.Difficulty = clampInt(difficulty, model.MinDifficulty, model.MaxDifficulty)

	priority := model.DefaultPriority
	if v, ok := asIntValue(obj["priorityScore"]); ok && v != 0 {
		priority = v
	}
	out.PriorityScore = clampInt(priority, model.MinPriority, model.MaxPriority)

	// parseFloat(x) || 1 — zero is treated as "unset" by the oracle.
	hours := 1.0
	if v, ok := asFloatValue(obj["estimatedHours"]); ok && v != 0 {
		hours = v
	}
	if hours < 0 {
		hours = 0
	}
	out.EstimatedHours = hours

	return out, nil
}

// ParseWeeklyPlan parses the JSON array a weekly-plan call returns.
func ParseWeeklyPlan(raw string) ([]model.PlanDay, error) {
	var arr []map[string]any
	if err := json.Unmarshal([]byte(jsonCandidate(raw, '[')), &arr); err != nil {
		return nil, &ParseError{Message: "Failed to parse weekly plan from AI response."}
	}
	if arr == nil {
		return nil, &ParseError{Message: "Failed to parse weekly plan from AI response."}
	}

	days := make([]model.PlanDay, 0, len(arr))
	for _, d := range arr {
		day := model.PlanDay{
			Day:        asStringValue(d["day"]),
			Date:       asStringPtr(d["date"]),
			Blocks:     []model.PlanBlock{},
			TotalHours: asFloatValueOr(d["totalHours"], 0),
			Note:       asStringValue(d["note"]),
		}
		if day.Day == "" {
			day.Day = "Unknown"
		}
		if blocks, ok := d["blocks"].([]any); ok {
			for _, rawBlock := range blocks {
				bm, ok := rawBlock.(map[string]any)
				if !ok {
					continue
				}
				hours := asFloatValueOr(bm["hours"], 1)
				if hours < 0.25 {
					hours = 0.25
				}
				task := asStringValue(bm["task"])
				if task == "" {
					task = "Untitled"
				}
				slot := asStringValue(bm["slot"])
				if slot == "" {
					slot = "Anytime"
				}
				day.Blocks = append(day.Blocks, model.PlanBlock{Task: task, Hours: hours, Slot: slot})
			}
		}
		days = append(days, day)
	}
	return days, nil
}

// ParseWBS parses the JSON array a work-breakdown call returns.
func ParseWBS(raw string) ([]WBSItem, error) {
	var arr []map[string]any
	if err := json.Unmarshal([]byte(jsonCandidate(raw, '[')), &arr); err != nil || arr == nil {
		return nil, &ParseError{Message: "Failed to parse WBS from AI response."}
	}

	items := make([]WBSItem, 0, len(arr))
	for _, it := range arr {
		hours := asFloatValueOr(it["estimatedHours"], 1)
		if hours < 0.25 {
			hours = 0.25
		}
		impact := 5
		if v, ok := asIntValue(it["impactScore"]); ok && v != 0 {
			impact = v
		}
		title := asStringValue(it["title"])
		if title == "" {
			title = "Untitled Task"
		}
		items = append(items, WBSItem{
			Title:          title,
			EstimatedHours: hours,
			ImpactScore:    clampInt(impact, model.MinImpact, model.MaxImpact),
		})
	}
	return items, nil
}

// ParseExtractedTask parses the JSON object a natural-language extraction
// call returns.
func ParseExtractedTask(raw string) (ExtractedTask, error) {
	var obj map[string]any
	if err := json.Unmarshal([]byte(jsonCandidate(raw, '{')), &obj); err != nil {
		return ExtractedTask{}, &ParseError{Message: "Failed to parse task from text."}
	}
	if isTrue(obj["error"]) {
		reason := asStringValue(obj["reason"])
		if reason == "" {
			reason = "Could not extract task from text."
		}
		return ExtractedTask{}, &ParseError{Message: reason}
	}

	out := ExtractedTask{
		Title:    asStringValue(obj["title"]),
		Type:     model.NormalizeType(asStringValue(obj["type"])),
		Deadline: asStringPtr(obj["deadline"]),
		Weight:   asStringPtr(obj["weight"]),
		Notes:    asStringPtr(obj["notes"]),
	}
	if out.Title == "" {
		out.Title = "Untitled Task"
	}
	if v, ok := asFloatValue(obj["estimatedHours"]); ok {
		out.EstimatedHours = &v
	}
	return out, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

// jsonCandidate strips markdown fences and reduces the text to the JSON value
// the caller expects. The oracle uses the same "first bracket to last bracket"
// trick when a model disobeys the no-fences instruction.
func jsonCandidate(raw string, kind byte) string {
	s := strings.TrimSpace(raw)

	if i := strings.Index(s, "```"); i >= 0 {
		rest := s[i+3:]
		if j := strings.Index(rest, "```"); j >= 0 {
			block := strings.TrimSpace(rest[:j])
			if nl := strings.IndexByte(block, '\n'); nl >= 0 {
				head := strings.TrimSpace(block[:nl])
				if head != "" && !strings.ContainsAny(head, "{[") {
					block = strings.TrimSpace(block[nl+1:])
				}
			}
			s = block
		}
	}

	if kind == '{' {
		if i := strings.IndexByte(s, '{'); i >= 0 {
			if j := strings.LastIndexByte(s, '}'); j > i {
				return s[i : j+1]
			}
		}
		return s
	}
	if i := strings.IndexByte(s, '['); i >= 0 {
		if j := strings.LastIndexByte(s, ']'); j > i {
			return s[i : j+1]
		}
	}
	return s
}

func asStringValue(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case json.Number:
		return t.String()
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func asStringPtr(v any) *string {
	s := asStringValue(v)
	if s == "" || strings.EqualFold(s, "null") {
		return nil
	}
	return &s
}

func asFloatValue(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	case string:
		return parseLeadingFloat(t)
	case bool:
		if t {
			return 1, true
		}
		return 0, true
	default:
		return 0, false
	}
}

func asFloatValueOr(v any, fallback float64) float64 {
	if f, ok := asFloatValue(v); ok {
		return f
	}
	return fallback
}

// asIntValue truncates like parseInt, so 5.9 becomes 5.
func asIntValue(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), true
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return int(i), true
		}
		if f, err := t.Float64(); err == nil {
			return int(f), true
		}
		return 0, false
	case string:
		if f, ok := parseLeadingFloat(t); ok {
			return int(f), true
		}
		return 0, false
	default:
		return 0, false
	}
}

func parseLeadingFloat(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	for i := len(s); i > 0; i-- {
		if f, err := strconv.ParseFloat(strings.TrimSpace(s[:i]), 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

func checklistStrings(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		if direct, ok := v.([]string); ok {
			return direct
		}
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		switch t := item.(type) {
		case string:
			if s := strings.TrimSpace(t); s != "" {
				out = append(out, s)
			}
		case map[string]any:
			if s := asStringValue(t["text"]); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func isTrue(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(strings.TrimSpace(t), "true")
	default:
		return false
	}
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

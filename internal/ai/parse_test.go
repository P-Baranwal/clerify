package ai

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/P-Baranwal/clerify/internal/model"
)

func fixedNow() time.Time {
	return time.Date(2026, 10, 26, 9, 0, 0, 0, time.Local)
}

func profileForPrompt() model.Profile {
	return model.Profile{
		Skills:               "comfortable with Go",
		PriorityPreset:       model.PresetBalanced,
		AvailableHoursPerDay: 6,
	}
}

func itemForPrompt() model.Item {
	due := "2026-11-02"
	return model.Item{
		ID:             "abc",
		Title:          "Write the essay",
		Type:           model.TypeEssay,
		Status:         model.StatusTodo,
		Deadline:       &due,
		EstimatedHours: 4,
		PriorityScore:  70,
	}
}

func TestParseAnalysisHandlesFencedJSON(t *testing.T) {
	raw := "```json\n{\"title\":\"Essay\",\"type\":\"Essay\",\"difficulty\":7,\"estimatedHours\":2.5,\"priorityScore\":80,\"checklist\":[\"a\",{\"text\":\"b\"}]}\n```"
	got, err := ParseAnalysis(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Essay" || got.Type != "Essay" {
		t.Errorf("title/type = %q/%q", got.Title, got.Type)
	}
	if got.Difficulty != 7 || got.EstimatedHours != 2.5 || got.PriorityScore != 80 {
		t.Errorf("numbers = %d %v %d", got.Difficulty, got.EstimatedHours, got.PriorityScore)
	}
	if len(got.Checklist) != 2 || got.Checklist[1] != "b" {
		t.Errorf("checklist = %v", got.Checklist)
	}
}

func TestParseAnalysisHandlesProseAndStrings(t *testing.T) {
	raw := `Sure! Here is the JSON:
{"title":"Thing","type":"nonsense","difficulty":"7","estimatedHours":"5.5h","priorityScore":"0"}
Hope that helps.`
	got, err := ParseAnalysis(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != "Other" {
		t.Errorf("type = %q, want Other", got.Type)
	}
	if got.Difficulty != 7 {
		t.Errorf("difficulty = %d, want 7", got.Difficulty)
	}
	if got.EstimatedHours != 5.5 {
		t.Errorf("hours = %v, want 5.5", got.EstimatedHours)
	}
	// parseInt(0) || 50 -> 50
	if got.PriorityScore != 50 {
		t.Errorf("priority = %d, want 50", got.PriorityScore)
	}
}

func TestParseAnalysisDefaultsOnEmptyObject(t *testing.T) {
	got, err := ParseAnalysis(`{}`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Untitled" {
		t.Errorf("title = %q", got.Title)
	}
	if got.Difficulty != 5 || got.PriorityScore != 50 || got.EstimatedHours != 1 {
		t.Errorf("defaults = %d %d %v", got.Difficulty, got.PriorityScore, got.EstimatedHours)
	}
	if got.Type != "Other" {
		t.Errorf("type = %q", got.Type)
	}
}

func TestParseAnalysisRejectsGarbage(t *testing.T) {
	if _, err := ParseAnalysis("I cannot help with that."); err == nil {
		t.Fatal("expected an error")
	}
}

func TestParseAnalysisClampsOutOfRange(t *testing.T) {
	got, err := ParseAnalysis(`{"difficulty":99,"priorityScore":-5,"estimatedHours":-3}`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Difficulty != 10 {
		t.Errorf("difficulty = %d, want 10", got.Difficulty)
	}
	if got.PriorityScore != 0 {
		t.Errorf("priority = %d, want 0", got.PriorityScore)
	}
	if got.EstimatedHours != 0 {
		t.Errorf("hours = %v, want 0", got.EstimatedHours)
	}
}

func TestParseWeeklyPlan(t *testing.T) {
	raw := "```\n[{\"day\":\"Monday\",\"date\":\"2026-10-26\",\"blocks\":[{\"task\":\"A\",\"hours\":0.1,\"slot\":\"\"}],\"totalHours\":\"2\",\"note\":null}]\n```"
	days, err := ParseWeeklyPlan(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 1 {
		t.Fatalf("days = %d", len(days))
	}
	d := days[0]
	if d.Day != "Monday" || d.Date == nil || *d.Date != "2026-10-26" {
		t.Errorf("day = %+v", d)
	}
	if d.TotalHours != 2 {
		t.Errorf("totalHours = %v, want 2", d.TotalHours)
	}
	if len(d.Blocks) != 1 || d.Blocks[0].Hours != 0.25 {
		t.Errorf("block hours should floor at 0.25: %+v", d.Blocks)
	}
	if d.Blocks[0].Slot != "Anytime" {
		t.Errorf("slot = %q", d.Blocks[0].Slot)
	}
}

func TestParseWeeklyPlanRejectsObject(t *testing.T) {
	if _, err := ParseWeeklyPlan(`{"day":"Monday"}`); err == nil {
		t.Fatal("expected an error for a non-array response")
	}
}

func TestParseWBS(t *testing.T) {
	items, err := ParseWBS(`[{"title":"A","estimatedHours":0,"impactScore":99},{"estimatedHours":"2"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d", len(items))
	}
	if items[0].EstimatedHours != 0.25 {
		t.Errorf("hours = %v, want 0.25", items[0].EstimatedHours)
	}
	if items[0].ImpactScore != 10 {
		t.Errorf("impact = %d, want 10", items[0].ImpactScore)
	}
	if items[1].Title != "Untitled Task" || items[1].EstimatedHours != 2 || items[1].ImpactScore != 5 {
		t.Errorf("second item = %+v", items[1])
	}
}

func TestParseExtractedTask(t *testing.T) {
	got, err := ParseExtractedTask(`{"title":"Essay","type":"Essay","deadline":"2026-11-01","estimatedHours":3}`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Essay" || got.EstimatedHours == nil || *got.EstimatedHours != 3 {
		t.Errorf("task = %+v", got)
	}

	if _, err := ParseExtractedTask(`{"error":"true","reason":"too vague"}`); err == nil || err.Error() != "too vague" {
		t.Errorf("error passthrough = %v", err)
	}
}

func TestBuildAnalyzePromptBoostBranch(t *testing.T) {
	existing := itemForPrompt()
	system, user, feature := BuildAnalyzePrompt(AnalyzeInput{
		RawContent:      "ignored",
		Profile:         profileForPrompt(),
		BoostReason:     "exam next week",
		ExistingContext: &existing,
	}, fixedNow())

	if feature != "re_analyze" {
		t.Errorf("feature = %q", feature)
	}
	if !contains(user, "I am applying a boost") || contains(user, "ignored") {
		t.Errorf("boost branch should replace the user message: %q", user)
	}
	if !contains(system, "COMPETENT student") || !contains(system, "NEVER estimate more than 40 hours") {
		t.Errorf("system prompt lost its load-bearing text")
	}
	if !contains(system, "Balanced") {
		t.Errorf("system prompt missing preset")
	}
}

func TestBuildAnalyzePromptWithoutBoostUsesDocument(t *testing.T) {
	_, user, feature := BuildAnalyzePrompt(AnalyzeInput{
		RawContent: "the actual assignment",
		Profile:    profileForPrompt(),
	}, fixedNow())
	if feature != "priority_score" {
		t.Errorf("feature = %q", feature)
	}
	if !contains(user, "the actual assignment") {
		t.Errorf("user message = %q", user)
	}
}

func TestBuildWeeklyPlanPrompt(t *testing.T) {
	system, user := BuildWeeklyPlanPrompt(profileForPrompt(), []model.Item{itemForPrompt()}, fixedNow())
	if !strings.Contains(system, "do NOT overload") || !strings.Contains(system, "6 hours available") {
		t.Errorf("system = %q", system)
	}
	if !strings.Contains(user, `"Write the essay"`) || !strings.Contains(user, "deadline: 2026-11-02") {
		t.Errorf("user = %q", user)
	}
}

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }

func TestParseAnalysisValidatesDeadline(t *testing.T) {
	bad := `{"title":"x","deadline":"tomorrow"}`
	if _, err := ParseAnalysis(bad); err == nil {
		t.Error("invalid deadline should fail analysis")
	}
	got, err := ParseAnalysis(`{"title":"x","deadline":"2026-11-2"}`)
	if err == nil {
		t.Errorf("non-canonical deadline should fail, got %+v", got)
	}
	got, err = ParseAnalysis(`{"title":"x","deadline":"2026-11-02"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Deadline == nil || *got.Deadline != "2026-11-02" {
		t.Errorf("deadline = %v", got.Deadline)
	}
}

func TestBuildWBSPromptJSONEncodesBrief(t *testing.T) {
	_, user := BuildWBSPrompt(`build """this""" safely`, fixedNow())
	if strings.Contains(user, `"""`) {
		t.Errorf("brief can close the prompt block: %q", user)
	}
	if !strings.Contains(user, `build`) {
		t.Errorf("brief content missing: %q", user)
	}
}

func TestDecodeJSONUsesNumber(t *testing.T) {
	var obj map[string]any
	if err := decodeJSON(`{"n":12345678901234567890}`, &obj); err != nil {
		t.Fatal(err)
	}
	if _, ok := obj["n"].(json.Number); !ok {
		t.Errorf("expected json.Number, got %T", obj["n"])
	}
}

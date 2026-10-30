package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/P-Baranwal/clerify/internal/model"
)

func TestStatusDoingAccepted(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, "", "--data-dir", dir, "add", "Task", "--no-ai")
	it := listItems(t, dir)[0]

	if code, _, errOut := runCLI(t, "", "--data-dir", dir, "edit", it.ID[:6], "--status", "doing"); code != 0 {
		t.Fatalf("doing should be accepted: %s", errOut)
	}
	if got := listItems(t, dir)[0].Status; got != model.StatusInProgress {
		t.Errorf("status = %q, want %q", got, model.StatusInProgress)
	}
}

func TestQuickCaptureAdd(t *testing.T) {
	clearKeyEnv(t)
	dir := t.TempDir()
	code, out, errOut := runCLI(t, "", "--data-dir", dir, "add", "essay due friday, about 5h", "--no-ai")
	if code != 0 {
		t.Fatalf("quick capture failed: %s", errOut)
	}
	if !strings.Contains(out, "priority") {
		t.Errorf("output = %q", out)
	}
	items := listItems(t, dir)
	if len(items) != 1 {
		t.Fatalf("items = %d", len(items))
	}
	it := items[0]
	if it.Title != "essay" || it.Type != model.TypeEssay || it.EstimatedHours != 5 {
		t.Errorf("captured item = %+v", it)
	}
	if it.Deadline == nil {
		t.Fatal("deadline was not captured")
	}
}

func TestRowAndTitleReferences(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, "", "--data-dir", dir, "add", "First task", "--due", "2026-11-05", "--no-ai")
	runCLI(t, "", "--data-dir", dir, "add", "Second task", "--due", "2026-12-01", "--no-ai")

	code, out, errOut := runCLI(t, "", "--data-dir", dir, "show", "#1", "--json")
	if code != 0 {
		t.Fatalf("show #1: %s", errOut)
	}
	var first model.Item
	if err := json.Unmarshal([]byte(out), &first); err != nil {
		t.Fatalf("show #1 JSON: %v", err)
	}
	if first.Title != "First task" {
		t.Errorf("#1 = %q, want First task", first.Title)
	}

	code, out, errOut = runCLI(t, "", "--data-dir", dir, "show", "#2", "--json")
	if code != 0 {
		t.Fatalf("show #2: %s", errOut)
	}
	var second model.Item
	if err := json.Unmarshal([]byte(out), &second); err != nil {
		t.Fatalf("show #2 JSON: %v", err)
	}
	if second.Title != "Second task" {
		t.Errorf("#2 = %q, want Second task", second.Title)
	}

	if code, _, errOut := runCLI(t, "", "--data-dir", dir, "done", "#1"); code != 0 {
		t.Fatalf("done #1: %s", errOut)
	}
	for _, it := range listItems(t, dir) {
		if it.Title == "First task" && !it.IsDone() {
			t.Error("done #1 did not complete First task")
		}
	}

	runCLI(t, "", "--data-dir", dir, "add", "Read paper", "--no-ai")
	if code, _, errOut := runCLI(t, "", "--data-dir", dir, "done", "Read"); code != 0 {
		t.Fatalf("done by title: %s", errOut)
	}
	for _, it := range listItems(t, dir) {
		if it.Title == "Read paper" && !it.IsDone() {
			t.Error("title reference did not complete Read paper")
		}
	}
}

func TestAmbiguousTitleListsCandidates(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, "", "--data-dir", dir, "add", "Essay one", "--no-ai")
	runCLI(t, "", "--data-dir", dir, "add", "Essay two", "--no-ai")

	code, _, errOut := runCLI(t, "", "--data-dir", dir, "show", "Essay")
	if code != 3 {
		t.Fatalf("ambiguous title exit = %d, want 3; stderr: %s", code, errOut)
	}
	for _, want := range []string{"Essay one", "Essay two", "ambiguous"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr missing %q: %s", want, errOut)
		}
	}
}

func TestBareClerifyFirstRunAndToday(t *testing.T) {
	clearKeyEnv(t)
	dir := t.TempDir()

	code, out, errOut := runCLI(t, "", "--data-dir", dir)
	if code != 0 {
		t.Fatalf("bare clerify: %s", errOut)
	}
	if !strings.Contains(out, "Welcome to clerify") {
		t.Errorf("first run = %q", out)
	}

	runCLI(t, "", "--data-dir", dir, "add", "Write report", "--no-ai")
	code, out, errOut = runCLI(t, "", "--data-dir", dir)
	if code != 0 {
		t.Fatalf("bare clerify after add: %s", errOut)
	}
	if !strings.Contains(out, "Today") || !strings.Contains(out, "Write report") {
		t.Errorf("bare clerify should show today: %q", out)
	}
}

func TestHelpGroupsAndNoTUI(t *testing.T) {
	dir := t.TempDir()
	code, out, errOut := runCLI(t, "", "--data-dir", dir, "--help")
	if code != 0 {
		t.Fatalf("--help: %s", errOut)
	}
	for _, want := range []string{"Everyday commands:", "Advanced commands:"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "tui") {
		t.Errorf("help still advertises tui:\n%s", out)
	}
}

func TestListHeadersAndEmptyStates(t *testing.T) {
	clearKeyEnv(t)
	dir := t.TempDir()

	if code, out, _ := runCLI(t, "", "--data-dir", dir, "ls"); code != 0 || !strings.Contains(out, "No items yet. Try:") {
		t.Errorf("empty ls = %q", out)
	}

	runCLI(t, "", "--data-dir", dir, "add", "Task", "--no-ai")
	code, out, errOut := runCLI(t, "", "--data-dir", dir, "ls")
	if code != 0 {
		t.Fatalf("ls: %s", errOut)
	}
	for _, want := range []string{"DUE IN", "PRIORITY", "ESTIMATE", "Medium"} {
		if !strings.Contains(out, want) {
			t.Errorf("ls missing %q:\n%s", want, out)
		}
	}
	for _, hidden := range []string{"IMPACT", "PROJECT"} {
		if strings.Contains(out, hidden) {
			t.Errorf("ls should hide unused %q:\n%s", hidden, out)
		}
	}

	runCLI(t, "", "--data-dir", dir, "project", "new", "--title", "Website")
	runCLI(t, "", "--data-dir", dir, "add", "Task two", "--project", "Website", "--impact", "5", "--est", "2", "--no-ai")
	code, out, errOut = runCLI(t, "", "--data-dir", dir, "ls")
	if code != 0 {
		t.Fatalf("ls: %s", errOut)
	}
	for _, want := range []string{"IMPACT", "PROJECT"} {
		if !strings.Contains(out, want) {
			t.Errorf("ls should show used %q:\n%s", want, out)
		}
	}
}

func TestInitAndGuide(t *testing.T) {
	dir := t.TempDir()

	code, out, errOut := runCLI(t, "", "--data-dir", dir, "init", "--no-ai")
	if code != 0 {
		t.Fatalf("init --no-ai: %s", errOut)
	}
	if !strings.Contains(out, "AI is optional") {
		t.Errorf("init --no-ai = %q", out)
	}

	code, out, _ = runCLI(t, "", "--data-dir", dir, "init")
	if code != 0 || !strings.Contains(out, "needs a terminal") {
		t.Errorf("non-interactive init = %q", out)
	}

	code, out, errOut = runCLI(t, "", "--data-dir", dir, "guide")
	if code != 0 {
		t.Fatalf("guide: %s", errOut)
	}
	if !strings.Contains(out, "five steps") {
		t.Errorf("guide = %q", out)
	}
}

func TestTodayAllShowsEmptySections(t *testing.T) {
	clearKeyEnv(t)
	dir := t.TempDir()
	runCLI(t, "", "--data-dir", dir, "add", "Task", "--no-ai")
	code, out, errOut := runCLI(t, "", "--data-dir", dir, "today", "--all")
	if code != 0 {
		t.Fatalf("today --all: %s", errOut)
	}
	for _, want := range []string{"OVERDUE", "DUE TODAY", "TOP PRIORITY", "HIGH ROI", "TODAY'S PLAN"} {
		if !strings.Contains(out, want) {
			t.Errorf("today --all missing %q:\n%s", want, out)
		}
	}
}

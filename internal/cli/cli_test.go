package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/P-Baranwal/clerify/internal/model"
)

func runCLI(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := ExecuteArgs(args, &out, &errOut, strings.NewReader(stdin))
	return code, out.String(), errOut.String()
}

func listItems(t *testing.T, dir string) []model.Item {
	t.Helper()
	code, out, errOut := runCLI(t, "", "--data-dir", dir, "ls", "--all", "--json")
	if code != 0 {
		t.Fatalf("ls failed (%d): %s", code, errOut)
	}
	var items []model.Item
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatalf("ls --json is not valid JSON: %v\n%s", err, out)
	}
	return items
}

func TestAddListAndShow(t *testing.T) {
	dir := t.TempDir()

	code, out, errOut := runCLI(t, "", "--data-dir", dir, "add", "Write the essay",
		"--due", "2026-11-02", "--type", "Essay", "--est", "4", "--no-ai")
	if code != 0 {
		t.Fatalf("add exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "added") {
		t.Errorf("add output = %q", out)
	}

	items := listItems(t, dir)
	if len(items) != 1 {
		t.Fatalf("items = %d", len(items))
	}
	it := items[0]
	if it.Title != "Write the essay" || it.Type != model.TypeEssay || it.EstimatedHours != 4 {
		t.Errorf("item = %+v", it)
	}
	if it.Status != model.StatusTodo {
		t.Errorf("status = %q", it.Status)
	}

	code, out, _ = runCLI(t, "", "--data-dir", dir, "show", it.ID[:6])
	if code != 0 || !strings.Contains(out, "Write the essay") {
		t.Errorf("show exit %d: %q", code, out)
	}
}

func TestUnknownIDExitsThree(t *testing.T) {
	dir := t.TempDir()
	code, _, _ := runCLI(t, "", "--data-dir", dir, "show", "doesnotexist")
	if code != 3 {
		t.Errorf("exit = %d, want 3", code)
	}
}

func TestBadFlagExitsTwo(t *testing.T) {
	dir := t.TempDir()
	code, _, _ := runCLI(t, "", "--data-dir", dir, "ls", "--sort", "nonsense")
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
}

func TestMissingArgumentExitsTwo(t *testing.T) {
	dir := t.TempDir()
	code, _, _ := runCLI(t, "", "--data-dir", dir, "show")
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
}

func TestAnalyzeWithoutKeyExitsFour(t *testing.T) {
	clearKeyEnv(t)

	dir := t.TempDir()
	if code, _, errOut := runCLI(t, "", "--data-dir", dir, "add", "Something", "--no-ai"); code != 0 {
		t.Fatalf("add failed: %s", errOut)
	}
	item := listItems(t, dir)[0]

	code, _, errOut := runCLI(t, "", "--data-dir", dir, "analyze", item.ID[:6])
	if code != 4 {
		t.Errorf("exit = %d, want 4; stderr: %s", code, errOut)
	}
	if !strings.Contains(errOut, "no API key") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestStatusTransitionAndChecklist(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, "", "--data-dir", dir, "add", "Task", "--no-ai", "--check", "step one", "--check", "step two")
	it := listItems(t, dir)[0]
	if len(it.Checklist) != 2 {
		t.Fatalf("checklist = %+v", it.Checklist)
	}

	code, _, errOut := runCLI(t, "", "--data-dir", dir, "block", it.ID[:6], "--note", "waiting")
	if code != 0 {
		t.Fatalf("block: %s", errOut)
	}
	it = listItems(t, dir)[0]
	if it.Status != model.StatusBlocked || it.BlockerNote == nil || *it.BlockerNote != "waiting" {
		t.Errorf("after block = %+v", it)
	}

	runCLI(t, "", "--data-dir", dir, "unblock", it.ID[:6])
	it = listItems(t, dir)[0]
	if it.Status != model.StatusTodo || it.BlockerNote != nil {
		t.Errorf("after unblock = %+v", it)
	}

	code, _, _ = runCLI(t, "", "--data-dir", dir, "edit", it.ID[:6], "--check-done", "step one")
	if code != 0 {
		t.Fatalf("edit exit %d", code)
	}
	it = listItems(t, dir)[0]
	if !it.Checklist[0].Done {
		t.Errorf("checklist not updated: %+v", it.Checklist)
	}
}

func TestDeleteAndUndo(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, "", "--data-dir", dir, "add", "Undo me", "--no-ai")
	it := listItems(t, dir)[0]

	if code, _, errOut := runCLI(t, "", "--data-dir", dir, "rm", it.ID[:6], "--yes"); code != 0 {
		t.Fatalf("rm: %s", errOut)
	}
	if got := len(listItems(t, dir)); got != 0 {
		t.Fatalf("items after rm = %d", got)
	}

	if code, _, errOut := runCLI(t, "", "--data-dir", dir, "undo"); code != 0 {
		t.Fatalf("undo: %s", errOut)
	}
	items := listItems(t, dir)
	if len(items) != 1 || items[0].Title != "Undo me" {
		t.Errorf("after undo = %+v", items)
	}
}

func TestProjectAndWBSDryRun(t *testing.T) {
	clearKeyEnv(t)
	dir := t.TempDir()

	if code, _, errOut := runCLI(t, "", "--data-dir", dir, "project", "new", "--title", "Client portal"); code != 0 {
		t.Fatalf("project new: %s", errOut)
	}

	code, out, _ := runCLI(t, "", "--data-dir", dir, "project", "ls", "--json")
	if code != 0 {
		t.Fatalf("project ls exit %d", code)
	}
	var projects []model.Project
	if err := json.Unmarshal([]byte(out), &projects); err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].Title != "Client portal" {
		t.Errorf("projects = %+v", projects)
	}

	code, out, _ = runCLI(t, "", "--data-dir", dir, "--dry-run", "wbs", "--brief", "build auth")
	if code != 0 {
		t.Errorf("dry-run wbs exit %d", code)
	}
	if !strings.Contains(out, "project planner") {
		t.Errorf("dry-run should print the prompt, got: %q", out[:min(len(out), 120)])
	}
}

func TestLegacyImport(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "export.json")
	payload := `{
	  "_exportVersion": 2,
	  "profile": {"priorityPreset": "Deadline First", "availableHoursPerDay": 5},
	  "assignmentIndex": ["a1"],
	  "assignments": {"a1": {"id":"a1","title":"Imported essay","status":"active","deadline":"2026-11-02","difficulty":0,"estimatedHours":2,"priorityScore":40,"checklist":["one"]}},
	  "taskIndex": [],
	  "tasks": {},
	  "projectIndex": [],
	  "projects": {}
	}`
	if err := os.WriteFile(legacy, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := runCLI(t, "", "--data-dir", dir, "import", legacy, "--yes")
	if code != 0 {
		t.Fatalf("import exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "imported 1 item") {
		t.Errorf("import output = %q", out)
	}

	items := listItems(t, dir)
	if len(items) != 1 {
		t.Fatalf("items = %d", len(items))
	}
	if items[0].Status != model.StatusTodo || items[0].Difficulty != 1 {
		t.Errorf("imported item = %+v", items[0])
	}

	code, out, _ = runCLI(t, "", "--data-dir", dir, "config", "show", "--json")
	if code != 0 {
		t.Fatalf("config show exit %d", code)
	}
	if !strings.Contains(out, "Easiest-first") && !strings.Contains(out, "Deadline-first") {
		t.Errorf("preset not canonicalised: %s", out)
	}
}

func TestExportRoundTrip(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, "", "--data-dir", dir, "add", "Round trip", "--no-ai", "--due", "2026-11-05")

	code, out, _ := runCLI(t, "", "--data-dir", dir, "export", "--csv")
	if code != 0 {
		t.Fatalf("export exit %d", code)
	}
	if !strings.HasPrefix(out, "ID,Title,Type,Status") {
		t.Errorf("csv header = %q", strings.SplitN(out, "\n", 2)[0])
	}

	backup := filepath.Join(dir, "backup.json")
	code, _, _ = runCLI(t, "", "--data-dir", dir, "export", "--json", "--out", backup)
	if code != 0 {
		t.Fatalf("export json exit %d", code)
	}

	if code, _, errOut := runCLI(t, "", "--data-dir", dir, "import", backup, "--yes"); code != 0 {
		t.Fatalf("import: %s", errOut)
	}
	if len(listItems(t, dir)) != 1 {
		t.Errorf("round trip lost items")
	}
}

func TestDoctorAndConfigPath(t *testing.T) {
	dir := t.TempDir()
	code, out, _ := runCLI(t, "", "--data-dir", dir, "doctor")
	if code != 0 {
		t.Errorf("doctor exit %d: %s", code, out)
	}
	if !strings.Contains(out, "data_dir") {
		t.Errorf("doctor output = %q", out)
	}

	code, out, _ = runCLI(t, "", "--data-dir", dir, "config", "path")
	if code != 0 || !strings.Contains(out, "store") {
		t.Errorf("config path exit %d: %q", code, out)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

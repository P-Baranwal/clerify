package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/P-Baranwal/clerify/internal/ai"
	"github.com/P-Baranwal/clerify/internal/config"
	"github.com/P-Baranwal/clerify/internal/model"
	"github.com/P-Baranwal/clerify/internal/store"
)

func fakeAI(t *testing.T, content string) (*httptest.Server, *int, *string) {
	t.Helper()
	var (
		mu    sync.Mutex
		calls int
		auth  string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		auth = r.Header.Get("Authorization")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]any{"content": content}},
			},
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &calls, &auth
}

func writeAIConfig(t *testing.T, dir, baseURL, preset, key string, noAuth bool) {
	t.Helper()
	cfg := config.Default()
	cfg.AI.Preset = preset
	cfg.AI.Wire = "openai"
	cfg.AI.BaseURL = baseURL
	cfg.AI.Model = "test-model"
	cfg.AI.APIKey = key
	cfg.AI.NoAuth = noAuth
	cfg.AI.ExtraBody = nil
	cfg.AI.ExtraHeaders = nil
	cfg.AI.AuthHeader = "Authorization"
	cfg.AI.AuthPrefix = "Bearer"
	if noAuth {
		cfg.AI.AuthHeader = ""
		cfg.AI.AuthPrefix = ""
	}
	cfg.AI.Temperature = 0.2
	cfg.AI.MaxTokens = 32
	cfg.AI.TimeoutSeconds = 5
	if err := cfg.Save(filepath.Join(dir, "config.json")); err != nil {
		t.Fatal(err)
	}
}

func clearKeyEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"CLERIFY_API_KEY", "CUSTOM_API_KEY", "OLLAMA_API_KEY", "OPENAI_API_KEY", "SARVAM_API_KEY"} {
		t.Setenv(name, "")
	}
}

func TestInvalidDeadlineRejected(t *testing.T) {
	dir := t.TempDir()
	for _, due := range []string{"2026-11-2", "tomorrow", "11/02/2026"} {
		code, _, _ := runCLI(t, "", "--data-dir", dir, "add", "--title", "x", "--due", due, "--no-ai")
		if code != 2 {
			t.Errorf("--due %q exit = %d, want 2", due, code)
		}
	}
	code, _, errOut := runCLI(t, "", "--data-dir", dir, "add", "--title", "valid", "--due", "2026-11-02", "--no-ai")
	if code != 0 {
		t.Fatalf("valid deadline failed: %s", errOut)
	}
}

func TestAddTitleOnly(t *testing.T) {
	dir := t.TempDir()
	code, _, errOut := runCLI(t, "", "--data-dir", dir, "add", "--title", "Only a title", "--no-ai")
	if code != 0 {
		t.Fatalf("add --title exit %d: %s", code, errOut)
	}
	items := listItems(t, dir)
	if len(items) != 1 || items[0].Title != "Only a title" {
		t.Errorf("items = %+v", items)
	}
}

func TestChecklistTextWithCommas(t *testing.T) {
	dir := t.TempDir()
	code, _, errOut := runCLI(t, "", "--data-dir", dir, "add", "Task", "--no-ai", "--check", "one, two")
	if code != 0 {
		t.Fatalf("add: %s", errOut)
	}
	it := listItems(t, dir)[0]
	if len(it.Checklist) != 1 || it.Checklist[0].Text != "one, two" {
		t.Errorf("checklist = %+v", it.Checklist)
	}
}

func TestQuietStillPrintsErrors(t *testing.T) {
	dir := t.TempDir()
	code, _, errOut := runCLI(t, "", "--data-dir", dir, "--quiet", "show", "missing")
	if code != 3 {
		t.Errorf("exit = %d, want 3", code)
	}
	if !strings.Contains(errOut, "error:") {
		t.Errorf("quiet suppressed the error: %q", errOut)
	}
}

func TestStrictEnumsRejected(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, "", "--data-dir", dir, "add", "Task", "--no-ai")
	it := listItems(t, dir)[0]
	cases := [][]string{
		{"edit", it.ID[:6], "--status", "donee"},
		{"ls", "--status", "doen"},
		{"add", "x", "--no-ai", "--type", "Nope"},
		{"config", "set", "--theme", "neon"},
		{"config", "set", "--hours", "99"},
		{"config", "set", "--preset", "whatever"},
		{"config", "set", "--table-style", "fancy"},
		{"config", "set", "--date-format", "nope"},
	}
	for _, args := range cases {
		code, _, _ := runCLI(t, "", append([]string{"--data-dir", dir}, args...)...)
		if code != 2 {
			t.Errorf("%v exit = %d, want 2", args, code)
		}
	}
}

func TestUndoConsumesEntry(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, "", "--data-dir", dir, "add", "Undo twice", "--no-ai")
	if code, _, errOut := runCLI(t, "", "--data-dir", dir, "undo"); code != 0 {
		t.Fatalf("first undo: %s", errOut)
	}
	if len(listItems(t, dir)) != 0 {
		t.Fatal("first undo did not remove the created item")
	}
	code, _, errOut := runCLI(t, "", "--data-dir", dir, "undo")
	if code == 0 {
		t.Fatal("second undo should have nothing left to undo")
	}
	if !strings.Contains(errOut, "nothing to undo") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestProjectDeleteUndoRestoresLinks(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, "", "--data-dir", dir, "project", "new", "--title", "P")
	_, projectsOut, _ := runCLI(t, "", "--data-dir", dir, "project", "ls", "--json")
	var projects []model.Project
	if err := json.Unmarshal([]byte(projectsOut), &projects); err != nil || len(projects) != 1 {
		t.Fatalf("projects = %q (%v)", projectsOut, err)
	}
	runCLI(t, "", "--data-dir", dir, "config", "set", "--default-project", "P")
	runCLI(t, "", "--data-dir", dir, "add", "Task", "--project", "P", "--no-ai")
	if it := listItems(t, dir)[0]; it.ProjectID == nil {
		t.Fatal("item was not linked to the project")
	}

	if code, _, errOut := runCLI(t, "", "--data-dir", dir, "project", "rm", "P", "--yes"); code != 0 {
		t.Fatalf("project rm: %s", errOut)
	}
	if it := listItems(t, dir)[0]; it.ProjectID != nil {
		t.Fatal("project delete did not detach the item")
	}
	if code, _, errOut := runCLI(t, "", "--data-dir", dir, "undo"); code != 0 {
		t.Fatalf("undo: %s", errOut)
	}
	it := listItems(t, dir)[0]
	if it.ProjectID == nil || *it.ProjectID != projects[0].ID {
		t.Errorf("undo did not restore the link: %+v", it.ProjectID)
	}
	_, cfgOut, _ := runCLI(t, "", "--data-dir", dir, "config", "show", "--json")
	var cfg struct {
		Profile model.Profile `json:"profile"`
	}
	if err := json.Unmarshal([]byte(cfgOut), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Profile.DefaultProjectID == nil || *cfg.Profile.DefaultProjectID != projects[0].ID {
		t.Errorf("undo did not restore the default project: %+v", cfg.Profile.DefaultProjectID)
	}
}

func TestCompletedAtSurvivesEdit(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, "", "--data-dir", dir, "add", "Task", "--no-ai")
	it := listItems(t, dir)[0]
	if code, _, errOut := runCLI(t, "", "--data-dir", dir, "done", it.ID[:6]); code != 0 {
		t.Fatalf("done: %s", errOut)
	}
	done := listItems(t, dir)[0]
	if done.CompletedAt == nil {
		t.Fatal("done did not set completedAt")
	}
	completed := *done.CompletedAt
	if code, _, errOut := runCLI(t, "", "--data-dir", dir, "edit", it.ID[:6], "--title", "Renamed"); code != 0 {
		t.Fatalf("edit: %s", errOut)
	}
	edited := listItems(t, dir)[0]
	if edited.CompletedAt == nil || *edited.CompletedAt != completed {
		t.Errorf("editing a done item moved completedAt: %v -> %v", completed, edited.CompletedAt)
	}
}

func TestJSONOutputsForDigestRmUndoRescore(t *testing.T) {
	dir := t.TempDir()
	code, out, _ := runCLI(t, "", "--data-dir", dir, "digest", "--json")
	if code != 0 || !json.Valid([]byte(out)) {
		t.Fatalf("digest --json = %d %q", code, out)
	}

	runCLI(t, "", "--data-dir", dir, "add", "Task", "--no-ai")
	it := listItems(t, dir)[0]
	code, out, _ = runCLI(t, "", "--data-dir", dir, "rm", it.ID[:6], "--yes", "--json")
	if code != 0 || !strings.Contains(out, "deleted") || !json.Valid([]byte(out)) {
		t.Fatalf("rm --json = %d %q", code, out)
	}
	code, out, _ = runCLI(t, "", "--data-dir", dir, "undo", "--json")
	if code != 0 || !strings.Contains(out, "undone") || !json.Valid([]byte(out)) {
		t.Fatalf("undo --json = %d %q", code, out)
	}
	code, out, _ = runCLI(t, "", "--data-dir", dir, "rescore", "--json")
	if code != 0 || !json.Valid([]byte(out)) {
		t.Fatalf("rescore --json = %d %q", code, out)
	}
}

func TestCSVRoundTripAllFields(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, "", "--data-dir", dir, "project", "new", "--title", "P")
	code, _, errOut := runCLI(t, "", "--data-dir", dir, "add", "Task", "--no-ai",
		"--project", "P", "--check", "one, two", "--note", "note", "--desc", "desc",
		"--impact", "7", "--est", "3", "--due", "2026-11-02")
	if code != 0 {
		t.Fatalf("add: %s", errOut)
	}
	it := listItems(t, dir)[0]
	runCLI(t, "", "--data-dir", dir, "edit", it.ID[:6], "--check-done", "one, two")

	csvPath := filepath.Join(dir, "items.csv")
	if code, _, errOut := runCLI(t, "", "--data-dir", dir, "export", "--csv", "--out", csvPath); code != 0 {
		t.Fatalf("export csv: %s", errOut)
	}

	dir2 := t.TempDir()
	if code, _, errOut := runCLI(t, "", "--data-dir", dir2, "import", csvPath, "--from-csv", "--yes"); code != 0 {
		t.Fatalf("import csv: %s", errOut)
	}
	got := listItems(t, dir2)[0]
	if got.Title != "Task" || got.Notes != "note" || got.Description != "desc" {
		t.Errorf("text fields lost: %+v", got)
	}
	if got.ProjectID == nil || *got.ProjectID != *it.ProjectID {
		t.Errorf("project lost: %+v", got.ProjectID)
	}
	if got.ImpactScore == nil || *got.ImpactScore != 7 {
		t.Errorf("impact lost: %+v", got.ImpactScore)
	}
	if len(got.Checklist) != 1 || got.Checklist[0].Text != "one, two" || !got.Checklist[0].Done {
		t.Errorf("checklist lost: %+v", got.Checklist)
	}
	if got.Difficulty != it.Difficulty {
		t.Errorf("difficulty lost: %d vs %d", got.Difficulty, it.Difficulty)
	}
}

func TestCSVMergeKeepsNewerItem(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, "", "--data-dir", dir, "add", "Original", "--no-ai")
	it := listItems(t, dir)[0]
	backup := filepath.Join(dir, "backup.json")
	if code, _, errOut := runCLI(t, "", "--data-dir", dir, "export", "--out", backup); code != 0 {
		t.Fatalf("export: %s", errOut)
	}
	runCLI(t, "", "--data-dir", dir, "edit", it.ID[:6], "--title", "Edited")
	if code, _, errOut := runCLI(t, "", "--data-dir", dir, "import", backup, "--merge", "--yes"); code != 0 {
		t.Fatalf("import merge: %s", errOut)
	}
	got := listItems(t, dir)[0]
	if got.Title != "Edited" {
		t.Errorf("older import clobbered a newer edit: %q", got.Title)
	}
}

func TestICSFormat(t *testing.T) {
	dir := t.TempDir()
	longTitle := "A very long title, with a comma, that should be folded across several lines " + strings.Repeat("x", 80)
	runCLI(t, "", "--data-dir", dir, "add", "--title", longTitle, "--due", "2026-11-02", "--no-ai")
	it := listItems(t, dir)[0]
	runCLI(t, "", "--data-dir", dir, "done", it.ID[:6])

	code, out, _ := runCLI(t, "", "--data-dir", dir, "export", "--ics")
	if code != 0 {
		t.Fatalf("export ics exit %d", code)
	}
	if !strings.Contains(out, "\r\n") {
		t.Error("ICS output does not use CRLF")
	}
	if strings.Contains(out, "STATUS:COMPLETED") {
		t.Error("VEVENT must not use STATUS:COMPLETED")
	}
	if !strings.Contains(out, "STATUS:CONFIRMED") || !strings.Contains(out, "X-CLERIFY-COMPLETED:TRUE") {
		t.Error("missing status markers")
	}
	for _, line := range strings.Split(out, "\r\n") {
		if len(line) > 75 {
			t.Errorf("unfolded line (%d bytes): %q", len(line), line)
		}
	}
}

func TestAPIKeyFlagWinsOverEnvironment(t *testing.T) {
	clearKeyEnv(t)
	srv, _, auth := fakeAI(t, "ok")
	dir := t.TempDir()
	writeAIConfig(t, dir, srv.URL, "custom", "config-key", false)
	t.Setenv("CLERIFY_API_KEY", "env-key")
	t.Setenv("CUSTOM_API_KEY", "preset-env-key")

	code, _, errOut := runCLI(t, "", "--data-dir", dir, "ai", "test", "--api-key", "flag-key")
	if code != 0 {
		t.Fatalf("ai test: %s", errOut)
	}
	if *auth != "Bearer flag-key" {
		t.Errorf("server saw %q, want the --api-key value", *auth)
	}
}

func TestLocalPresetNeedsNoKey(t *testing.T) {
	clearKeyEnv(t)
	srv, _, auth := fakeAI(t, "ok")
	dir := t.TempDir()
	writeAIConfig(t, dir, srv.URL, "ollama", "", true)

	code, _, errOut := runCLI(t, "", "--data-dir", dir, "ai", "test")
	if code != 0 {
		t.Fatalf("local preset failed: %s", errOut)
	}
	if *auth != "" {
		t.Errorf("local preset sent auth: %q", *auth)
	}
}

func TestProjectNewWBSJSON(t *testing.T) {
	clearKeyEnv(t)
	srv, _, _ := fakeAI(t, `[{"title":"Subtask","estimatedHours":2,"impactScore":7}]`)
	dir := t.TempDir()
	writeAIConfig(t, dir, srv.URL, "custom", "k", false)

	code, out, errOut := runCLI(t, "", "--data-dir", dir, "project", "new", "--title", "P", "--brief", "B", "--wbs", "--json")
	if code != 0 {
		t.Fatalf("project new --wbs --json: %s", errOut)
	}
	var payload struct {
		Project model.Project `json:"project"`
		Items   []model.Item  `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if payload.Project.Title != "P" || len(payload.Items) != 1 || payload.Items[0].Title != "Subtask" {
		t.Errorf("payload = %+v", payload)
	}
}

func TestWBSApplyCallsAICOnce(t *testing.T) {
	clearKeyEnv(t)
	srv, calls, _ := fakeAI(t, `[{"title":"A","estimatedHours":1,"impactScore":5}]`)
	dir := t.TempDir()
	writeAIConfig(t, dir, srv.URL, "custom", "k", false)

	code, _, errOut := runCLI(t, "", "--data-dir", dir, "wbs", "--brief", "B", "--apply", "--json")
	if code != 0 {
		t.Fatalf("wbs --apply: %s", errOut)
	}
	if *calls != 1 {
		t.Errorf("AI was called %d times, want 1", *calls)
	}
}

func TestPlanRefreshJSONIsOnlyJSON(t *testing.T) {
	clearKeyEnv(t)
	srv, _, _ := fakeAI(t, `[{"day":"Monday","date":"2026-10-26","blocks":[],"totalHours":0}]`)
	dir := t.TempDir()
	writeAIConfig(t, dir, srv.URL, "custom", "k", false)
	runCLI(t, "", "--data-dir", dir, "add", "Task", "--no-ai")

	code, out, errOut := runCLI(t, "", "--data-dir", dir, "plan", "--refresh", "--json")
	if code != 0 {
		t.Fatalf("plan --refresh --json: %s", errOut)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("stdout is not pure JSON: %q", out)
	}
}

func TestExitCodeFiveForUnreadableStore(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "store.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := runCLI(t, "", "--data-dir", dir, "ls")
	if code != 5 {
		t.Errorf("exit = %d, want 5; stderr: %s", code, errOut)
	}
}

func TestLocalPresetConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	writeAIConfig(t, dir, "http://localhost:11434/v1", "ollama", "", true)
	path := filepath.Join(dir, "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.AI.NoAuth {
		t.Error("no_auth was not persisted")
	}
}

func TestConfigExtraBodyCanBeCleared(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	payload := `{"ai":{"preset":"anthropic","base_url":"https://example.test","model":"m","extra_body":{}}}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.ExtraBody == nil || len(cfg.AI.ExtraBody) != 0 {
		t.Errorf("extra_body = %#v, want an explicit empty map", cfg.AI.ExtraBody)
	}
}

func TestMaskKeyNeverRevealsMoreThanFour(t *testing.T) {
	for _, key := range []string{"1234", "12345", "123456789", strings.Repeat("a", 40)} {
		masked := maskKey(key)
		if strings.Count(masked, "•") == 0 && len(key) > 4 {
			t.Errorf("maskKey(%q) = %q", key, masked)
		}
		if len(key) > 4 && !strings.HasSuffix(masked, key[len(key)-4:]) {
			t.Errorf("maskKey(%q) = %q, want last four preserved", key, masked)
		}
	}
}

func failingAI(t *testing.T, status int) (*httptest.Server, *int) {
	t.Helper()
	var (
		mu    sync.Mutex
		calls int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":{"message":"nope"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestReportMarkdownAndLast(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, "", "--data-dir", dir, "add", "Task", "--no-ai")
	it := listItems(t, dir)[0]
	runCLI(t, "", "--data-dir", dir, "done", it.ID[:6], "--actual", "3")

	code, out, errOut := runCLI(t, "", "--data-dir", dir, "report", "--md")
	if code != 0 {
		t.Fatalf("report --md: %s", errOut)
	}
	if !strings.Contains(out, "## SUMMARY") || !strings.Contains(out, "| WEEK |") {
		t.Errorf("markdown report missing sections: %q", out)
	}

	code, out, errOut = runCLI(t, "", "--data-dir", dir, "report", "--json", "--last", "2")
	if code != 0 {
		t.Fatalf("report --json --last: %s", errOut)
	}
	var payload struct {
		Velocity []any `json:"velocity"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Velocity) != 2 {
		t.Errorf("velocity buckets = %d, want 2", len(payload.Velocity))
	}
}

func TestTodayJSONIncludesInboxAndHighROI(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, "", "--data-dir", dir, "add", "Inbox task", "--no-ai", "--impact", "7", "--est", "1")
	code, out, errOut := runCLI(t, "", "--data-dir", dir, "today", "--json")
	if code != 0 {
		t.Fatalf("today --json: %s", errOut)
	}
	var payload struct {
		Inbox   []model.Item `json:"inbox"`
		HighROI []model.Item `json:"highRoi"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Inbox) != 1 || len(payload.HighROI) != 1 {
		t.Errorf("inbox=%d highROI=%d", len(payload.Inbox), len(payload.HighROI))
	}
}

func TestProjectShowBoard(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, "", "--data-dir", dir, "project", "new", "--title", "P")
	runCLI(t, "", "--data-dir", dir, "add", "Task", "--project", "P", "--no-ai")
	code, out, errOut := runCLI(t, "", "--data-dir", dir, "project", "show", "P", "--board")
	if code != 0 {
		t.Fatalf("project show --board: %s", errOut)
	}
	if !strings.Contains(out, "TODO (1)") {
		t.Errorf("board output = %q", out)
	}
}

func TestCSVReplacePreservesProjects(t *testing.T) {
	src := t.TempDir()
	runCLI(t, "", "--data-dir", src, "project", "new", "--title", "P")
	runCLI(t, "", "--data-dir", src, "add", "Task", "--project", "P", "--no-ai")
	csvPath := filepath.Join(src, "items.csv")
	if code, _, errOut := runCLI(t, "", "--data-dir", src, "export", "--csv", "--out", csvPath); code != 0 {
		t.Fatalf("export: %s", errOut)
	}

	dst := t.TempDir()
	runCLI(t, "", "--data-dir", dst, "project", "new", "--title", "Keep")
	if code, _, errOut := runCLI(t, "", "--data-dir", dst, "import", csvPath, "--from-csv", "--yes"); code != 0 {
		t.Fatalf("import: %s", errOut)
	}
	_, projectsOut, _ := runCLI(t, "", "--data-dir", dst, "project", "ls", "--json")
	var projects []model.Project
	if err := json.Unmarshal([]byte(projectsOut), &projects); err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].Title != "Keep" {
		t.Errorf("CSV replace wiped projects: %+v", projects)
	}
}

func TestRescoreAllFailedExitsFour(t *testing.T) {
	clearKeyEnv(t)
	srv, _ := failingAI(t, http.StatusInternalServerError)
	dir := t.TempDir()
	writeAIConfig(t, dir, srv.URL, "custom", "k", false)
	runCLI(t, "", "--data-dir", dir, "add", "Task", "--no-ai", "--desc", "details")

	code, out, _ := runCLI(t, "", "--data-dir", dir, "rescore", "--json")
	if code != 4 {
		t.Errorf("exit = %d, want 4; output: %s", code, out)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("stdout is not JSON: %q", out)
	}
}

func TestRescoreAuthErrorStops(t *testing.T) {
	clearKeyEnv(t)
	srv, calls := failingAI(t, http.StatusUnauthorized)
	dir := t.TempDir()
	writeAIConfig(t, dir, srv.URL, "custom", "k", false)
	runCLI(t, "", "--data-dir", dir, "add", "One", "--no-ai", "--desc", "details")
	runCLI(t, "", "--data-dir", dir, "add", "Two", "--no-ai", "--desc", "details")

	code, _, _ := runCLI(t, "", "--data-dir", dir, "rescore")
	if code != 4 {
		t.Errorf("exit = %d, want 4", code)
	}
	if *calls != 1 {
		t.Errorf("stopped after %d calls, want 1 auth failure", *calls)
	}
}

func TestProjectNewWBSRequiresBriefBeforeCreating(t *testing.T) {
	dir := t.TempDir()
	code, _, _ := runCLI(t, "", "--data-dir", dir, "project", "new", "--title", "P", "--wbs")
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	_, projectsOut, _ := runCLI(t, "", "--data-dir", dir, "project", "ls", "--json")
	var projects []model.Project
	_ = json.Unmarshal([]byte(projectsOut), &projects)
	if len(projects) != 0 {
		t.Errorf("an empty project was created: %+v", projects)
	}
}

func TestAnalyzeRePreservesManualFields(t *testing.T) {
	clearKeyEnv(t)
	srv, _, _ := fakeAI(t, `{"title":"AI title","type":"Coding","deadline":"2027-01-01","difficulty":9,"estimatedHours":40,"priorityScore":99,"priorityReasoning":"r"}`)
	dir := t.TempDir()
	writeAIConfig(t, dir, srv.URL, "custom", "k", false)
	runCLI(t, "", "--data-dir", dir, "add", "Manual title", "--due", "2026-12-01", "--est", "5", "--no-ai")
	it := listItems(t, dir)[0]

	code, _, errOut := runCLI(t, "", "--data-dir", dir, "analyze", it.ID[:6], "--re")
	if code != 0 {
		t.Fatalf("analyze --re: %s", errOut)
	}
	got := listItems(t, dir)[0]
	if got.Title != "Manual title" || got.Deadline == nil || *got.Deadline != "2026-12-01" || got.EstimatedHours != 5 {
		t.Errorf("manual fields were overwritten: %+v", got)
	}
	if got.Difficulty != 9 || got.PriorityScore != 99 {
		t.Errorf("AI fields were not refreshed: %+v", got)
	}
}

func TestRescorePreservesManualFields(t *testing.T) {
	clearKeyEnv(t)
	srv, _, _ := fakeAI(t, `{"title":"AI title","type":"Coding","deadline":"2027-01-01","difficulty":8,"estimatedHours":30,"priorityScore":88,"priorityReasoning":"r"}`)
	dir := t.TempDir()
	writeAIConfig(t, dir, srv.URL, "custom", "k", false)
	runCLI(t, "", "--data-dir", dir, "add", "Manual title", "--due", "2026-12-01", "--est", "5", "--desc", "details", "--no-ai")

	if code, _, errOut := runCLI(t, "", "--data-dir", dir, "rescore"); code != 0 {
		t.Fatalf("rescore: %s", errOut)
	}
	got := listItems(t, dir)[0]
	if got.Title != "Manual title" || got.Deadline == nil || *got.Deadline != "2026-12-01" || got.EstimatedHours != 5 {
		t.Errorf("rescore overwrote manual fields: %+v", got)
	}
	if got.Difficulty != 8 || got.PriorityScore != 88 {
		t.Errorf("rescore did not update AI fields: %+v", got)
	}
}

func TestCSVFormulaInjectionIsEscapedAndRoundTrips(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, "", "--data-dir", dir, "add", "--title", "=1+1", "--no-ai")
	csvPath := filepath.Join(dir, "items.csv")
	if code, _, errOut := runCLI(t, "", "--data-dir", dir, "export", "--csv", "--out", csvPath); code != 0 {
		t.Fatalf("export: %s", errOut)
	}
	raw, err := os.ReadFile(csvPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "'=1+1") {
		t.Errorf("formula cell was not escaped: %q", string(raw))
	}

	dst := t.TempDir()
	if code, _, errOut := runCLI(t, "", "--data-dir", dst, "import", csvPath, "--from-csv", "--yes"); code != 0 {
		t.Fatalf("import: %s", errOut)
	}
	if got := listItems(t, dst)[0].Title; got != "=1+1" {
		t.Errorf("title = %q, want the original value", got)
	}
}

func TestPresetSwitchClearsOldKey(t *testing.T) {
	clearKeyEnv(t)
	dir := t.TempDir()
	writeAIConfig(t, dir, "https://example.test/v1", "custom", "secret", false)
	code, out, errOut := runCLI(t, "", "--data-dir", dir, "ai", "preset", "deepseek", "--json")
	if code != 0 {
		t.Fatalf("ai preset: %s", errOut)
	}
	var cfg struct {
		APIKey string `json:"api_key"`
	}
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "" {
		t.Errorf("old key followed the preset switch: %q", cfg.APIKey)
	}
}

func TestBoostClearDryRunDoesNotWrite(t *testing.T) {
	clearKeyEnv(t)
	srv, _, _ := fakeAI(t, `{"difficulty":5,"estimatedHours":1,"priorityScore":77,"priorityReasoning":"boosted"}`)
	dir := t.TempDir()
	writeAIConfig(t, dir, srv.URL, "custom", "k", false)
	runCLI(t, "", "--data-dir", dir, "add", "Boosted", "--no-ai")
	it := listItems(t, dir)[0]
	if code, _, errOut := runCLI(t, "", "--data-dir", dir, "boost", it.ID[:6], "--reason", "exam"); code != 0 {
		t.Fatalf("boost: %s", errOut)
	}
	if !listItems(t, dir)[0].Boost.Active {
		t.Fatal("boost was not applied")
	}

	if code, _, errOut := runCLI(t, "", "--data-dir", dir, "boost", it.ID[:6], "--clear", "--dry-run"); code != 0 {
		t.Fatalf("clear dry-run: %s", errOut)
	}
	if !listItems(t, dir)[0].Boost.Active {
		t.Error("--dry-run cleared the boost in the store")
	}
}

func TestRescoreJournalsOneBatchAndUndoes(t *testing.T) {
	clearKeyEnv(t)
	srv, _, _ := fakeAI(t, `{"difficulty":8,"estimatedHours":3,"priorityScore":91,"priorityReasoning":"r"}`)
	dir := t.TempDir()
	writeAIConfig(t, dir, srv.URL, "custom", "k", false)
	for _, title := range []string{"One", "Two", "Three"} {
		runCLI(t, "", "--data-dir", dir, "add", title, "--no-ai", "--desc", "details")
	}
	before := listItems(t, dir)

	if code, _, errOut := runCLI(t, "", "--data-dir", dir, "rescore"); code != 0 {
		t.Fatalf("rescore: %s", errOut)
	}
	paths, err := store.ResolvePaths(dir)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := store.ReadJournal(paths)
	if err != nil {
		t.Fatal(err)
	}
	rescores := 0
	for _, e := range entries {
		if e.Op == store.OpRescore {
			rescores++
		}
	}
	if rescores != 1 {
		t.Errorf("rescore journal entries = %d, want 1", rescores)
	}

	if code, _, errOut := runCLI(t, "", "--data-dir", dir, "undo"); code != 0 {
		t.Fatalf("undo: %s", errOut)
	}
	after := listItems(t, dir)
	if len(after) != len(before) {
		t.Fatalf("undo lost items: %d -> %d", len(before), len(after))
	}
	for i := range after {
		if after[i].PriorityScore != before[i].PriorityScore {
			t.Errorf("undo did not restore priority for %s: %d -> %d", after[i].Title, before[i].PriorityScore, after[i].PriorityScore)
		}
	}
}

func TestConfigSetUISettings(t *testing.T) {
	dir := t.TempDir()
	code, _, errOut := runCLI(t, "", "--data-dir", dir, "config", "set", "--table-style", "ascii", "--date-format", "02/01/2006")
	if code != 0 {
		t.Fatalf("config set ui: %s", errOut)
	}
	_, out, _ := runCLI(t, "", "--data-dir", dir, "config", "show", "--json")
	var cfg config.Config
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.UI.TableStyle != "ascii" || cfg.UI.DateFormat != "02/01/2006" {
		t.Errorf("ui = %+v", cfg.UI)
	}
}

func TestRedactAIConfigMasksExtraHeaders(t *testing.T) {
	cfg := ai.Config{
		APIKey:       "supersecretkey",
		ExtraHeaders: map[string]string{"Authorization": "Bearer topsecret"},
	}
	red := redactAIConfig(cfg)
	if red.APIKey == cfg.APIKey {
		t.Error("api key was not masked")
	}
	if red.ExtraHeaders["Authorization"] == cfg.ExtraHeaders["Authorization"] {
		t.Error("extra header value was not masked")
	}
	if cfg.ExtraHeaders["Authorization"] != "Bearer topsecret" {
		t.Error("redaction mutated the original config")
	}
}

func TestUndoUpdatePreservesRawContent(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "input.txt")
	if err := os.WriteFile(src, []byte("Assignment line one\nline two with details\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := runCLI(t, "", "--data-dir", dir, "add", "--file", src, "--no-ai"); code != 0 {
		t.Fatalf("add --file: %s", errOut)
	}
	it := listItems(t, dir)[0]
	if it.RawContent == "" {
		t.Fatal("raw content was not stored")
	}
	if code, _, errOut := runCLI(t, "", "--data-dir", dir, "edit", it.ID[:6], "--title", "Renamed"); code != 0 {
		t.Fatalf("edit: %s", errOut)
	}
	if code, _, errOut := runCLI(t, "", "--data-dir", dir, "undo"); code != 0 {
		t.Fatalf("undo: %s", errOut)
	}
	got := listItems(t, dir)[0]
	if got.RawContent != it.RawContent {
		t.Errorf("undo lost raw content: %q -> %q", it.RawContent, got.RawContent)
	}
}

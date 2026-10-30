package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/P-Baranwal/clerify/internal/model"
)

func openTemp(t *testing.T) (*DB, Paths) {
	t.Helper()
	paths, err := ResolvePaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	db, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	return db, paths
}

func TestAddFindAndReload(t *testing.T) {
	db, paths := openTemp(t)

	saved, err := db.AddItem(model.NewItem("Write essay"))
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID == "" || saved.CreatedAt == "" || saved.UpdatedAt == "" {
		t.Fatalf("not stamped: %+v", saved)
	}

	reopened, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.FindItem(saved.ID[:6])
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Write essay" {
		t.Errorf("title = %q", got.Title)
	}
}

func TestFindItemAmbiguousPrefix(t *testing.T) {
	db, _ := openTemp(t)
	for _, id := range []string{"abcdef01-0000-4000-8000-000000000000", "abcdef02-0000-4000-8000-000000000000"} {
		it := model.NewItem("x")
		it.ID = id
		if _, err := db.AddItem(it); err != nil {
			t.Fatal(err)
		}
	}
	_, err := db.FindItem("abcdef")
	var ambiguous *AmbiguousError
	if err == nil {
		t.Fatal("expected ambiguity")
	}
	if !asAmbiguous(err, &ambiguous) || len(ambiguous.Candidates) != 2 {
		t.Errorf("err = %v", err)
	}
	if _, err := db.FindItem("abcdef01"); err != nil {
		t.Errorf("unique prefix should resolve: %v", err)
	}
}

func asAmbiguous(err error, target **AmbiguousError) bool {
	v, ok := err.(*AmbiguousError)
	if ok {
		*target = v
	}
	return ok
}

func TestCorruptStoreFallsBackToBackup(t *testing.T) {
	paths, err := ResolvePaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.Dir, 0o700); err != nil {
		t.Fatal(err)
	}

	due := "2026-11-02"
	backup := model.Store{
		SchemaVersion: model.SchemaVersion,
		Profile:       model.NormalizeProfile(model.Profile{}),
		Items:         []model.Item{model.NormalizeItem(model.Item{ID: "abc", Title: "from backup", Deadline: &due})},
		Projects:      []model.Project{},
	}
	data, _ := json.Marshal(backup)
	if err := os.WriteFile(paths.BackupFile, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.StoreFile, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	if !db.Recovered {
		t.Error("expected Recovered to be true")
	}
	if len(db.Items()) != 1 || db.Items()[0].Title != "from backup" {
		t.Errorf("items = %+v", db.Items())
	}
}

func TestSaveWritesBackupAndIsValidJSON(t *testing.T) {
	db, paths := openTemp(t)
	if _, err := db.AddItem(model.NewItem("first")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddItem(model.NewItem("second")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(paths.StoreFile)
	if err != nil {
		t.Fatal(err)
	}
	var s model.Store
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("store is not valid JSON: %v", err)
	}
	if len(s.Items) != 2 {
		t.Errorf("items = %d", len(s.Items))
	}
	if _, err := os.Stat(paths.BackupFile); err != nil {
		t.Errorf("backup not written: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(paths.Dir, "*.tmp.*"))
	if len(matches) != 0 {
		t.Errorf("temp files left behind: %v", matches)
	}
}

func TestJournalUndoConsumesEntry(t *testing.T) {
	db, paths := openTemp(t)
	it, err := db.AddItem(model.NewItem("journaled"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Append(paths, Entry{TS: "now", Op: OpCreate, Label: "added", After: &it}); err != nil {
		t.Fatal(err)
	}

	last, err := LastUndoable(paths)
	if err != nil || last == nil {
		t.Fatalf("last = %v, %v", last, err)
	}
	if last.Op != OpCreate || last.After.ID != it.ID {
		t.Errorf("entry = %+v", last)
	}

	if err := Append(paths, Entry{TS: "now", Op: OpUndo, UndoSeq: last.Seq}); err != nil {
		t.Fatal(err)
	}
	next, err := LastUndoable(paths)
	if err != nil {
		t.Fatal(err)
	}
	if next != nil {
		t.Errorf("an undone entry must not be returned again: %+v", next)
	}
}

func TestParseLegacyExport(t *testing.T) {
	export := `{
	  "_exportVersion": 2,
	  "profile": {"mode": "professional", "tier": "pro", "subscription": "pro", "priorityPreset": "Easy First", "availableHoursPerDay": 8},
	  "theme": "dark",
	  "assignmentIndex": ["a1"],
	  "assignments": {
	    "a1": {"id":"a1","title":"Essay","type":"Essay","status":"active","difficulty":0,"estimatedHours":0,"priorityScore":0,"deadline":"2026-11-02","checklist":["Read paper", {"id":"c2","text":"Draft","done":true}],"boost":{"active":true,"reason":"exam","boostedPriorityScore":90}}
	  },
	  "taskIndex": ["t1","a1"],
	  "tasks": {
	    "t1": {"id":"t1","title":"Task","status":"in_progress","impactScore":7,"estimatedHours":2,"actualHours":3,"projectId":"p1"},
	    "a1": {"id":"a1","title":"Duplicate id task","status":"todo"}
	  },
	  "projectIndex": ["p1"],
	  "projects": {
	    "p1": {"id":"p1","title":"Client","clientContext":"build it","status":"active","visibility":"shared"}
	  }
	}`

	res, err := ParseLegacy([]byte(export))
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Warnings) == 0 {
		t.Error("expected a duplicate-id warning")
	}
	if len(res.Store.Items) != 3 {
		t.Fatalf("items = %d, want 3", len(res.Store.Items))
	}

	var essay model.Item
	for _, it := range res.Store.Items {
		if it.Title == "Essay" {
			essay = it
		}
	}
	if essay.Status != model.StatusTodo {
		t.Errorf("active should map to todo: %q", essay.Status)
	}
	if essay.Difficulty != 1 || essay.EstimatedHours != 0 || essay.PriorityScore != 0 {
		t.Errorf("clamps not applied: %+v", essay)
	}
	if len(essay.Checklist) != 2 || !essay.Checklist[1].Done || essay.Checklist[1].ID != "c2" {
		t.Errorf("checklist = %+v", essay.Checklist)
	}
	if !essay.Boost.Active || essay.Boost.BoostedPriorityScore == nil || *essay.Boost.BoostedPriorityScore != 90 {
		t.Errorf("boost = %+v", essay.Boost)
	}

	if res.Store.Profile.PriorityPreset != model.PresetEasiestFirst {
		t.Errorf("preset = %q", res.Store.Profile.PriorityPreset)
	}
	if res.Store.Profile.AvailableHoursPerDay != 8 {
		t.Errorf("hours = %d", res.Store.Profile.AvailableHoursPerDay)
	}
	if res.Store.Profile.Theme != model.ThemeDark {
		t.Errorf("theme = %q", res.Store.Profile.Theme)
	}

	if len(res.Store.Projects) != 1 || res.Store.Projects[0].Brief != "build it" {
		t.Errorf("projects = %+v", res.Store.Projects)
	}
}

func TestDetectDocumentKinds(t *testing.T) {
	if !IsLegacyExport([]byte(`{"_exportVersion":2}`)) {
		t.Error("legacy not detected")
	}
	if !IsNativeStore([]byte(`{"schemaVersion":1}`)) {
		t.Error("native not detected")
	}
	if IsLegacyExport([]byte(`{"items":[]}`)) {
		t.Error("false positive")
	}
}

func TestConcurrentMutationsDoNotLoseUpdates(t *testing.T) {
	db1, paths := openTemp(t)
	it, err := db1.AddItem(model.NewItem("base"))
	if err != nil {
		t.Fatal(err)
	}
	db2, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, err := db1.MutateItem(it.ID, func(cur *model.Item) error {
			cur.Title = "from one"
			return nil
		})
		errs <- err
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err := db2.MutateItem(it.ID, func(cur *model.Item) error {
			cur.Notes = "from two"
			return nil
		})
		errs <- err
	}()
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	reopened, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reopened.ItemByID(it.ID)
	if !ok {
		t.Fatal("item disappeared")
	}
	if got.Title != "from one" || got.Notes != "from two" {
		t.Errorf("lost an update: %+v", got)
	}
}

func TestCorruptRecoveryPreservesBackup(t *testing.T) {
	paths, err := ResolvePaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	backup := model.Store{
		SchemaVersion: model.SchemaVersion,
		Profile:       model.NormalizeProfile(model.Profile{}),
		Items:         []model.Item{model.NormalizeItem(model.Item{ID: "abc", Title: "from backup"})},
		Projects:      []model.Project{},
	}
	good, _ := json.Marshal(backup)
	if err := os.WriteFile(paths.BackupFile, good, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.StoreFile, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddItem(model.NewItem("after recovery")); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(paths.BackupFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(good) {
		t.Error("the good backup was overwritten during recovery")
	}
	matches, _ := filepath.Glob(paths.StoreFile + ".corrupt-*")
	if len(matches) != 1 {
		t.Errorf("corrupt store was not quarantined: %v", matches)
	}
}

func TestJournalHandlesLongEntry(t *testing.T) {
	_, paths := openTemp(t)
	it := model.NewItem("huge")
	it.RawContent = strings.Repeat("x", 5<<20)
	if err := Append(paths, Entry{TS: "now", Op: OpDelete, Before: &it}); err != nil {
		t.Fatal(err)
	}
	entries, err := ReadJournal(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Before == nil || len(entries[0].Before.RawContent) != len(it.RawContent) {
		t.Errorf("long journal entry was truncated")
	}
}

func TestJournalAppendReturnsReadError(t *testing.T) {
	_, paths := openTemp(t)
	if err := os.MkdirAll(paths.JournalFile, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Append(paths, Entry{TS: "now", Op: OpCreate}); err == nil {
		t.Fatal("expected a read error, not a silently truncated journal")
	}
}

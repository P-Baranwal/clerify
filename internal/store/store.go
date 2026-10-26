package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/P-Baranwal/clerify/internal/model"
)

var (
	// ErrNotFound is returned when a reference matches no record.
	ErrNotFound = errors.New("not found")
	// ErrLockTimeout is returned when another process holds the store lock.
	ErrLockTimeout = errors.New("could not acquire the store lock; another clerify process may be running")
)

// AmbiguousError is returned when an id prefix matches more than one record.
type AmbiguousError struct {
	Ref        string
	Candidates []string
}

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("ambiguous id %q matches %d items: %s", e.Ref, len(e.Candidates), strings.Join(e.Candidates, ", "))
}

// Paths holds every file location used by the CLI.
type Paths struct {
	Dir         string
	StoreFile   string
	ConfigFile  string
	JournalFile string
	LockFile    string
	BackupFile  string
}

// ResolvePaths resolves the data directory: explicit flag, then CLERIFY_HOME,
// then the platform config directory.
func ResolvePaths(explicit string) (Paths, error) {
	dir := strings.TrimSpace(explicit)
	if dir == "" {
		dir = strings.TrimSpace(os.Getenv("CLERIFY_HOME"))
	}
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve config directory: %w", err)
		}
		dir = filepath.Join(base, "clerify")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Paths{}, err
	}
	return Paths{
		Dir:         abs,
		StoreFile:   filepath.Join(abs, "store.json"),
		ConfigFile:  filepath.Join(abs, "config.json"),
		JournalFile: filepath.Join(abs, "journal.jsonl"),
		LockFile:    filepath.Join(abs, "store.lock"),
		BackupFile:  filepath.Join(abs, "store.json.bak"),
	}, nil
}

// DB is an open store. Mutating helpers persist immediately.
type DB struct {
	Paths     Paths
	Data      *model.Store
	Recovered bool
	Warnings  []string

	// Now is injectable so tests can control timestamps.
	Now func() time.Time
}

// Open loads the store, creating it if absent. A corrupt store falls back to
// the last known-good backup rather than silently starting empty.
func Open(p Paths) (*DB, error) {
	db := &DB{Paths: p, Now: time.Now}
	if err := os.MkdirAll(p.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	s := model.Store{}
	data, err := os.ReadFile(p.StoreFile)
	switch {
	case err == nil:
		if jerr := json.Unmarshal(data, &s); jerr != nil {
			backup, berr := os.ReadFile(p.BackupFile)
			if berr != nil {
				return nil, fmt.Errorf("store is corrupt (%v) and no backup is available", jerr)
			}
			if jerr2 := json.Unmarshal(backup, &s); jerr2 != nil {
				return nil, fmt.Errorf("store and backup are both corrupt: %v / %v", jerr, jerr2)
			}
			db.Recovered = true
		}
	case os.IsNotExist(err):
		s = newStore(time.Now())
	default:
		return nil, fmt.Errorf("read store: %w", err)
	}

	if s.SchemaVersion > model.SchemaVersion {
		return nil, fmt.Errorf("store schema version %d is newer than this binary supports (%d)", s.SchemaVersion, model.SchemaVersion)
	}
	migrate(&s)
	sanitize(&s)

	if db.Recovered {
		db.Warnings = append(db.Warnings, "store.json was unreadable; recovered from store.json.bak")
	}
	db.Data = &s
	return db, nil
}

// Close is a no-op placeholder kept so callers can defer it.
func (db *DB) Close() error { return nil }

// Save atomically rewrites store.json after copying the previous contents to
// store.json.bak.
func (db *DB) Save() error {
	if db.Data == nil {
		return errors.New("store is not open")
	}
	db.Data.SchemaVersion = model.SchemaVersion
	sanitize(db.Data)

	data, err := json.MarshalIndent(db.Data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode store: %w", err)
	}
	data = append(data, '\n')

	release, err := acquireLock(db.Paths.LockFile, 5*time.Second)
	if err != nil {
		return err
	}
	defer release()

	if err := backupCurrent(db.Paths.StoreFile, db.Paths.BackupFile); err != nil {
		return err
	}
	if err := writeFileAtomic(db.Paths.StoreFile, data, 0o644); err != nil {
		return fmt.Errorf("write store: %w", err)
	}
	return nil
}

// ── Items ────────────────────────────────────────────────────────────────────

// Items returns the live item slice.
func (db *DB) Items() []model.Item { return db.Data.Items }

// ItemByID returns a copy of the item with the exact id.
func (db *DB) ItemByID(id string) (model.Item, bool) {
	for _, it := range db.Data.Items {
		if it.ID == id {
			return it.Clone(), true
		}
	}
	return model.Item{}, false
}

// FindItem resolves a full id or a unique id prefix.
func (db *DB) FindItem(ref string) (model.Item, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return model.Item{}, ErrNotFound
	}
	if it, ok := db.ItemByID(ref); ok {
		return it, nil
	}
	lower := strings.ToLower(ref)
	candidates := make([]model.Item, 0, 1)
	for _, it := range db.Data.Items {
		if strings.HasPrefix(strings.ToLower(it.ID), lower) {
			candidates = append(candidates, it.Clone())
		}
	}
	switch len(candidates) {
	case 0:
		return model.Item{}, ErrNotFound
	case 1:
		return candidates[0], nil
	default:
		ids := make([]string, 0, len(candidates))
		for _, it := range candidates {
			ids = append(ids, it.ID)
		}
		return model.Item{}, &AmbiguousError{Ref: ref, Candidates: ids}
	}
}

// AddItem normalizes, stamps, appends and saves.
func (db *DB) AddItem(it model.Item) (model.Item, error) {
	it = model.NormalizeItem(it)
	if it.ID == "" {
		it.ID = model.NewID()
	}
	if _, exists := db.ItemByID(it.ID); exists {
		return it, fmt.Errorf("item %s already exists", it.ID)
	}
	now := model.NowRFC3339(db.now())
	if it.CreatedAt == "" {
		it.CreatedAt = now
	}
	if it.UpdatedAt == "" {
		it.UpdatedAt = now
	}
	db.Data.Items = append(db.Data.Items, it)
	return it, db.Save()
}

// UpdateItem replaces an existing item and saves.
func (db *DB) UpdateItem(it model.Item) (model.Item, error) {
	if strings.TrimSpace(it.ID) == "" {
		return it, errors.New("item id is required")
	}
	idx := db.itemIndex(it.ID)
	if idx < 0 {
		return it, ErrNotFound
	}
	current := db.Data.Items[idx]
	it = model.NormalizeItem(it)
	if it.CreatedAt == "" {
		it.CreatedAt = current.CreatedAt
	}
	it.UpdatedAt = model.NowRFC3339(db.now())
	db.Data.Items[idx] = it
	return it, db.Save()
}

// DeleteItem removes an item and returns the removed record.
func (db *DB) DeleteItem(id string) (model.Item, error) {
	idx := db.itemIndex(id)
	if idx < 0 {
		return model.Item{}, ErrNotFound
	}
	removed := db.Data.Items[idx].Clone()
	db.Data.Items = append(db.Data.Items[:idx], db.Data.Items[idx+1:]...)
	return removed, db.Save()
}

// RestoreItem writes a snapshot back exactly as stored, without touching
// timestamps. Used by undo.
func (db *DB) RestoreItem(it model.Item) error {
	it = model.NormalizeItem(it)
	if it.ID == "" {
		return errors.New("item id is required")
	}
	if idx := db.itemIndex(it.ID); idx >= 0 {
		db.Data.Items[idx] = it
	} else {
		db.Data.Items = append(db.Data.Items, it)
	}
	return db.Save()
}

// ReplaceItems swaps the whole collection (used by import --replace).
func (db *DB) ReplaceItems(items []model.Item) error {
	out := make([]model.Item, 0, len(items))
	for _, it := range items {
		it = model.NormalizeItem(it)
		if it.ID == "" {
			it.ID = model.NewID()
		}
		out = append(out, it)
	}
	db.Data.Items = out
	return db.Save()
}

// UpsertItems merges by id (used by import --merge).
func (db *DB) UpsertItems(items []model.Item) (added, replaced int, err error) {
	for _, it := range items {
		it = model.NormalizeItem(it)
		if it.ID == "" {
			it.ID = model.NewID()
		}
		if idx := db.itemIndex(it.ID); idx >= 0 {
			db.Data.Items[idx] = it
			replaced++
			continue
		}
		db.Data.Items = append(db.Data.Items, it)
		added++
	}
	return added, replaced, db.Save()
}

func (db *DB) itemIndex(id string) int {
	for i := range db.Data.Items {
		if db.Data.Items[i].ID == id {
			return i
		}
	}
	return -1
}

// ── Projects ─────────────────────────────────────────────────────────────────

// Projects returns the live project slice.
func (db *DB) Projects() []model.Project { return db.Data.Projects }

// ProjectByID returns a project copy.
func (db *DB) ProjectByID(id string) (model.Project, bool) {
	for _, p := range db.Data.Projects {
		if p.ID == id {
			return p, true
		}
	}
	return model.Project{}, false
}

// FindProject resolves a full id, unique id prefix, or exact (case-insensitive) title.
func (db *DB) FindProject(ref string) (model.Project, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return model.Project{}, ErrNotFound
	}
	if p, ok := db.ProjectByID(ref); ok {
		return p, nil
	}
	lower := strings.ToLower(ref)
	var byTitle []model.Project
	var byPrefix []model.Project
	for _, p := range db.Data.Projects {
		if strings.EqualFold(p.Title, ref) {
			byTitle = append(byTitle, p)
		}
		if strings.HasPrefix(strings.ToLower(p.ID), lower) {
			byPrefix = append(byPrefix, p)
		}
	}
	if len(byTitle) == 1 {
		return byTitle[0], nil
	}
	switch len(byPrefix) {
	case 0:
		return model.Project{}, ErrNotFound
	case 1:
		return byPrefix[0], nil
	default:
		ids := make([]string, 0, len(byPrefix))
		for _, p := range byPrefix {
			ids = append(ids, p.ID)
		}
		return model.Project{}, &AmbiguousError{Ref: ref, Candidates: ids}
	}
}

// AddProject stamps and saves a new project.
func (db *DB) AddProject(p model.Project) (model.Project, error) {
	p = model.NormalizeProject(p)
	if p.ID == "" {
		p.ID = model.NewID()
	}
	if _, exists := db.ProjectByID(p.ID); exists {
		return p, fmt.Errorf("project %s already exists", p.ID)
	}
	now := model.NowRFC3339(db.now())
	if p.CreatedAt == "" {
		p.CreatedAt = now
	}
	if p.UpdatedAt == "" {
		p.UpdatedAt = now
	}
	db.Data.Projects = append(db.Data.Projects, p)
	return p, db.Save()
}

// UpdateProject saves changes to an existing project.
func (db *DB) UpdateProject(p model.Project) (model.Project, error) {
	idx := -1
	for i := range db.Data.Projects {
		if db.Data.Projects[i].ID == p.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return p, ErrNotFound
	}
	p = model.NormalizeProject(p)
	p.UpdatedAt = model.NowRFC3339(db.now())
	db.Data.Projects[idx] = p
	return p, db.Save()
}

// DeleteProject removes a project and detaches its items (they become inbox items).
func (db *DB) DeleteProject(id string) (model.Project, error) {
	idx := -1
	for i := range db.Data.Projects {
		if db.Data.Projects[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return model.Project{}, ErrNotFound
	}
	removed := db.Data.Projects[idx]
	db.Data.Projects = append(db.Data.Projects[:idx], db.Data.Projects[idx+1:]...)
	for i := range db.Data.Items {
		if db.Data.Items[i].ProjectID != nil && *db.Data.Items[i].ProjectID == id {
			db.Data.Items[i].ProjectID = nil
		}
	}
	if db.Data.Profile.DefaultProjectID != nil && *db.Data.Profile.DefaultProjectID == id {
		db.Data.Profile.DefaultProjectID = nil
	}
	return removed, db.Save()
}

// RestoreProject writes a project snapshot back without touching timestamps.
func (db *DB) RestoreProject(p model.Project) error {
	p = model.NormalizeProject(p)
	if p.ID == "" {
		return errors.New("project id is required")
	}
	for i := range db.Data.Projects {
		if db.Data.Projects[i].ID == p.ID {
			db.Data.Projects[i] = p
			return db.Save()
		}
	}
	db.Data.Projects = append(db.Data.Projects, p)
	return db.Save()
}

// ReplaceProjects swaps the whole collection.
func (db *DB) ReplaceProjects(projects []model.Project) error {
	out := make([]model.Project, 0, len(projects))
	for _, p := range projects {
		p = model.NormalizeProject(p)
		if p.ID == "" {
			p.ID = model.NewID()
		}
		out = append(out, p)
	}
	db.Data.Projects = out
	return db.Save()
}

// ProjectTitle resolves a project id for display.
func (db *DB) ProjectTitle(id string) string {
	if p, ok := db.ProjectByID(id); ok {
		return p.Title
	}
	return "Unknown"
}

// ── Profile & plan ───────────────────────────────────────────────────────────

// SetProfile normalizes, stores and saves the profile.
func (db *DB) SetProfile(p model.Profile) (model.Profile, error) {
	p = model.NormalizeProfile(p)
	db.Data.Profile = p
	return p, db.Save()
}

// SetPlan caches the generated weekly plan.
func (db *DB) SetPlan(p *model.Plan) error {
	db.Data.Plan = p
	return db.Save()
}

func (db *DB) now() time.Time {
	if db.Now != nil {
		return db.Now()
	}
	return time.Now()
}

func newStore(now time.Time) model.Store {
	ts := model.NowRFC3339(now)
	return model.Store{
		SchemaVersion: model.SchemaVersion,
		Profile: model.NormalizeProfile(model.Profile{
			PriorityPreset:       model.PresetBalanced,
			AvailableHoursPerDay: model.DefaultAvailableHours,
			Theme:                model.ThemeSystem,
		}),
		Items:    []model.Item{},
		Projects: []model.Project{},
		Meta: model.Meta{
			DeviceID:  model.NewID(),
			CreatedAt: ts,
		},
	}
}

func sanitize(s *model.Store) {
	s.Profile = model.NormalizeProfile(s.Profile)
	if s.Items == nil {
		s.Items = []model.Item{}
	}
	if s.Projects == nil {
		s.Projects = []model.Project{}
	}
	now := model.NowRFC3339(time.Now())
	for i := range s.Items {
		s.Items[i] = model.NormalizeItem(s.Items[i])
		if s.Items[i].ID == "" {
			s.Items[i].ID = model.NewID()
		}
		if s.Items[i].CreatedAt == "" {
			s.Items[i].CreatedAt = now
		}
		if s.Items[i].UpdatedAt == "" {
			s.Items[i].UpdatedAt = s.Items[i].CreatedAt
		}
	}
	for i := range s.Projects {
		s.Projects[i] = model.NormalizeProject(s.Projects[i])
		if s.Projects[i].ID == "" {
			s.Projects[i].ID = model.NewID()
		}
		if s.Projects[i].CreatedAt == "" {
			s.Projects[i].CreatedAt = now
		}
		if s.Projects[i].UpdatedAt == "" {
			s.Projects[i].UpdatedAt = s.Projects[i].CreatedAt
		}
	}
	if s.Meta.CreatedAt == "" {
		s.Meta.CreatedAt = now
	}
	if s.Meta.DeviceID == "" {
		s.Meta.DeviceID = model.NewID()
	}
}

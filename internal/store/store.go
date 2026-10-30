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
	// ErrUnreadable is returned when store.json cannot be read or recovered.
	ErrUnreadable = errors.New("store is unreadable")
)

// AmbiguousError is returned when an id prefix matches more than one record.
type AmbiguousError struct {
	Ref        string
	Candidates []string
	// Labels are short, human-readable forms of the candidates. They are used
	// in the error instead of the full UUIDs.
	Labels []string
}

func (e *AmbiguousError) Error() string {
	items := e.Candidates
	if len(e.Labels) > 0 {
		items = e.Labels
	}
	return fmt.Sprintf("ambiguous id %q matches %d items: %s", e.Ref, len(items), strings.Join(items, ", "))
}

// shortRef renders "1a2b3c (Title)" for ambiguity messages.
func shortRef(id, title string) string {
	short := id
	if len(short) > 6 {
		short = short[:6]
	}
	if strings.TrimSpace(title) == "" {
		return short
	}
	return short + " (" + title + ")"
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

// DB is an open store. Reads use Data; writes go through Mutate so they reload
// the document under the lock and never overwrite a concurrent change.
type DB struct {
	Paths     Paths
	Data      *model.Store
	Recovered bool
	Warnings  []string

	// Now is injectable so tests can control timestamps.
	Now func() time.Time

	skipBackupOnce bool
}

// Tx is one locked read-modify-write transaction.
type Tx struct {
	db      *DB
	store   *model.Store
	entries []Entry
	now     time.Time
}

// Store returns the live document inside the transaction.
func (tx *Tx) Store() *model.Store { return tx.store }

// Now returns the transaction clock.
func (tx *Tx) Now() time.Time { return tx.now }

// Item returns a live pointer to the item with the exact id.
func (tx *Tx) Item(id string) (*model.Item, bool) {
	for i := range tx.store.Items {
		if tx.store.Items[i].ID == id {
			return &tx.store.Items[i], true
		}
	}
	return nil, false
}

// Project returns a live pointer to the project with the exact id.
func (tx *Tx) Project(id string) (*model.Project, bool) {
	for i := range tx.store.Projects {
		if tx.store.Projects[i].ID == id {
			return &tx.store.Projects[i], true
		}
	}
	return nil, false
}

// Journal records a reversible entry in the same transaction as the write.
func (tx *Tx) Journal(e Entry) { tx.entries = append(tx.entries, e) }

// Open loads the store, creating it if absent. A corrupt store falls back to
// the last known-good backup rather than silently starting empty.
func Open(p Paths) (*DB, error) {
	db := &DB{Paths: p, Now: time.Now}
	if err := os.MkdirAll(p.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	now := db.now()
	s, recovered, warnings, err := loadStore(p, now)
	if err != nil {
		return nil, err
	}
	db.Warnings = append(db.Warnings, warnings...)

	if recovered {
		db.Recovered = true
		if corrupt, ok := quarantineCorruptStore(p); ok {
			db.Warnings = append(db.Warnings, "store.json was unreadable; recovered from store.json.bak (corrupt file saved as "+corrupt+")")
		} else {
			db.Warnings = append(db.Warnings, "store.json was unreadable; recovered from store.json.bak")
		}
		// Never let the first post-recovery save overwrite the known-good
		// backup, even if the corrupt file was successfully quarantined.
		db.skipBackupOnce = true
		// Persist the recovered document immediately. Otherwise the next
		// Mutate would see a missing store.json (we just quarantined it) and
		// create an empty store, losing the recovered data.
		if err := writeStoreFile(p, s); err != nil {
			return nil, err
		}
	}

	db.Data = s
	return db, nil
}

// Close is a no-op placeholder kept so callers can defer it.
func (db *DB) Close() error { return nil }

// Mutate acquires the store lock, reloads the document from disk, applies fn,
// writes the result, and appends the transaction's journal entries. Because
// the reload and write share one lock, concurrent commands can no longer lose
// each other's changes.
func (db *DB) Mutate(fn func(*Tx) error) error {
	release, err := acquireLock(db.Paths.LockFile, 5*time.Second)
	if err != nil {
		return err
	}
	defer release()

	now := db.now()
	s, recovered, warnings, err := loadStore(db.Paths, now)
	if err != nil {
		return err
	}
	if recovered {
		db.Recovered = true
		if corrupt, ok := quarantineCorruptStore(db.Paths); ok {
			warnings = append(warnings, "store.json was unreadable; recovered from store.json.bak (corrupt file saved as "+corrupt+")")
		}
		db.skipBackupOnce = true
	}

	tx := &Tx{db: db, store: s, now: now}
	if err := fn(tx); err != nil {
		return err
	}

	tx.store.SchemaVersion = model.SchemaVersion
	sanitizeAt(tx.store, now)

	if !db.skipBackupOnce {
		if err := backupCurrent(db.Paths.StoreFile, db.Paths.BackupFile); err != nil {
			return err
		}
	}
	if err := writeStoreFile(db.Paths, tx.store); err != nil {
		return err
	}
	if len(tx.entries) > 0 {
		if err := appendEntriesLocked(db.Paths, tx.entries); err != nil {
			return err
		}
	}

	db.Data = tx.store
	db.Warnings = append(db.Warnings, warnings...)
	db.skipBackupOnce = false
	return nil
}

func writeStoreFile(p Paths, s *model.Store) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode store: %w", err)
	}
	data = append(data, '\n')
	if err := writeFileAtomic(p.StoreFile, data, 0o644); err != nil {
		return fmt.Errorf("write store: %w", err)
	}
	return nil
}

// MutateItem is the common single-item transaction. Callers that need a
// journal entry should use Mutate directly so the snapshot and entry share the
// same lock.
func (db *DB) MutateItem(id string, fn func(*model.Item) error) (model.Item, error) {
	var out model.Item
	err := db.Mutate(func(tx *Tx) error {
		it, ok := tx.Item(id)
		if !ok {
			return ErrNotFound
		}
		if err := fn(it); err != nil {
			return err
		}
		*it = model.NormalizeItem(*it)
		it.UpdatedAt = model.NowRFC3339(tx.now)
		out = it.Clone()
		return nil
	})
	return out, err
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
		labels := make([]string, 0, len(candidates))
		for _, it := range candidates {
			ids = append(ids, it.ID)
			labels = append(labels, shortRef(it.ID, it.Title))
		}
		return model.Item{}, &AmbiguousError{Ref: ref, Candidates: ids, Labels: labels}
	}
}

// AddItem normalizes, stamps, appends and saves.
func (db *DB) AddItem(it model.Item) (model.Item, error) {
	var saved model.Item
	err := db.Mutate(func(tx *Tx) error {
		it = model.NormalizeItem(it)
		if it.ID == "" {
			it.ID = model.NewID()
		}
		if _, exists := tx.Item(it.ID); exists {
			return fmt.Errorf("item %s already exists", it.ID)
		}
		now := model.NowRFC3339(tx.now)
		if it.CreatedAt == "" {
			it.CreatedAt = now
		}
		if it.UpdatedAt == "" {
			it.UpdatedAt = now
		}
		if it.Status == model.StatusDone && it.CompletedAt == nil {
			it.CompletedAt = &now
		}
		tx.store.Items = append(tx.store.Items, it)
		saved = it.Clone()
		return nil
	})
	return saved, err
}

// UpdateItem replaces an existing item and saves.
func (db *DB) UpdateItem(it model.Item) (model.Item, error) {
	if strings.TrimSpace(it.ID) == "" {
		return it, errors.New("item id is required")
	}
	var saved model.Item
	err := db.Mutate(func(tx *Tx) error {
		current, ok := tx.Item(it.ID)
		if !ok {
			return ErrNotFound
		}
		next := model.NormalizeItem(it)
		if next.CreatedAt == "" {
			next.CreatedAt = current.CreatedAt
		}
		next.UpdatedAt = model.NowRFC3339(tx.now)
		*current = next
		saved = current.Clone()
		return nil
	})
	return saved, err
}

// DeleteItem removes an item and returns the removed record.
func (db *DB) DeleteItem(id string) (model.Item, error) {
	var removed model.Item
	err := db.Mutate(func(tx *Tx) error {
		for i := range tx.store.Items {
			if tx.store.Items[i].ID == id {
				removed = tx.store.Items[i].Clone()
				tx.store.Items = append(tx.store.Items[:i], tx.store.Items[i+1:]...)
				return nil
			}
		}
		return ErrNotFound
	})
	return removed, err
}

// RestoreItem writes a snapshot back exactly as stored, without touching
// timestamps. Used by undo.
func (db *DB) RestoreItem(it model.Item) error {
	it = model.NormalizeItem(it)
	if it.ID == "" {
		return errors.New("item id is required")
	}
	return db.Mutate(func(tx *Tx) error {
		if current, ok := tx.Item(it.ID); ok {
			*current = it
			return nil
		}
		tx.store.Items = append(tx.store.Items, it)
		return nil
	})
}

// ReplaceItems swaps the whole collection (used by import --replace).
func (db *DB) ReplaceItems(items []model.Item) error {
	return db.Mutate(func(tx *Tx) error {
		out := make([]model.Item, 0, len(items))
		for _, it := range items {
			it = model.NormalizeItem(it)
			if it.ID == "" {
				it.ID = model.NewID()
			}
			out = append(out, it)
		}
		tx.store.Items = out
		return nil
	})
}

// UpsertItems merges by id. It is kept for callers that do not need the newer
// timestamp rule; import uses UpsertItemsIfNewer.
func (db *DB) UpsertItems(items []model.Item) (added, replaced int, err error) {
	added, replaced, _, err = db.UpsertItemsIfNewer(items)
	return added, replaced, err
}

// UpsertItemsIfNewer merges by id and refuses to overwrite a locally newer
// item, so importing an older backup cannot clobber recent edits.
func (db *DB) UpsertItemsIfNewer(items []model.Item) (added, replaced, skipped int, err error) {
	err = db.Mutate(func(tx *Tx) error {
		for _, it := range items {
			it = model.NormalizeItem(it)
			if it.ID == "" {
				it.ID = model.NewID()
			}
			idx := -1
			for i := range tx.store.Items {
				if tx.store.Items[i].ID == it.ID {
					idx = i
					break
				}
			}
			if idx < 0 {
				tx.store.Items = append(tx.store.Items, it)
				added++
				continue
			}
			if !incomingNewer(it.UpdatedAt, tx.store.Items[idx].UpdatedAt) {
				skipped++
				continue
			}
			tx.store.Items[idx] = it
			replaced++
		}
		return nil
	})
	return added, replaced, skipped, err
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
		labels := make([]string, 0, len(byPrefix))
		for _, p := range byPrefix {
			ids = append(ids, p.ID)
			labels = append(labels, shortRef(p.ID, p.Title))
		}
		return model.Project{}, &AmbiguousError{Ref: ref, Candidates: ids, Labels: labels}
	}
}

// AddProject stamps and saves a new project.
func (db *DB) AddProject(p model.Project) (model.Project, error) {
	var saved model.Project
	err := db.Mutate(func(tx *Tx) error {
		p = model.NormalizeProject(p)
		if p.ID == "" {
			p.ID = model.NewID()
		}
		if _, exists := tx.Project(p.ID); exists {
			return fmt.Errorf("project %s already exists", p.ID)
		}
		now := model.NowRFC3339(tx.now)
		if p.CreatedAt == "" {
			p.CreatedAt = now
		}
		if p.UpdatedAt == "" {
			p.UpdatedAt = now
		}
		tx.store.Projects = append(tx.store.Projects, p)
		saved = p
		return nil
	})
	return saved, err
}

// UpdateProject saves changes to an existing project.
func (db *DB) UpdateProject(p model.Project) (model.Project, error) {
	var saved model.Project
	err := db.Mutate(func(tx *Tx) error {
		current, ok := tx.Project(p.ID)
		if !ok {
			return ErrNotFound
		}
		next := model.NormalizeProject(p)
		next.CreatedAt = current.CreatedAt
		next.UpdatedAt = model.NowRFC3339(tx.now)
		*current = next
		saved = next
		return nil
	})
	return saved, err
}

// DeleteProject removes a project and detaches its items (they become inbox items).
func (db *DB) DeleteProject(id string) (model.Project, error) {
	var removed model.Project
	err := db.Mutate(func(tx *Tx) error {
		idx := -1
		for i := range tx.store.Projects {
			if tx.store.Projects[i].ID == id {
				idx = i
				break
			}
		}
		if idx < 0 {
			return ErrNotFound
		}
		removed = tx.store.Projects[idx]
		tx.store.Projects = append(tx.store.Projects[:idx], tx.store.Projects[idx+1:]...)
		for i := range tx.store.Items {
			if tx.store.Items[i].ProjectID != nil && *tx.store.Items[i].ProjectID == id {
				tx.store.Items[i].ProjectID = nil
			}
		}
		if tx.store.Profile.DefaultProjectID != nil && *tx.store.Profile.DefaultProjectID == id {
			tx.store.Profile.DefaultProjectID = nil
		}
		return nil
	})
	return removed, err
}

// RestoreProject writes a project snapshot back without touching timestamps.
func (db *DB) RestoreProject(p model.Project) error {
	p = model.NormalizeProject(p)
	if p.ID == "" {
		return errors.New("project id is required")
	}
	return db.Mutate(func(tx *Tx) error {
		if current, ok := tx.Project(p.ID); ok {
			*current = p
			return nil
		}
		tx.store.Projects = append(tx.store.Projects, p)
		return nil
	})
}

// ReplaceProjects swaps the whole collection.
func (db *DB) ReplaceProjects(projects []model.Project) error {
	return db.Mutate(func(tx *Tx) error {
		out := make([]model.Project, 0, len(projects))
		for _, p := range projects {
			p = model.NormalizeProject(p)
			if p.ID == "" {
				p.ID = model.NewID()
			}
			out = append(out, p)
		}
		tx.store.Projects = out
		return nil
	})
}

// UpsertProjectsIfNewer merges projects by id using the same timestamp rule as
// items.
func (db *DB) UpsertProjectsIfNewer(projects []model.Project) (added, replaced, skipped int, err error) {
	err = db.Mutate(func(tx *Tx) error {
		for _, p := range projects {
			p = model.NormalizeProject(p)
			if p.ID == "" {
				p.ID = model.NewID()
			}
			idx := -1
			for i := range tx.store.Projects {
				if tx.store.Projects[i].ID == p.ID {
					idx = i
					break
				}
			}
			if idx < 0 {
				tx.store.Projects = append(tx.store.Projects, p)
				added++
				continue
			}
			if !incomingNewer(p.UpdatedAt, tx.store.Projects[idx].UpdatedAt) {
				skipped++
				continue
			}
			p.CreatedAt = tx.store.Projects[idx].CreatedAt
			p.UpdatedAt = model.NowRFC3339(tx.now)
			tx.store.Projects[idx] = p
			replaced++
		}
		return nil
	})
	return added, replaced, skipped, err
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
	saved := model.NormalizeProfile(p)
	err := db.Mutate(func(tx *Tx) error {
		tx.store.Profile = saved
		return nil
	})
	return saved, err
}

// SetPlan caches the generated weekly plan.
func (db *DB) SetPlan(p *model.Plan) error {
	return db.Mutate(func(tx *Tx) error {
		tx.store.Plan = p
		return nil
	})
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

func loadStore(p Paths, now time.Time) (*model.Store, bool, []string, error) {
	s := model.Store{}
	recovered := false

	data, err := os.ReadFile(p.StoreFile)
	switch {
	case err == nil:
		if jerr := json.Unmarshal(data, &s); jerr != nil {
			backup, berr := os.ReadFile(p.BackupFile)
			if berr != nil {
				return nil, false, nil, fmt.Errorf("%w: store is corrupt (%v) and no backup is available", ErrUnreadable, jerr)
			}
			if jerr2 := json.Unmarshal(backup, &s); jerr2 != nil {
				return nil, false, nil, fmt.Errorf("%w: store and backup are both corrupt: %v / %v", ErrUnreadable, jerr, jerr2)
			}
			recovered = true
		}
	case os.IsNotExist(err):
		s = newStore(now)
	default:
		return nil, false, nil, fmt.Errorf("%w: read store: %v", ErrUnreadable, err)
	}

	if s.SchemaVersion > model.SchemaVersion {
		return nil, false, nil, fmt.Errorf("store schema version %d is newer than this binary supports (%d)", s.SchemaVersion, model.SchemaVersion)
	}
	migrate(&s)
	warnings := sanitizeAt(&s, now)
	return &s, recovered, warnings, nil
}

func quarantineCorruptStore(p Paths) (string, bool) {
	target := p.StoreFile + ".corrupt-" + time.Now().Format("20060102-150405")
	if err := os.Rename(p.StoreFile, target); err != nil {
		return "", false
	}
	return target, true
}

func sanitizeAt(s *model.Store, now time.Time) []string {
	if now.IsZero() {
		now = time.Now()
	}
	s.Profile = model.NormalizeProfile(s.Profile)
	if s.Items == nil {
		s.Items = []model.Item{}
	}
	if s.Projects == nil {
		s.Projects = []model.Project{}
	}
	ts := model.NowRFC3339(now)
	var warnings []string
	for i := range s.Items {
		s.Items[i] = model.NormalizeItem(s.Items[i])
		if s.Items[i].ID == "" {
			s.Items[i].ID = model.NewID()
		}
		if s.Items[i].CreatedAt == "" {
			s.Items[i].CreatedAt = ts
		}
		if s.Items[i].UpdatedAt == "" {
			s.Items[i].UpdatedAt = s.Items[i].CreatedAt
		}
		if s.Items[i].Status == model.StatusDone && s.Items[i].CompletedAt == nil {
			completed := s.Items[i].UpdatedAt
			s.Items[i].CompletedAt = &completed
		}
		if s.Items[i].Deadline != nil {
			if _, ok := model.ParseDeadline(*s.Items[i].Deadline); !ok {
				warnings = append(warnings, fmt.Sprintf("item %s has invalid deadline %q", s.Items[i].ID, *s.Items[i].Deadline))
			}
		}
	}
	for i := range s.Projects {
		s.Projects[i] = model.NormalizeProject(s.Projects[i])
		if s.Projects[i].ID == "" {
			s.Projects[i].ID = model.NewID()
		}
		if s.Projects[i].CreatedAt == "" {
			s.Projects[i].CreatedAt = ts
		}
		if s.Projects[i].UpdatedAt == "" {
			s.Projects[i].UpdatedAt = s.Projects[i].CreatedAt
		}
	}
	if s.Meta.CreatedAt == "" {
		s.Meta.CreatedAt = ts
	}
	if s.Meta.DeviceID == "" {
		s.Meta.DeviceID = model.NewID()
	}
	return warnings
}

func incomingNewer(incoming, local string) bool {
	in, inErr := time.Parse(time.RFC3339, strings.TrimSpace(incoming))
	lo, loErr := time.Parse(time.RFC3339, strings.TrimSpace(local))
	if inErr != nil {
		return false
	}
	if loErr != nil || local == "" {
		return true
	}
	return in.After(lo)
}

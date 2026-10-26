package store

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"

	"github.com/P-Baranwal/clerify/internal/model"
)

// MaxJournalEntries bounds the journal; older entries are dropped on append.
const MaxJournalEntries = 50

// Journal operation names.
const (
	OpCreate = "create"
	OpUpdate = "update"
	OpDelete = "delete"
	OpUndo   = "undo"
)

// Entry is one reversible mutation.
type Entry struct {
	TS            string         `json:"ts"`
	Op            string         `json:"op"`
	Label         string         `json:"label,omitempty"`
	Before        *model.Item    `json:"before,omitempty"`
	After         *model.Item    `json:"after,omitempty"`
	ProjectBefore *model.Project `json:"projectBefore,omitempty"`
	ProjectAfter  *model.Project `json:"projectAfter,omitempty"`
}

// Append adds an entry to the journal, pruning to MaxJournalEntries.
func Append(p Paths, e Entry) error {
	entries, _ := ReadJournal(p)
	entries = append(entries, e)
	if len(entries) > MaxJournalEntries {
		entries = entries[len(entries)-MaxJournalEntries:]
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	for _, entry := range entries {
		if err := enc.Encode(entry); err != nil {
			return err
		}
	}
	return writeFileAtomic(p.JournalFile, []byte(b.String()), 0o600)
}

// ReadJournal returns every journal entry in order.
func ReadJournal(p Paths) ([]Entry, error) {
	f, err := os.Open(p.JournalFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue // skip a torn line rather than losing the whole journal
		}
		entries = append(entries, e)
	}
	return entries, sc.Err()
}

// LastUndoable returns the most recent entry that actually changes state.
func LastUndoable(p Paths) (*Entry, error) {
	entries, err := ReadJournal(p)
	if err != nil {
		return nil, err
	}
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Op != OpUndo {
			return &entries[i], nil
		}
	}
	return nil, nil
}

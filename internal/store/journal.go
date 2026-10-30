package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/P-Baranwal/clerify/internal/model"
)

// MaxJournalEntries bounds the journal; older entries are dropped on append.
const MaxJournalEntries = 50

// Journal operation names.
const (
	OpCreate  = "create"
	OpUpdate  = "update"
	OpDelete  = "delete"
	OpUndo    = "undo"
	OpRescore = "rescore"
)

// ItemChange is one item mutation inside a batch journal entry.
type ItemChange struct {
	Before *model.Item `json:"before,omitempty"`
	After  *model.Item `json:"after,omitempty"`
}

// Entry is one reversible mutation.
type Entry struct {
	Seq                    int            `json:"seq,omitempty"`
	UndoSeq                int            `json:"undoSeq,omitempty"`
	TS                     string         `json:"ts"`
	Op                     string         `json:"op"`
	Label                  string         `json:"label,omitempty"`
	Before                 *model.Item    `json:"before,omitempty"`
	After                  *model.Item    `json:"after,omitempty"`
	ProjectBefore          *model.Project `json:"projectBefore,omitempty"`
	ProjectAfter           *model.Project `json:"projectAfter,omitempty"`
	DefaultProjectIDBefore *string        `json:"defaultProjectIdBefore,omitempty"`
	ItemsBefore            []model.Item   `json:"itemsBefore,omitempty"`
	Batch                  []ItemChange   `json:"batch,omitempty"`
}

// Append adds an entry to the journal, pruning to MaxJournalEntries.
func Append(p Paths, e Entry) error {
	release, err := acquireLock(p.LockFile, 5*time.Second)
	if err != nil {
		return err
	}
	defer release()
	return appendEntriesLocked(p, []Entry{e})
}

// appendEntriesLocked appends entries while the caller already holds the
// store lock, which keeps the store write and the journal in one critical
// section.
func appendEntriesLocked(p Paths, incoming []Entry) error {
	entries, err := ReadJournal(p)
	if err != nil {
		return err
	}
	next := 1
	for _, e := range entries {
		if e.Seq >= next {
			next = e.Seq + 1
		}
	}
	for i := range incoming {
		if incoming[i].Seq == 0 {
			incoming[i].Seq = next
			next++
		}
		entries = append(entries, prepareEntry(incoming[i]))
	}
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
	reader := bufio.NewReader(f)
	for {
		line, readErr := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line != "" {
			var e Entry
			if err := json.Unmarshal([]byte(line), &e); err == nil {
				if e.Seq == 0 {
					e.Seq = len(entries) + 1
				}
				entries = append(entries, e)
			}
			// Skip a torn line rather than losing the whole journal.
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return entries, readErr
		}
	}
	return entries, nil
}

// LastUndoable returns the most recent entry that actually changes state and
// has not already been undone.
func LastUndoable(p Paths) (*Entry, error) {
	entries, err := ReadJournal(p)
	if err != nil {
		return nil, err
	}
	consumed := make(map[int]bool)
	for _, e := range entries {
		if e.Op == OpUndo && e.UndoSeq != 0 {
			consumed[e.UndoSeq] = true
		}
	}
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Op == OpUndo || consumed[e.Seq] {
			continue
		}
		return &e, nil
	}
	return nil, nil
}

func prepareEntry(e Entry) Entry {
	// A create undo only needs the id, and update/batch undo does not need the
	// raw source text. Delete entries keep it so restoring a deleted item is
	// lossless.
	if e.Op == OpUpdate || e.Op == OpRescore || e.Op == OpCreate {
		e.Before = stripRawContent(e.Before)
		e.After = stripRawContent(e.After)
		for i := range e.Batch {
			e.Batch[i].Before = stripRawContent(e.Batch[i].Before)
			e.Batch[i].After = stripRawContent(e.Batch[i].After)
		}
	}
	return e
}

func stripRawContent(it *model.Item) *model.Item {
	if it == nil || it.RawContent == "" {
		return it
	}
	copy := it.Clone()
	copy.RawContent = ""
	return &copy
}

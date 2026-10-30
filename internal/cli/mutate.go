package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/P-Baranwal/clerify/internal/model"
	"github.com/P-Baranwal/clerify/internal/store"
)

type editFlags struct {
	title        string
	typ          string
	due          string
	clearDue     bool
	est          float64
	actual       float64
	impact       int
	clearImpact  bool
	priority     int
	note         string
	desc         string
	project      string
	clearProject bool
	status       string
	checkAdd     []string
	checkRemove  []string
	checkDone    []string
	checkUndone  []string
}

func (a *App) editCmd() *cobra.Command {
	var f editFlags
	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Change fields on an item",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			return a.runEdit(cmd, args[0], f)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.title, "title", "", "new title")
	fl.StringVar(&f.typ, "type", "", "new type")
	fl.StringVar(&f.due, "due", "", "new deadline YYYY-MM-DD")
	fl.BoolVar(&f.clearDue, "clear-due", false, "remove the deadline")
	fl.Float64Var(&f.est, "est", 0, "estimated hours")
	fl.Float64Var(&f.actual, "actual", 0, "actual hours")
	fl.IntVar(&f.impact, "impact", 0, "impact score 1-10")
	fl.BoolVar(&f.clearImpact, "clear-impact", false, "remove the impact score")
	fl.IntVar(&f.priority, "priority", -1, "priority score 0-100")
	fl.StringVar(&f.note, "note", "", "note")
	fl.StringVar(&f.desc, "desc", "", "description")
	fl.StringVar(&f.project, "project", "", "project id, prefix, or title")
	fl.BoolVar(&f.clearProject, "clear-project", false, "detach from any project")
	fl.StringVar(&f.status, "status", "", "status: todo|in_progress|blocked|done")
	fl.StringArrayVar(&f.checkAdd, "check-add", nil, "append checklist item (repeatable; commas are literal)")
	fl.StringArrayVar(&f.checkRemove, "check-rm", nil, "remove checklist item by text (repeatable; commas are literal)")
	fl.StringArrayVar(&f.checkDone, "check-done", nil, "mark checklist item done by text (repeatable; commas are literal)")
	fl.StringArrayVar(&f.checkUndone, "check-undone", nil, "mark checklist item not done by text (repeatable; commas are literal)")
	return cmd
}

func (a *App) runEdit(cmd *cobra.Command, ref string, f editFlags) error {
	it, err := a.requireItem(ref)
	if err != nil {
		return err
	}

	if a.flags.DryRun {
		draft := it.Clone()
		if err := a.applyEdit(cmd, &draft, f, a.now()); err != nil {
			return err
		}
		return a.printJSON(draft)
	}

	var saved model.Item
	err = a.DB.Mutate(func(tx *store.Tx) error {
		cur, ok := tx.Item(it.ID)
		if !ok {
			return store.ErrNotFound
		}
		before := cur.Clone()
		if err := a.applyEdit(cmd, cur, f, tx.Now()); err != nil {
			return err
		}
		*cur = model.NormalizeItem(*cur)
		cur.UpdatedAt = model.NowRFC3339(tx.Now())
		saved = cur.Clone()
		tx.Journal(store.Entry{
			TS:     model.NowRFC3339(tx.Now()),
			Op:     store.OpUpdate,
			Label:  "Edited \"" + saved.Title + "\"",
			Before: &before,
			After:  &saved,
		})
		return nil
	})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return notFound("item", it.ID, err)
		}
		return err
	}

	if a.flags.JSON {
		return a.printJSON(saved)
	}
	a.printf("updated %s  %s\n", shortID(saved.ID), saved.Title)
	return nil
}

func (a *App) applyEdit(cmd *cobra.Command, it *model.Item, f editFlags, now time.Time) error {
	if cmd.Flags().Changed("title") && strings.TrimSpace(f.title) != "" {
		it.Title = strings.TrimSpace(f.title)
	}
	if cmd.Flags().Changed("type") {
		typ, err := model.ParseType(f.typ)
		if err != nil {
			return fail(2, err)
		}
		it.Type = typ
	}
	if f.clearDue {
		it.Deadline = nil
	} else if cmd.Flags().Changed("due") {
		due, err := model.ParseDeadlineStrict(f.due)
		if err != nil {
			return fail(2, err)
		}
		if due == "" {
			it.Deadline = nil
		} else {
			it.Deadline = &due
		}
	}
	if cmd.Flags().Changed("est") {
		it.EstimatedHours = f.est
	}
	if cmd.Flags().Changed("actual") {
		it.ActualHours = f.actual
	}
	if f.clearImpact {
		it.ImpactScore = nil
	} else if cmd.Flags().Changed("impact") {
		v := f.impact
		it.ImpactScore = &v
	}
	if f.priority >= 0 {
		it.PriorityScore = f.priority
		if it.ImpactScore != nil {
			return fail(2, fmt.Errorf("--priority is ignored while --impact is set (ROI scoring owns priority)"))
		}
	}
	if cmd.Flags().Changed("note") {
		it.Notes = f.note
	}
	if cmd.Flags().Changed("desc") {
		it.Description = f.desc
	}
	if f.clearProject {
		it.ProjectID = nil
	} else if strings.TrimSpace(f.project) != "" {
		id, err := a.resolveProject(f.project)
		if err != nil {
			return notFound("project", f.project, err)
		}
		it.ProjectID = id
	}
	if cmd.Flags().Changed("status") {
		st, err := model.ParseStatus(f.status)
		if err != nil {
			return fail(2, err)
		}
		applyStatus(it, st, "", now)
	}

	if len(f.checkAdd) > 0 {
		it.Checklist = model.ReconcileChecklist(it.Checklist, append(checklistTexts(it.Checklist), f.checkAdd...))
	}
	for _, text := range f.checkRemove {
		it.Checklist = removeChecklistByText(it.Checklist, text)
	}
	for _, text := range f.checkDone {
		setChecklistDone(it.Checklist, text, true)
	}
	for _, text := range f.checkUndone {
		setChecklistDone(it.Checklist, text, false)
	}
	return nil
}

func (a *App) statusCmds() []*cobra.Command {
	return []*cobra.Command{
		a.statusLeaf("todo <id>", model.StatusTodo, false),
		a.statusLeaf("start <id>", model.StatusInProgress, false),
		a.statusLeaf("block <id>", model.StatusBlocked, true),
		a.statusLeaf("unblock <id>", model.StatusTodo, false),
		a.statusLeaf("done <id>", model.StatusDone, false),
	}
}

func (a *App) statusLeaf(use, status string, wantsNote bool) *cobra.Command {
	var (
		note   string
		actual float64
	)
	cmd := &cobra.Command{
		Use:   use,
		Short: "Set status to " + status,
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			it, err := a.requireItem(args[0])
			if err != nil {
				return err
			}
			changedActual := cmd.Flags().Changed("actual")

			var saved model.Item
			err = a.DB.Mutate(func(tx *store.Tx) error {
				cur, ok := tx.Item(it.ID)
				if !ok {
					return store.ErrNotFound
				}
				before := cur.Clone()
				applyStatus(cur, status, note, tx.Now())
				if status == model.StatusDone && changedActual {
					cur.ActualHours = actual
				}
				*cur = model.NormalizeItem(*cur)
				cur.UpdatedAt = model.NowRFC3339(tx.Now())
				saved = cur.Clone()
				tx.Journal(store.Entry{
					TS:     model.NowRFC3339(tx.Now()),
					Op:     store.OpUpdate,
					Label:  "Changed \"" + saved.Title + "\" to " + saved.Status,
					Before: &before,
					After:  &saved,
				})
				return nil
			})
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return notFound("item", it.ID, err)
				}
				return err
			}
			if a.flags.JSON {
				return a.printJSON(saved)
			}
			a.printf("%s  %s\n", shortID(saved.ID), saved.Status)
			return nil
		},
	}
	if wantsNote {
		cmd.Flags().StringVar(&note, "note", "", "why it is blocked (default \"Blocked\")")
	}
	if status == model.StatusDone {
		cmd.Flags().Float64Var(&actual, "actual", 0, "actual hours spent")
	}
	return cmd
}

func applyStatus(it *model.Item, status, note string, now time.Time) {
	wasDone := it.IsDone()
	it.Status = model.NormalizeStatus(status)
	if it.Status == model.StatusDone {
		if !wasDone || it.CompletedAt == nil {
			ts := model.NowRFC3339(now)
			it.CompletedAt = &ts
		}
	} else {
		it.CompletedAt = nil
	}
	if it.Status == model.StatusBlocked {
		if it.BlockerNote == nil || strings.TrimSpace(*it.BlockerNote) == "" {
			n := strings.TrimSpace(note)
			if n == "" {
				n = "Blocked"
			}
			it.BlockerNote = &n
		}
		return
	}
	it.BlockerNote = nil
}

func (a *App) rmCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "rm <id>...",
		Aliases: []string{"delete"},
		Short:   "Delete items (undoable)",
		Args:    minArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			if !a.confirm(fmt.Sprintf("Delete %d item(s)?", len(args))) {
				return fail(1, errors.New("aborted (pass --yes to skip confirmation)"))
			}

			ids := make([]string, 0, len(args))
			for _, ref := range args {
				it, err := a.requireItem(ref)
				if err != nil {
					return err
				}
				ids = append(ids, it.ID)
			}

			deleted := make([]model.Item, 0, len(ids))
			err := a.DB.Mutate(func(tx *store.Tx) error {
				for _, id := range ids {
					cur, ok := tx.Item(id)
					if !ok {
						return store.ErrNotFound
					}
					before := cur.Clone()
					removeItem(tx.Store(), id)
					deleted = append(deleted, before)
					tx.Journal(store.Entry{
						TS:     model.NowRFC3339(tx.Now()),
						Op:     store.OpDelete,
						Label:  "Deleted \"" + before.Title + "\"",
						Before: &before,
					})
				}
				return nil
			})
			if err != nil {
				return err
			}

			if a.flags.JSON {
				return a.printJSON(map[string]any{"deleted": deleted})
			}
			for _, it := range deleted {
				a.printf("deleted %s  %s\n", shortID(it.ID), it.Title)
			}
			return nil
		},
	}
}

func (a *App) undoCmd() *cobra.Command {
	var (
		list bool
		n    int
	)
	cmd := &cobra.Command{
		Use:   "undo",
		Short: "Reverse the most recent mutation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			return a.runUndo(list, n)
		},
	}
	cmd.Flags().BoolVar(&list, "list", false, "list recent journal entries")
	cmd.Flags().IntVar(&n, "n", 10, "how many entries to list")
	return cmd
}

func (a *App) runUndo(list bool, n int) error {
	entries, err := store.ReadJournal(a.Paths)
	if err != nil {
		return err
	}

	if list {
		if len(entries) == 0 {
			a.printf("Journal is empty.\n")
			return nil
		}
		start := 0
		if n > 0 && len(entries) > n {
			start = len(entries) - n
		}
		if a.flags.JSON {
			return a.printJSON(entries[start:])
		}
		for _, e := range entries[start:] {
			label := e.Label
			if label == "" {
				label = e.Op
			}
			a.printf("%s  %-7s %s\n", e.TS, e.Op, label)
		}
		return nil
	}

	var undone *store.Entry
	err = a.DB.Mutate(func(tx *store.Tx) error {
		entry, err := store.LastUndoable(a.Paths)
		if err != nil {
			return err
		}
		if entry == nil {
			return fail(1, errors.New("nothing to undo"))
		}
		if err := undoEntry(tx, entry); err != nil {
			return err
		}
		tx.Journal(store.Entry{
			TS:      model.NowRFC3339(tx.Now()),
			Op:      store.OpUndo,
			UndoSeq: entry.Seq,
			Label:   entry.Label,
		})
		undone = entry
		return nil
	})
	if err != nil {
		return err
	}

	label := undone.Label
	if label == "" {
		label = undone.Op
	}
	if a.flags.JSON {
		return a.printJSON(map[string]any{
			"undone": label,
			"op":     undone.Op,
			"batch":  len(undone.Batch),
		})
	}
	a.printf("undid: %s\n", label)
	return nil
}

func undoEntry(tx *store.Tx, e *store.Entry) error {
	if len(e.Batch) > 0 {
		for i := len(e.Batch) - 1; i >= 0; i-- {
			change := e.Batch[i]
			switch {
			case change.Before != nil:
				restoreItem(tx.Store(), *change.Before)
			case change.After != nil:
				removeItem(tx.Store(), change.After.ID)
			}
		}
		return nil
	}
	if e.ProjectAfter != nil || e.ProjectBefore != nil || len(e.ItemsBefore) > 0 {
		switch {
		case e.Op == store.OpCreate:
			if e.ProjectAfter != nil {
				removeProject(tx.Store(), e.ProjectAfter.ID)
			}
		default:
			if e.ProjectBefore != nil {
				restoreProject(tx.Store(), *e.ProjectBefore)
			}
		}
		for _, it := range e.ItemsBefore {
			restoreItem(tx.Store(), it)
		}
		if e.DefaultProjectIDBefore != nil {
			tx.Store().Profile.DefaultProjectID = e.DefaultProjectIDBefore
		}
		return nil
	}

	switch e.Op {
	case store.OpCreate:
		if e.After == nil {
			return errors.New("journal entry is missing the created item")
		}
		removeItem(tx.Store(), e.After.ID)
	case store.OpDelete, store.OpUpdate:
		if e.Before == nil {
			return errors.New("journal entry is missing the previous item")
		}
		restoreItem(tx.Store(), *e.Before)
	default:
		return fmt.Errorf("cannot undo operation %q", e.Op)
	}
	return nil
}

func removeItem(s *model.Store, id string) {
	for i := range s.Items {
		if s.Items[i].ID == id {
			s.Items = append(s.Items[:i], s.Items[i+1:]...)
			return
		}
	}
}

func restoreItem(s *model.Store, it model.Item) {
	for i := range s.Items {
		if s.Items[i].ID == it.ID {
			// Update and rescore journal snapshots omit rawContent to keep the
			// journal small; none of those operations change it, so preserve
			// the current value when restoring.
			if it.RawContent == "" && s.Items[i].RawContent != "" {
				it.RawContent = s.Items[i].RawContent
			}
			s.Items[i] = it
			return
		}
	}
	s.Items = append(s.Items, it)
}

func removeProject(s *model.Store, id string) {
	for i := range s.Projects {
		if s.Projects[i].ID == id {
			s.Projects = append(s.Projects[:i], s.Projects[i+1:]...)
			return
		}
	}
}

func restoreProject(s *model.Store, p model.Project) {
	for i := range s.Projects {
		if s.Projects[i].ID == p.ID {
			s.Projects[i] = p
			return
		}
	}
	s.Projects = append(s.Projects, p)
}

// confirm asks for confirmation. Non-interactive callers must pass --yes.
func (a *App) confirm(question string) bool {
	if a.flags.Yes {
		return true
	}
	f, ok := a.Stdin.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	fmt.Fprintf(a.Err, "%s [y/N] ", question)
	var answer string
	_, _ = fmt.Fscanln(f, &answer)
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes")
}

// ── checklist helpers ────────────────────────────────────────────────────────

func checklistTexts(items []model.ChecklistItem) []string {
	out := make([]string, 0, len(items))
	for _, c := range items {
		out = append(out, c.Text)
	}
	return out
}

func removeChecklistByText(items []model.ChecklistItem, text string) []model.ChecklistItem {
	text = strings.TrimSpace(text)
	out := make([]model.ChecklistItem, 0, len(items))
	for _, c := range items {
		if c.Text != text {
			out = append(out, c)
		}
	}
	return out
}

func setChecklistDone(items []model.ChecklistItem, text string, done bool) {
	text = strings.TrimSpace(text)
	for i := range items {
		if items[i].Text == text {
			items[i].Done = done
		}
	}
}

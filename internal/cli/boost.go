package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/P-Baranwal/clerify/internal/ai"
	"github.com/P-Baranwal/clerify/internal/model"
	"github.com/P-Baranwal/clerify/internal/store"
)

func (a *App) boostCmd() *cobra.Command {
	var (
		reason string
		clear  bool
	)
	cmd := &cobra.Command{
		Use:   "boost <id>",
		Short: "Temporarily raise an item's priority with a reason",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			if clear {
				return a.runBoostClear(args[0])
			}
			if strings.TrimSpace(reason) == "" {
				return fail(2, errors.New("--reason is required (or use --clear)"))
			}
			return a.runBoost(args[0], reason)
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why this item deserves a boost")
	cmd.Flags().BoolVar(&clear, "clear", false, "remove the boost and recompute the baseline")
	return cmd
}

func (a *App) runBoost(ref, reason string) error {
	it, err := a.requireItem(ref)
	if err != nil {
		return err
	}

	res, ok, err := a.analyzeText(contentFor(it), &it, reason)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	score := res.PriorityScore
	r := strings.TrimSpace(reason)
	var saved model.Item
	err = a.DB.Mutate(func(tx *store.Tx) error {
		cur, exists := tx.Item(it.ID)
		if !exists {
			return store.ErrNotFound
		}
		before := cur.Clone()
		cur.Boost = model.Boost{Active: true, Reason: &r, BoostedPriorityScore: &score}
		cur.PriorityReasoning = res.PriorityReasoning
		*cur = model.NormalizeItem(*cur)
		cur.UpdatedAt = model.NowRFC3339(tx.Now())
		saved = cur.Clone()
		tx.Journal(store.Entry{
			TS:     model.NowRFC3339(tx.Now()),
			Op:     store.OpUpdate,
			Label:  "Boosted \"" + saved.Title + "\"",
			Before: &before,
			After:  &saved,
		})
		return nil
	})
	if err != nil {
		return err
	}

	if a.flags.JSON {
		return a.printJSON(saved)
	}
	a.printf("boosted %s  %s -> %d  (%s)\n", shortID(saved.ID), saved.Title, *saved.Boost.BoostedPriorityScore, reason)
	return nil
}

func (a *App) runBoostClear(ref string) error {
	it, err := a.requireItem(ref)
	if err != nil {
		return err
	}

	if a.flags.DryRun {
		draft := it.Clone()
		draft.Boost = model.Boost{}
		return a.printJSON(draft)
	}

	promptItem := it.Clone()
	promptItem.Boost = model.Boost{}
	var (
		score   int
		reason  string
		hasBase bool
	)
	if res, ok, err := a.analyzeText(contentFor(promptItem), &promptItem, ""); err == nil && ok {
		score = res.PriorityScore
		reason = res.PriorityReasoning
		hasBase = true
	}

	var saved model.Item
	err = a.DB.Mutate(func(tx *store.Tx) error {
		cur, exists := tx.Item(it.ID)
		if !exists {
			return store.ErrNotFound
		}
		before := cur.Clone()
		cur.Boost = model.Boost{}
		if hasBase {
			cur.PriorityScore = score
			cur.PriorityReasoning = reason
		}
		*cur = model.NormalizeItem(*cur)
		cur.UpdatedAt = model.NowRFC3339(tx.Now())
		saved = cur.Clone()
		tx.Journal(store.Entry{
			TS:     model.NowRFC3339(tx.Now()),
			Op:     store.OpUpdate,
			Label:  "Cleared boost on \"" + saved.Title + "\"",
			Before: &before,
			After:  &saved,
		})
		return nil
	})
	if err != nil {
		return err
	}

	if a.flags.JSON {
		return a.printJSON(saved)
	}
	a.printf("cleared boost on %s  %s  (priority %d)\n", shortID(saved.ID), saved.Title, saved.PriorityScore)
	return nil
}

func (a *App) rescoreCmd() *cobra.Command {
	var (
		all          bool
		failedOnly   bool
		limit        int
		includeEmpty bool
	)
	cmd := &cobra.Command{
		Use:   "rescore",
		Short: "Re-run AI scoring across items",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			return a.runRescore(all, failedOnly, limit, includeEmpty)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include completed items")
	cmd.Flags().BoolVar(&failedOnly, "failed-only", false, "only items that were never analyzed")
	cmd.Flags().IntVar(&limit, "limit", 0, "stop after N items (0 = all)")
	cmd.Flags().BoolVar(&includeEmpty, "include-empty", false, "also rescore items with only a title")
	return cmd
}

type rescoreResult struct {
	ID  string
	Res ai.AnalysisResult
}

func (a *App) runRescore(includeDone, failedOnly bool, limit int, includeEmpty bool) error {
	targets := make([]model.Item, 0)
	skipped := 0
	for _, it := range a.DB.Items() {
		if !includeDone && it.IsDone() {
			continue
		}
		if failedOnly && it.AnalyzedAt != nil {
			continue
		}
		if !includeEmpty && !hasSubstantiveContent(it) {
			skipped++
			continue
		}
		targets = append(targets, it)
		if limit > 0 && len(targets) >= limit {
			break
		}
	}

	if len(targets) == 0 {
		if a.flags.JSON {
			return a.printJSON(map[string]any{
				"total": 0, "updated": 0, "failed": 0, "skipped": skipped,
			})
		}
		a.printf("Nothing to rescore.\n")
		return nil
	}

	results := make([]rescoreResult, 0, len(targets))
	failed := 0
	for i, it := range targets {
		if !a.flags.Quiet {
			fmt.Fprintf(a.Err, "rescoring %d/%d: %s\n", i+1, len(targets), it.Title)
		}
		res, ok, err := a.analyzeText(contentFor(it), nil, "")
		if err != nil || !ok {
			if err != nil {
				failed++
				if !a.flags.Quiet {
					fmt.Fprintf(a.Err, "  failed: %v\n", err)
				}
				if isAuthError(err) {
					return fail(4, err)
				}
			}
			continue
		}
		results = append(results, rescoreResult{ID: it.ID, Res: res})
	}

	updated := 0
	if len(results) > 0 {
		err := a.DB.Mutate(func(tx *store.Tx) error {
			changes := make([]store.ItemChange, 0, len(results))
			for _, r := range results {
				cur, exists := tx.Item(r.ID)
				if !exists {
					continue
				}
				before := cur.Clone()
				applyAnalysisRefresh(cur, r.Res, false)
				cur.AnalyzedAt = strPtr(model.NowRFC3339(tx.Now()))
				*cur = model.NormalizeItem(*cur)
				cur.UpdatedAt = model.NowRFC3339(tx.Now())
				after := cur.Clone()
				changes = append(changes, store.ItemChange{Before: &before, After: &after})
			}
			if len(changes) > 0 {
				tx.Journal(store.Entry{
					TS:    model.NowRFC3339(tx.Now()),
					Op:    store.OpRescore,
					Label: fmt.Sprintf("Rescored %d item(s)", len(changes)),
					Batch: changes,
				})
			}
			updated = len(changes)
			return nil
		})
		if err != nil {
			return err
		}
	}

	allFailed := updated == 0 && failed > 0
	if a.flags.JSON {
		payload := map[string]any{
			"total":   len(targets),
			"updated": updated,
			"failed":  failed,
			"skipped": skipped,
		}
		if err := a.printJSON(payload); err != nil {
			return err
		}
		if allFailed {
			return fail(4, fmt.Errorf("all %d rescore call(s) failed", failed))
		}
		return nil
	}
	if failed > 0 {
		a.printf("rescored %d/%d items (%d failed, %d skipped)\n", updated, len(targets), failed, skipped)
	} else {
		a.printf("rescored %d items (%d skipped)\n", updated, skipped)
	}
	if allFailed {
		return fail(4, fmt.Errorf("all %d rescore call(s) failed", failed))
	}
	return nil
}

func isAuthError(err error) bool {
	var cfgErr *ai.ConfigError
	if errors.As(err, &cfgErr) {
		return true
	}
	var apiErr *ai.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status == 401 || apiErr.Status == 403
	}
	return false
}

// contentFor picks the text an analysis should see. RawContent is often just
// the original title, so fall through to the description/notes in that case.
func contentFor(it model.Item) string {
	raw := strings.TrimSpace(it.RawContent)
	title := strings.TrimSpace(it.Title)
	if raw != "" && raw != title {
		return raw
	}
	parts := []string{it.Title}
	if strings.TrimSpace(it.Description) != "" {
		parts = append(parts, it.Description)
	}
	if strings.TrimSpace(it.Notes) != "" {
		parts = append(parts, it.Notes)
	}
	return strings.Join(parts, "\n")
}

func hasSubstantiveContent(it model.Item) bool {
	raw := strings.TrimSpace(it.RawContent)
	title := strings.TrimSpace(it.Title)
	if raw != "" && raw != title {
		return true
	}
	return strings.TrimSpace(it.Description) != "" || strings.TrimSpace(it.Notes) != ""
}

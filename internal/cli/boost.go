package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

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
	before := it.Clone()

	res, ok, err := a.analyzeText(contentFor(it), &it, reason)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	score := res.PriorityScore
	r := strings.TrimSpace(reason)
	it.Boost = model.Boost{Active: true, Reason: &r, BoostedPriorityScore: &score}
	it.PriorityReasoning = res.PriorityReasoning

	saved, err := a.DB.UpdateItem(it)
	if err != nil {
		return err
	}
	a.journalItem(store.OpUpdate, "Boosted \""+saved.Title+"\"", &before, &saved)

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
	before := it.Clone()
	it.Boost = model.Boost{}

	// Persist the cleared boost first, mirroring Detail.svelte.
	saved, err := a.DB.UpdateItem(it)
	if err != nil {
		return err
	}

	// Recompute the baseline. Failures are intentionally swallowed: the boost
	// is already gone, which is the user-visible outcome.
	if res, ok, err := a.analyzeText(contentFor(it), nil, ""); err == nil && ok {
		it.PriorityScore = res.PriorityScore
		it.PriorityReasoning = res.PriorityReasoning
		if updated, uerr := a.DB.UpdateItem(it); uerr == nil {
			saved = updated
		}
	}

	a.journalItem(store.OpUpdate, "Cleared boost on \""+saved.Title+"\"", &before, &saved)
	if a.flags.JSON {
		return a.printJSON(saved)
	}
	a.printf("cleared boost on %s  %s  (priority %d)\n", shortID(saved.ID), saved.Title, saved.PriorityScore)
	return nil
}

func (a *App) rescoreCmd() *cobra.Command {
	var (
		all        bool
		failedOnly bool
		limit      int
	)
	cmd := &cobra.Command{
		Use:   "rescore",
		Short: "Re-run AI scoring across items",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			return a.runRescore(all, failedOnly, limit)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include completed items")
	cmd.Flags().BoolVar(&failedOnly, "failed-only", false, "only items that were never analyzed")
	cmd.Flags().IntVar(&limit, "limit", 0, "stop after N items (0 = all)")
	return cmd
}

func (a *App) runRescore(includeDone, failedOnly bool, limit int) error {
	targets := make([]model.Item, 0)
	for _, it := range a.DB.Items() {
		if !includeDone && it.IsDone() {
			continue
		}
		if failedOnly && it.AnalyzedAt != nil {
			continue
		}
		targets = append(targets, it)
		if limit > 0 && len(targets) >= limit {
			break
		}
	}

	if len(targets) == 0 {
		a.printf("Nothing to rescore.\n")
		return nil
	}

	updated, failed := 0, 0
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
			}
			continue
		}
		before := it.Clone()
		applyAnalysis(&it, res)
		it.AnalyzedAt = strPtr(model.NowRFC3339(a.now()))
		if _, err := a.DB.UpdateItem(it); err != nil {
			failed++
			continue
		}
		after := it
		a.journalItem(store.OpUpdate, "Rescored \""+it.Title+"\"", &before, &after)
		updated++
	}

	if failed > 0 {
		a.printf("rescored %d/%d items (%d failed)\n", updated, len(targets), failed)
	} else {
		a.printf("rescored %d items\n", updated)
	}
	return nil
}

// contentFor picks the text an analysis should see.
func contentFor(it model.Item) string {
	if strings.TrimSpace(it.RawContent) != "" {
		return it.RawContent
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

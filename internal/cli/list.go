package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/P-Baranwal/clerify/internal/derive"
	"github.com/P-Baranwal/clerify/internal/model"
	"github.com/P-Baranwal/clerify/internal/render"
)

func (a *App) lsCmd() *cobra.Command {
	var (
		statuses   []string
		projectRef string
		overdue    bool
		dueWithin  int
		typeFilter string
		sortKey    string
		limit      int
		includeAll bool
		doneOnly   bool
	)
	cmd := &cobra.Command{
		Use:     "ls [query]",
		Aliases: []string{"list"},
		Short:   "List items",
		Long:    "List items with a stable #N reference number. The number follows priority order, not the current sort; use it, an id prefix, or a title in other commands.",
		Example: `  clerify ls
  clerify ls --overdue
  clerify ls --status doing --project website`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			return a.runLs(cmd, args, statuses, projectRef, overdue, dueWithin, typeFilter, sortKey, limit, includeAll, doneOnly)
		},
	}

	fl := cmd.Flags()
	fl.StringSliceVar(&statuses, "status", nil, "filter by status: todo|doing|in_progress|blocked|done (repeatable)")
	fl.StringVar(&projectRef, "project", "", "filter by project id, id prefix, or title")
	fl.BoolVar(&overdue, "overdue", false, "only items past their deadline")
	fl.IntVar(&dueWithin, "due-within", 0, "only items due within N days")
	fl.StringVar(&typeFilter, "type", "", "filter by type")
	fl.StringVar(&sortKey, "sort", derive.SortPriority, "priority|due|updated|roi (impact for effort)|title")
	fl.IntVar(&limit, "limit", 0, "maximum rows (0 = all)")
	fl.BoolVar(&includeAll, "all", false, "include completed items")
	fl.BoolVar(&doneOnly, "done", false, "only completed items")
	_ = cmd.RegisterFlagCompletionFunc("status", a.completeStatuses)
	_ = cmd.RegisterFlagCompletionFunc("sort", a.completeSortKeys)
	_ = cmd.RegisterFlagCompletionFunc("type", a.completeTypes)
	_ = cmd.RegisterFlagCompletionFunc("project", a.completeProjectRefs)
	return cmd
}

func (a *App) runLs(cmd *cobra.Command, args []string, statuses []string, projectRef string, overdue bool,
	dueWithin int, typeFilter, sortKey string, limit int, includeAll, doneOnly bool) error {

	switch sortKey {
	case "", derive.SortPriority, derive.SortDue, derive.SortUpdated, derive.SortROI, derive.SortTitle:
	default:
		return fail(2, fmt.Errorf("unknown sort key %q (use priority, due, updated, roi, or title)", sortKey))
	}

	canonicalStatuses := make([]string, 0, len(statuses))
	for _, s := range statuses {
		st, err := model.ParseStatus(s)
		if err != nil {
			return fail(2, err)
		}
		canonicalStatuses = append(canonicalStatuses, st)
	}
	canonicalType := ""
	if strings.TrimSpace(typeFilter) != "" {
		typ, err := model.ParseType(typeFilter)
		if err != nil {
			return fail(2, err)
		}
		canonicalType = typ
	}

	filter := derive.Filter{
		Statuses:    canonicalStatuses,
		Type:        canonicalType,
		Overdue:     overdue,
		Query:       strings.Join(args, " "),
		IncludeDone: includeAll,
		OnlyDone:    doneOnly,
	}
	if cmd.Flags().Changed("due-within") {
		filter.DueWithin = &dueWithin
	}
	if strings.TrimSpace(projectRef) != "" {
		p, err := a.DB.FindProject(projectRef)
		if err != nil {
			return notFound("project", projectRef, err)
		}
		filter.ProjectID = &p.ID
	}

	items := derive.Apply(a.DB.Items(), filter, sortKey, a.now())
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}

	if a.flags.JSON {
		return a.printJSON(items)
	}
	if len(items) == 0 {
		if len(a.DB.Items()) == 0 {
			a.printf("No items yet. Try: clerify add \"Write report\" --due 2026-11-05\n")
		} else {
			a.printf("No items match those filters. Try: clerify ls --all\n")
		}
		return nil
	}
	render.ItemTableNumbered(a.Out, items, a.DB.ProjectTitle, a.now(), referenceNumbers(a.DB.Items()))
	return nil
}

func (a *App) showCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "show <id>",
		Short:             "Show one item in full",
		Example:           "  clerify show #1\n  clerify show 7f3a1b",
		Args:              exactArgs(1),
		ValidArgsFunction: a.completeItemIDs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			it, err := a.requireItem(args[0])
			if err != nil {
				return err
			}
			if a.flags.JSON {
				return a.printJSON(it)
			}
			render.ItemDetail(a.Out, it, a.DB.ProjectTitle, a.now())
			return nil
		},
	}
}

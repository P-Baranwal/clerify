package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/P-Baranwal/clerify/internal/derive"
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
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			return a.runLs(cmd, args, statuses, projectRef, overdue, dueWithin, typeFilter, sortKey, limit, includeAll, doneOnly)
		},
	}

	fl := cmd.Flags()
	fl.StringSliceVar(&statuses, "status", nil, "filter by status: todo|in_progress|blocked|done (repeatable)")
	fl.StringVar(&projectRef, "project", "", "filter by project id, id prefix, or title")
	fl.BoolVar(&overdue, "overdue", false, "only items past their deadline")
	fl.IntVar(&dueWithin, "due-within", 0, "only items due within N days")
	fl.StringVar(&typeFilter, "type", "", "filter by type")
	fl.StringVar(&sortKey, "sort", derive.SortPriority, "priority|due|updated|roi|title")
	fl.IntVar(&limit, "limit", 0, "maximum rows (0 = all)")
	fl.BoolVar(&includeAll, "all", false, "include completed items")
	fl.BoolVar(&doneOnly, "done", false, "only completed items")
	return cmd
}

func (a *App) runLs(cmd *cobra.Command, args []string, statuses []string, projectRef string, overdue bool,
	dueWithin int, typeFilter, sortKey string, limit int, includeAll, doneOnly bool) error {

	switch sortKey {
	case "", derive.SortPriority, derive.SortDue, derive.SortUpdated, derive.SortROI, derive.SortTitle:
	default:
		return fail(2, fmt.Errorf("unknown sort key %q (use priority, due, updated, roi, or title)", sortKey))
	}

	filter := derive.Filter{
		Statuses:    statuses,
		Type:        typeFilter,
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
		a.printf("No matching items.\n")
		return nil
	}
	render.ItemTable(a.Out, items, a.DB.ProjectTitle, a.now())
	return nil
}

func (a *App) showCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show one item in full",
		Args:  exactArgs(1),
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

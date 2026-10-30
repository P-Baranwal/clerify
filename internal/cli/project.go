package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/P-Baranwal/clerify/internal/ai"
	"github.com/P-Baranwal/clerify/internal/format"
	"github.com/P-Baranwal/clerify/internal/model"
	"github.com/P-Baranwal/clerify/internal/pdf"
	"github.com/P-Baranwal/clerify/internal/render"
	"github.com/P-Baranwal/clerify/internal/store"
)

func (a *App) projectCmd() *cobra.Command {
	parent := &cobra.Command{
		Use:     "project",
		Aliases: []string{"proj"},
		Short:   "Manage projects",
	}
	parent.AddCommand(a.projectNewCmd(), a.projectLsCmd(), a.projectShowCmd(), a.projectDoneCmd(), a.projectRmCmd())
	return parent
}

func (a *App) projectNewCmd() *cobra.Command {
	var (
		title     string
		brief     string
		briefFile string
		withWBS   bool
	)
	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create a project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			if strings.TrimSpace(title) == "" {
				return fail(2, errors.New("--title is required"))
			}
			body, err := a.briefText(brief, briefFile)
			if err != nil {
				return err
			}
			if withWBS && strings.TrimSpace(body) == "" {
				return fail(2, errors.New("--wbs needs a brief (--brief or --brief-file)"))
			}

			var wbsItems []ai.WBSItem
			if withWBS {
				items, ok, err := a.generateWBS(body)
				if err != nil {
					return err
				}
				if !ok {
					return nil // --dry-run printed the prompt
				}
				wbsItems = items
			}

			project, created, err := a.createProject(title, body, wbsItems)
			if err != nil {
				return err
			}

			if a.flags.JSON {
				if withWBS {
					return a.printJSON(map[string]any{"project": project, "items": created})
				}
				return a.printJSON(project)
			}
			a.printf("created project %s  %s\n", shortID(project.ID), project.Title)
			if withWBS {
				a.printf("created %d item(s) in %s\n", len(created), project.Title)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "project title")
	cmd.Flags().StringVar(&brief, "brief", "", "client brief / feature request")
	cmd.Flags().StringVar(&briefFile, "brief-file", "", "read the brief from a file")
	cmd.Flags().BoolVar(&withWBS, "wbs", false, "generate and create sub-tasks from the brief")
	return cmd
}

func (a *App) createProject(title, brief string, wbs []ai.WBSItem) (model.Project, []model.Item, error) {
	var (
		project model.Project
		created []model.Item
	)
	err := a.DB.Mutate(func(tx *store.Tx) error {
		now := model.NowRFC3339(tx.Now())
		project = model.NormalizeProject(model.Project{Title: title, Brief: brief, ID: model.NewID()})
		project.CreatedAt = now
		project.UpdatedAt = now
		tx.Store().Projects = append(tx.Store().Projects, project)
		tx.Journal(store.Entry{
			TS:           now,
			Op:           store.OpCreate,
			Label:        "Created project \"" + project.Title + "\"",
			ProjectAfter: &project,
		})

		for _, w := range wbs {
			it := model.NewItem(w.Title)
			it.EstimatedHours = w.EstimatedHours
			impact := w.ImpactScore
			it.ImpactScore = &impact
			it.ProjectID = &project.ID
			it.Description = render.Truncate(brief, 500)
			it = model.NormalizeItem(it)
			it.CreatedAt = now
			it.UpdatedAt = now
			tx.Store().Items = append(tx.Store().Items, it)
			created = append(created, it.Clone())
		}
		if len(created) > 0 {
			changes := make([]store.ItemChange, 0, len(created))
			for i := range created {
				changes = append(changes, store.ItemChange{After: &created[i]})
			}
			tx.Journal(store.Entry{
				TS:    now,
				Op:    store.OpCreate,
				Label: fmt.Sprintf("Added %d WBS item(s)", len(created)),
				Batch: changes,
			})
		}
		return nil
	})
	return project, created, err
}

func (a *App) projectLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List projects",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			projects := a.DB.Projects()
			if a.flags.JSON {
				return a.printJSON(projects)
			}
			if len(projects) == 0 {
				a.printf("No projects yet.\n")
				return nil
			}
			counts := make(map[string]int)
			for _, it := range a.DB.Items() {
				if it.HasProject() {
					counts[*it.ProjectID]++
				}
			}
			render.ProjectTable(a.Out, projects, counts)
			return nil
		},
	}
}

func (a *App) projectShowCmd() *cobra.Command {
	var board bool
	cmd := &cobra.Command{
		Use:   "show <ref>",
		Short: "Show a project and its items",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			p, err := a.DB.FindProject(args[0])
			if err != nil {
				return notFound("project", args[0], err)
			}
			items := make([]model.Item, 0)
			for _, it := range a.DB.Items() {
				if it.HasProject() && *it.ProjectID == p.ID {
					items = append(items, it)
				}
			}
			if a.flags.JSON {
				return a.printJSON(map[string]any{"project": p, "items": items, "board": board})
			}
			fmt.Fprintf(a.Out, "%s\n%s\n\n", p.Title, strings.Repeat("─", render.Width(p.Title)))
			fmt.Fprintf(a.Out, "%-16s %s\n", "id", p.ID)
			fmt.Fprintf(a.Out, "%-16s %s\n", "status", p.Status)
			fmt.Fprintf(a.Out, "%-16s %s\n", "created", render.FormatTimestamp(p.CreatedAt))
			fmt.Fprintf(a.Out, "%-16s %d\n", "items", len(items))
			if p.Brief != "" {
				fmt.Fprintf(a.Out, "\nbrief\n  %s\n", render.Truncate(p.Brief, 400))
			}
			if len(items) > 0 {
				fmt.Fprintln(a.Out)
				if board {
					render.Board(a.Out, items)
				} else {
					render.ItemTable(a.Out, items, a.DB.ProjectTitle, a.now())
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&board, "board", false, "group items into status columns")
	return cmd
}

func (a *App) projectDoneCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "done <ref>",
		Short: "Mark a project done",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			p, err := a.DB.FindProject(args[0])
			if err != nil {
				return notFound("project", args[0], err)
			}

			var saved model.Project
			err = a.DB.Mutate(func(tx *store.Tx) error {
				cur, ok := tx.Project(p.ID)
				if !ok {
					return store.ErrNotFound
				}
				before := *cur
				cur.Status = model.ProjectDone
				*cur = model.NormalizeProject(*cur)
				cur.UpdatedAt = model.NowRFC3339(tx.Now())
				saved = *cur
				tx.Journal(store.Entry{
					TS:            model.NowRFC3339(tx.Now()),
					Op:            store.OpUpdate,
					Label:         "Completed project \"" + saved.Title + "\"",
					ProjectBefore: &before,
					ProjectAfter:  &saved,
				})
				return nil
			})
			if err != nil {
				return err
			}
			if a.flags.JSON {
				return a.printJSON(saved)
			}
			a.printf("project %s is done\n", shortID(saved.ID))
			return nil
		},
	}
}

func (a *App) projectRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "rm <ref>",
		Aliases: []string{"delete"},
		Short:   "Delete a project (its items become inbox items)",
		Args:    exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			p, err := a.DB.FindProject(args[0])
			if err != nil {
				return notFound("project", args[0], err)
			}
			if !a.confirm(fmt.Sprintf("Delete project %q? Its items will be kept.", p.Title)) {
				return fail(1, errors.New("aborted (pass --yes to skip confirmation)"))
			}

			err = a.DB.Mutate(func(tx *store.Tx) error {
				cur, ok := tx.Project(p.ID)
				if !ok {
					return store.ErrNotFound
				}
				before := *cur
				var detached []model.Item
				for i := range tx.Store().Items {
					it := &tx.Store().Items[i]
					if it.ProjectID != nil && *it.ProjectID == p.ID {
						detached = append(detached, it.Clone())
						it.ProjectID = nil
					}
				}
				var defaultBefore *string
				if tx.Store().Profile.DefaultProjectID != nil && *tx.Store().Profile.DefaultProjectID == p.ID {
					value := *tx.Store().Profile.DefaultProjectID
					defaultBefore = &value
					tx.Store().Profile.DefaultProjectID = nil
				}
				removeProject(tx.Store(), p.ID)
				tx.Journal(store.Entry{
					TS:                     model.NowRFC3339(tx.Now()),
					Op:                     store.OpDelete,
					Label:                  "Deleted project \"" + before.Title + "\"",
					ProjectBefore:          &before,
					DefaultProjectIDBefore: defaultBefore,
					ItemsBefore:            detached,
				})
				return nil
			})
			if err != nil {
				return err
			}
			a.printf("deleted project %s  %s\n", shortID(p.ID), p.Title)
			return nil
		},
	}
}

func (a *App) wbsCmd() *cobra.Command {
	var (
		brief     string
		briefFile string
		apply     bool
	)
	cmd := &cobra.Command{
		Use:   "wbs [project]",
		Short: "Generate a work breakdown structure",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}

			var project model.Project
			haveProject := false
			if len(args) == 1 {
				p, err := a.DB.FindProject(args[0])
				if err != nil {
					return notFound("project", args[0], err)
				}
				project, haveProject = p, true
			}

			body, err := a.briefText(brief, briefFile)
			if err != nil {
				return err
			}
			if strings.TrimSpace(body) == "" && haveProject {
				body = project.Brief
			}
			if strings.TrimSpace(body) == "" {
				return fail(2, errors.New("no brief: pass --brief, --brief-file, or a project with a brief"))
			}

			items, ok, err := a.generateWBS(body)
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}

			if !apply {
				if a.flags.JSON {
					return a.printJSON(map[string]any{"brief": body, "items": items})
				}
				headers := []string{"TITLE", "EST", "IMPACT"}
				rows := make([][]string, 0, len(items))
				for _, it := range items {
					rows = append(rows, []string{render.Truncate(it.Title, 56), format.Hours(it.EstimatedHours), fmt.Sprintf("%d", it.ImpactScore)})
				}
				render.Table(a.Out, headers, rows)
				a.printf("\nnot created; re-run with --apply\n")
				return nil
			}

			var created []model.Item
			if !haveProject {
				project, created, err = a.createProject(deriveTitle(body), body, items)
			} else {
				created, err = a.applyWBSItems(project, items)
			}
			if err != nil {
				return err
			}

			if a.flags.JSON {
				return a.printJSON(map[string]any{"project": project, "items": created})
			}
			if !haveProject {
				a.printf("created project %s  %s\n", shortID(project.ID), project.Title)
			}
			a.printf("created %d item(s) in %s\n", len(created), project.Title)
			return nil
		},
	}
	cmd.Flags().StringVar(&brief, "brief", "", "brief text")
	cmd.Flags().StringVar(&briefFile, "brief-file", "", "read the brief from a file")
	cmd.Flags().BoolVar(&apply, "apply", false, "create the items")
	return cmd
}

func (a *App) applyWBSItems(project model.Project, items []ai.WBSItem) ([]model.Item, error) {
	var created []model.Item
	err := a.DB.Mutate(func(tx *store.Tx) error {
		now := model.NowRFC3339(tx.Now())
		for _, w := range items {
			it := model.NewItem(w.Title)
			it.EstimatedHours = w.EstimatedHours
			impact := w.ImpactScore
			it.ImpactScore = &impact
			it.ProjectID = &project.ID
			it = model.NormalizeItem(it)
			it.CreatedAt = now
			it.UpdatedAt = now
			tx.Store().Items = append(tx.Store().Items, it)
			created = append(created, it.Clone())
		}
		if len(created) > 0 {
			changes := make([]store.ItemChange, 0, len(created))
			for i := range created {
				changes = append(changes, store.ItemChange{After: &created[i]})
			}
			tx.Journal(store.Entry{
				TS:    now,
				Op:    store.OpCreate,
				Label: fmt.Sprintf("Added %d WBS item(s)", len(created)),
				Batch: changes,
			})
		}
		return nil
	})
	return created, err
}

// generateWBS calls the provider. ok is false when --dry-run printed the prompt.
func (a *App) generateWBS(brief string) ([]ai.WBSItem, bool, error) {
	system, user := ai.BuildWBSPrompt(a.truncate(brief), a.now())
	if a.flags.DryRun {
		fmt.Fprintln(a.Out, "--- system ---")
		fmt.Fprintln(a.Out, system)
		fmt.Fprintln(a.Out, "--- user ---")
		fmt.Fprintln(a.Out, user)
		return nil, false, nil
	}
	client := a.aiClient()
	out, err := client.Analyze(context.Background(), system, user, "wbs_generate")
	if err != nil {
		return nil, false, err
	}
	items, err := ai.ParseWBS(out)
	if err != nil {
		return nil, false, err
	}
	return items, true, nil
}

// briefText resolves a brief from inline text or a file. PDF extraction
// failures are surfaced instead of falling back to raw bytes.
func (a *App) briefText(inline, file string) (string, error) {
	if strings.TrimSpace(file) != "" {
		text, err := pdf.Extract(file)
		if err != nil {
			return "", fail(2, err)
		}
		return strings.TrimSpace(text), nil
	}
	return strings.TrimSpace(inline), nil
}

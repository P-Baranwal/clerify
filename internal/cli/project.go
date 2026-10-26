package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/P-Baranwal/clerify/internal/ai"
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

			project, err := a.DB.AddProject(model.Project{Title: title, Brief: body})
			if err != nil {
				return err
			}
			a.journalProject(store.OpCreate, "Created project \""+project.Title+"\"", nil, &project)

			if a.flags.JSON {
				return a.printJSON(project)
			}
			a.printf("created project %s  %s\n", shortID(project.ID), project.Title)

			if withWBS {
				if strings.TrimSpace(body) == "" {
					return fail(2, errors.New("--wbs needs a brief (--brief or --brief-file)"))
				}
				return a.applyWBS(project, body)
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
	return &cobra.Command{
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
				return a.printJSON(map[string]any{"project": p, "items": items})
			}
			fmt.Fprintf(a.Out, "%s\n%s\n\n", p.Title, strings.Repeat("─", len(p.Title)))
			fmt.Fprintf(a.Out, "%-16s %s\n", "id", p.ID)
			fmt.Fprintf(a.Out, "%-16s %s\n", "status", p.Status)
			fmt.Fprintf(a.Out, "%-16s %s\n", "created", p.CreatedAt)
			fmt.Fprintf(a.Out, "%-16s %d\n", "items", len(items))
			if p.Brief != "" {
				fmt.Fprintf(a.Out, "\nbrief\n  %s\n", render.Truncate(p.Brief, 400))
			}
			if len(items) > 0 {
				fmt.Fprintln(a.Out)
				render.ItemTable(a.Out, items, a.DB.ProjectTitle, a.now())
			}
			return nil
		},
	}
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
			before := p
			p.Status = model.ProjectDone
			saved, err := a.DB.UpdateProject(p)
			if err != nil {
				return err
			}
			a.journalProject(store.OpUpdate, "Completed project \""+saved.Title+"\"", &before, &saved)
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
			before := p
			if _, err := a.DB.DeleteProject(p.ID); err != nil {
				return err
			}
			a.journalProject(store.OpDelete, "Deleted project \""+before.Title+"\"", &before, nil)
			a.printf("deleted project %s  %s\n", shortID(before.ID), before.Title)
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

			if !apply {
				items, ok, err := a.generateWBS(body)
				if err != nil {
					return err
				}
				if !ok {
					return nil
				}
				headers := []string{"TITLE", "EST", "IMPACT"}
				rows := make([][]string, 0, len(items))
				for _, it := range items {
					rows = append(rows, []string{render.Truncate(it.Title, 56), hours(it.EstimatedHours), fmt.Sprintf("%d", it.ImpactScore)})
				}
				render.Table(a.Out, headers, rows)
				a.printf("\nnot created; re-run with --apply\n")
				return nil
			}

			if !haveProject {
				created, err := a.DB.AddProject(model.Project{Title: deriveTitle(body), Brief: body})
				if err != nil {
					return err
				}
				project, haveProject = created, true
				a.journalProject(store.OpCreate, "Created project \""+created.Title+"\"", nil, &created)
				a.printf("created project %s  %s\n", shortID(created.ID), created.Title)
			}
			return a.applyWBS(project, body)
		},
	}
	cmd.Flags().StringVar(&brief, "brief", "", "brief text")
	cmd.Flags().StringVar(&briefFile, "brief-file", "", "read the brief from a file")
	cmd.Flags().BoolVar(&apply, "apply", false, "create the items")
	return cmd
}

func (a *App) applyWBS(project model.Project, brief string) error {
	items, ok, err := a.generateWBS(brief)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	created := 0
	for _, w := range items {
		it := model.NewItem(w.Title)
		it.EstimatedHours = w.EstimatedHours
		impact := w.ImpactScore
		it.ImpactScore = &impact
		it.ProjectID = &project.ID
		it.Description = render.Truncate(brief, 500)
		saved, err := a.DB.AddItem(it)
		if err != nil {
			return err
		}
		a.journalItem(store.OpCreate, "Added \""+saved.Title+"\"", nil, &saved)
		created++
	}
	a.printf("created %d item(s) in %s\n", created, project.Title)
	return nil
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

// briefText resolves a brief from inline text or a file.
func (a *App) briefText(inline, file string) (string, error) {
	if strings.TrimSpace(file) != "" {
		text, err := pdf.Extract(file)
		if err != nil {
			if data, rerr := os.ReadFile(file); rerr == nil {
				return strings.TrimSpace(string(data)), nil
			}
			return "", fail(2, err)
		}
		return strings.TrimSpace(text), nil
	}
	return strings.TrimSpace(inline), nil
}

package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/P-Baranwal/clerify/internal/ai"
	"github.com/P-Baranwal/clerify/internal/derive"
	"github.com/P-Baranwal/clerify/internal/format"
	"github.com/P-Baranwal/clerify/internal/model"
	"github.com/P-Baranwal/clerify/internal/render"
)

func (a *App) todayCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:     "today",
		Short:   "What needs attention today",
		Long:    "Show overdue items, what is due today, and the top five priorities. Inbox means items not in a project; ROI means impact for the effort. Those extra sections appear when they have data; --all shows every section.",
		Example: "  clerify today\n  clerify today --all",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			return a.runToday(all)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "show every section, even when empty")
	return cmd
}

type todayOutput struct {
	Date     string            `json:"date"`
	Overdue  []model.Item      `json:"overdue"`
	DueToday []model.Item      `json:"dueToday"`
	Top      []model.Item      `json:"top"`
	Inbox    []model.Item      `json:"inbox,omitempty"`
	HighROI  []model.Item      `json:"highRoi,omitempty"`
	Risk     derive.RiskReport `json:"risk"`
	Blocks   []model.PlanBlock `json:"planBlocks,omitempty"`
}

func (a *App) runToday(all bool) error {
	now := a.now()
	items := a.DB.Items()

	out := todayOutput{Date: now.Format(model.DateLayout)}
	for _, it := range items {
		if it.IsDone() || it.Deadline == nil {
			continue
		}
		switch {
		case model.IsOverdue(*it.Deadline, now):
			out.Overdue = append(out.Overdue, it)
		default:
			if days, ok := model.DaysUntil(*it.Deadline, now); ok && days == 0 {
				out.DueToday = append(out.DueToday, it)
			}
		}
	}
	derive.SortBy(out.Overdue, derive.SortDue)
	derive.SortBy(out.DueToday, derive.SortPriority)

	top := derive.PriorityList(items)
	if len(top) > 5 {
		top = top[:5]
	}
	out.Top = top

	out.Inbox = derive.Inbox(items)
	if len(out.Inbox) > 5 {
		out.Inbox = out.Inbox[:5]
	}
	out.HighROI = derive.HighROI(items, 3)

	out.Risk = derive.Risk(items, now, a.DB.Data.Profile.AvailableHoursPerDay)
	out.Blocks = a.todayBlocks(now)

	if a.flags.JSON {
		return a.printJSON(out)
	}

	fmt.Fprintf(a.Out, "Today %s\n%s\n\n", out.Date, strings.Repeat("─", 16))

	if len(items) == 0 {
		a.printf("No tasks yet. Try: clerify add \"Write report\" --due tomorrow\n")
		return nil
	}
	if !all && len(out.Top) == 0 {
		a.printf("Nothing open right now. Try: clerify add \"Next task\"\n")
		return nil
	}

	section := func(title string, list []model.Item) {
		fmt.Fprintln(a.Out, title)
		if len(list) == 0 {
			fmt.Fprintln(a.Out, "  (nothing)")
		} else {
			render.ItemTable(a.Out, list, a.DB.ProjectTitle, now)
		}
		fmt.Fprintln(a.Out)
	}

	if all || len(out.Overdue) > 0 {
		section("OVERDUE", out.Overdue)
	}
	if all || len(out.DueToday) > 0 {
		section("DUE TODAY", out.DueToday)
	}
	if all || len(out.Top) > 0 {
		section("TOP PRIORITY", out.Top)
	}
	if all || len(out.Inbox) > 0 {
		allInbox := derive.Inbox(a.DB.Items())
		section(fmt.Sprintf("INBOX (%d)", len(allInbox)), out.Inbox)
	}
	if all || len(out.HighROI) > 0 {
		section("HIGH ROI (impact for the effort)", out.HighROI)
	}
	if all || out.Risk.TaskCount > 0 {
		fmt.Fprintf(a.Out, "CAPACITY   %s due in 7 days across %d items; %s available",
			format.Hours(out.Risk.TotalHours), out.Risk.TaskCount, format.Hours(out.Risk.AvailableHours))
		if out.Risk.Overloaded {
			if out.Risk.Urgent {
				fmt.Fprintf(a.Out, "  -- OVERLOADED (%.2fx), reschedule urgently", out.Risk.Ratio)
			} else {
				fmt.Fprintf(a.Out, "  -- over capacity (%.2fx), consider rescheduling", out.Risk.Ratio)
			}
		}
		fmt.Fprintln(a.Out)
		fmt.Fprintln(a.Out)
	}
	if all || len(out.Blocks) > 0 {
		fmt.Fprintln(a.Out, "TODAY'S PLAN")
		if len(out.Blocks) == 0 {
			fmt.Fprintln(a.Out, "  (nothing scheduled; run `clerify plan --refresh`)")
		}
		for _, b := range out.Blocks {
			fmt.Fprintf(a.Out, "  %-10s %s  (%s)\n", b.Slot, b.Task, format.Hours(b.Hours))
		}
	}
	return nil
}

// printWelcome is the first-run screen. It names the three things a newcomer
// actually needs: capture, review, and the optional AI step.
func (a *App) printWelcome() {
	a.printf("Welcome to clerify — your tasks live in one local JSON file.\n\n")
	a.printf("Add your first task:  clerify add \"Write report\" --due friday\n")
	a.printf("See what's next:      clerify today\n")
	a.printf("AI scoring (optional): clerify init    Everything else works offline.\n")
}

// todayBlocks returns the cached plan's blocks for today, if any.
func (a *App) todayBlocks(now time.Time) []model.PlanBlock {
	plan := a.DB.Data.Plan
	if plan == nil {
		return nil
	}
	today := now.Format(model.DateLayout)
	for _, d := range plan.Days {
		if d.Date != nil && *d.Date == today {
			return d.Blocks
		}
	}
	if len(plan.Days) > 0 && plan.Days[0].Date == nil {
		return plan.Days[0].Blocks
	}
	return nil
}

func (a *App) planCmd() *cobra.Command {
	var (
		refresh bool
		days    int
	)
	cmd := &cobra.Command{
		Use:     "plan",
		Short:   "Show or regenerate the weekly plan",
		Long:    "Show the cached weekly plan. --refresh asks the AI to build a new one from the current items.",
		Example: "  clerify plan\n  clerify plan --refresh --days 3",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			if refresh {
				if err := a.refreshPlan(); err != nil {
					return err
				}
			}
			return a.showPlan(days)
		},
	}
	cmd.Flags().BoolVar(&refresh, "refresh", false, "regenerate the plan with AI")
	cmd.Flags().IntVar(&days, "days", 0, "only show the first N days")
	return cmd
}

func (a *App) refreshPlan() error {
	now := a.now()
	active := derive.PriorityList(a.DB.Items())

	system, user := ai.BuildWeeklyPlanPrompt(a.DB.Data.Profile, active, now)
	if a.flags.DryRun {
		fmt.Fprintln(a.Out, "--- system ---")
		fmt.Fprintln(a.Out, system)
		fmt.Fprintln(a.Out, "--- user ---")
		fmt.Fprintln(a.Out, user)
		return nil
	}

	client := a.aiClient()
	out, err := client.Analyze(context.Background(), system, user, "weekly_plan")
	if err != nil {
		return err
	}
	parsed, err := ai.ParseWeeklyPlan(out)
	if err != nil {
		return err
	}

	plan := &model.Plan{
		GeneratedAt:      model.NowRFC3339(now),
		MaxItemUpdatedAt: derive.MaxUpdatedAt(a.DB.Items()),
		Days:             parsed,
	}
	if err := a.DB.SetPlan(plan); err != nil {
		return err
	}
	if !a.flags.JSON {
		a.printf("plan generated (%d days)\n", len(parsed))
	}
	return nil
}

func (a *App) showPlan(days int) error {
	plan := a.DB.Data.Plan
	if plan == nil {
		if a.flags.JSON {
			return a.printJSON(nil)
		}
		a.printf("No plan cached. Run `clerify plan --refresh`.\n")
		return nil
	}

	if !a.flags.Quiet {
		currentMax := derive.MaxUpdatedAt(a.DB.Items())
		if plan.MaxItemUpdatedAt != "" && currentMax != plan.MaxItemUpdatedAt {
			fmt.Fprintln(a.Err, "note: items changed since this plan was generated; run `clerify plan --refresh`")
		}
	}

	view := *plan
	if days > 0 && len(view.Days) > days {
		view.Days = view.Days[:days]
	}

	if a.flags.JSON {
		return a.printJSON(view)
	}
	render.PlanView(a.Out, &view, a.DB.Data.Profile.AvailableHoursPerDay)
	return nil
}

func (a *App) calendarCmd() *cobra.Command {
	var month string
	cmd := &cobra.Command{
		Use:     "calendar",
		Short:   "Show a month grid with deadlines",
		Example: "  clerify calendar\n  clerify calendar --month 2026-11",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			now := a.now()
			year, mon, err := parseMonth(month, now)
			if err != nil {
				return fail(2, err)
			}
			leading, days := derive.MonthGrid(a.DB.Items(), year, mon, now)

			if a.flags.JSON {
				return a.printJSON(map[string]any{
					"year":          year,
					"month":         int(mon),
					"leadingBlanks": leading,
					"days":          days,
					"undated":       derive.Undated(a.DB.Items()),
				})
			}
			render.CalendarMonth(a.Out, year, mon, leading, days)

			undated := derive.Undated(a.DB.Items())
			if len(undated) > 0 {
				fmt.Fprintf(a.Out, "\nUndated (%d)\n", len(undated))
				for _, it := range undated {
					fmt.Fprintf(a.Out, "  %s  %s\n", shortID(it.ID), render.Truncate(it.Title, 52))
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&month, "month", "", "month as YYYY-MM (default: current month)")
	return cmd
}

func parseMonth(s string, now time.Time) (int, time.Month, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return now.Year(), now.Month(), nil
	}
	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected YYYY-MM, got %q", s)
	}
	year, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid year in %q", s)
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 1 || m > 12 {
		return 0, 0, fmt.Errorf("invalid month in %q", s)
	}
	return year, time.Month(m), nil
}

package render

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/P-Baranwal/clerify/internal/derive"
	"github.com/P-Baranwal/clerify/internal/model"
)

// ReportData bundles everything a report needs.
type ReportData struct {
	Items                []model.Item
	Projects             []model.Project
	Now                  time.Time
	AvailableHoursPerDay int
}

// Report writes the requested sections with the default 8-week window.
func Report(w io.Writer, d ReportData, sections []string) {
	ReportWithWeeks(w, d, sections, 8)
}

// ReportWithWeeks writes the requested sections. An empty list or "all" prints
// everything.
func ReportWithWeeks(w io.Writer, d ReportData, sections []string, weeks int) {
	if weeks <= 0 {
		weeks = 8
	}
	include := func(name string) bool {
		if len(sections) == 0 {
			return true
		}
		for _, s := range sections {
			if s == "all" || s == name {
				return true
			}
		}
		return false
	}

	first := true
	section := func(title string) {
		if !first {
			fmt.Fprintln(w)
		}
		first = false
		fmt.Fprintln(w, title)
		fmt.Fprintln(w, strings.Repeat("─", Width(title)))
	}

	if include("summary") {
		section("SUMMARY")
		completed := 0
		for _, it := range d.Items {
			if it.IsDone() {
				completed++
			}
		}
		rate := 0.0
		if len(d.Items) > 0 {
			rate = float64(completed) / float64(len(d.Items)) * 100
		}
		fmt.Fprintf(w, "Total items       %d\n", len(d.Items))
		fmt.Fprintf(w, "Completed         %d\n", completed)
		fmt.Fprintf(w, "Completion rate   %.1f%%\n", rate)
		fmt.Fprintf(w, "Current streak    %d days\n", derive.Streak(d.Items, d.Now))
		risk := derive.Risk(d.Items, d.Now, d.AvailableHoursPerDay)
		if risk.Overloaded {
			fmt.Fprintf(w, "Overload          %.1fh due in 7d vs %.0fh capacity (%.2fx)\n",
				risk.TotalHours, risk.AvailableHours, risk.Ratio)
		}
	}

	if include("velocity") {
		section(fmt.Sprintf("VELOCITY (last %d weeks)", weeks))
		rows := make([][]string, 0, weeks)
		for _, b := range derive.Velocity(d.Items, d.Now, weeks) {
			rows = append(rows, []string{
				b.Label,
				fmt.Sprintf("%d", b.Count),
				b.Start.Format(model.DateLayout),
			})
		}
		Table(w, []string{"WEEK", "COMPLETED", "WEEK OF"}, rows)
	}

	if include("accuracy") {
		section("TIME ACCURACY")
		rows := make([][]string, 0)
		for _, r := range derive.Accuracy(d.Items) {
			rows = append(rows, []string{
				r.Type,
				Hours(r.Estimated),
				Hours(r.Actual),
				fmt.Sprintf("%s%%", r.VarianceText),
				fmt.Sprintf("%d", r.Count),
			})
		}
		if len(rows) == 0 {
			fmt.Fprintln(w, "No completed items with both an estimate and recorded actual hours yet.")
		} else {
			Table(w, []string{"TYPE", "ESTIMATED", "ACTUAL", "VARIANCE", "ITEMS"}, rows)
		}
	}

	if include("projects") {
		section("PROJECT BREAKDOWN (last 30 days)")
		rows := make([][]string, 0)
		for _, r := range derive.ProjectBreakdown(d.Items, d.Projects, d.Now, 30) {
			rows = append(rows, []string{Truncate(r.Title, 32), Hours(r.Hours), fmt.Sprintf("%d", r.Tasks)})
		}
		if len(rows) == 0 {
			fmt.Fprintln(w, "No completed project work in the last 30 days.")
		} else {
			Table(w, []string{"PROJECT", "HOURS", "ITEMS"}, rows)
		}
	}

	if include("calibration") {
		section(fmt.Sprintf("AI CALIBRATION (last %d weeks)", weeks))
		rows := make([][]string, 0)
		for _, r := range derive.Calibration(d.Items, weeks) {
			rows = append(rows, []string{
				r.Week,
				fmt.Sprintf("%.0f", r.AvgPriority),
				fmt.Sprintf("%.1f", r.AvgDifficulty),
				fmt.Sprintf("%d", r.Count),
			})
		}
		if len(rows) == 0 {
			fmt.Fprintln(w, "No analyzed items yet.")
		} else {
			Table(w, []string{"WEEK", "AVG PRIORITY", "AVG DIFFICULTY", "ITEMS"}, rows)
		}
	}

	if include("insights") {
		section("INSIGHTS")
		ins := derive.BuildInsights(d.Items)
		if ins.Samples < 3 {
			fmt.Fprintf(w, "Need at least 3 completed items with recorded hours (have %d).\n", ins.Samples)
		} else {
			switch {
			case ins.AvgVariance > 0:
				fmt.Fprintf(w, "You underestimate time by %.0f%% on average.\n", ins.AvgVariance)
			case ins.AvgVariance < 0:
				fmt.Fprintf(w, "You overestimate time by %.0f%% on average.\n", -ins.AvgVariance)
			default:
				fmt.Fprintln(w, "Your time estimates are remarkably accurate.")
			}
			for _, t := range ins.Types {
				verb := "on target"
				switch {
				case t.AvgVariance > 0:
					verb = fmt.Sprintf("underestimates by %.0f%%", t.AvgVariance)
				case t.AvgVariance < 0:
					verb = fmt.Sprintf("overestimates by %.0f%%", -t.AvgVariance)
				}
				fmt.Fprintf(w, "  %-10s %s (%d items)\n", t.Type, verb, t.Count)
			}
		}
	}
}

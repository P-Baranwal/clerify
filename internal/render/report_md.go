package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/P-Baranwal/clerify/internal/derive"
	"github.com/P-Baranwal/clerify/internal/model"
)

// ReportMarkdown writes the requested report sections as GitHub-flavored
// Markdown.
func ReportMarkdown(w io.Writer, d ReportData, sections []string, weeks int) {
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
	heading := func(title string) { fmt.Fprintf(w, "## %s\n\n", title) }
	table := func(headers []string, rows [][]string) {
		fmt.Fprint(w, "| ")
		for i, h := range headers {
			if i > 0 {
				fmt.Fprint(w, " | ")
			}
			fmt.Fprint(w, h)
		}
		fmt.Fprintln(w, " |")
		fmt.Fprint(w, "|")
		for range headers {
			fmt.Fprint(w, " --- |")
		}
		fmt.Fprintln(w)
		for _, row := range rows {
			fmt.Fprint(w, "| ")
			for i := range headers {
				if i > 0 {
					fmt.Fprint(w, " | ")
				}
				value := ""
				if i < len(row) {
					value = mdCell(row[i])
				}
				fmt.Fprint(w, value)
			}
			fmt.Fprintln(w, " |")
		}
		fmt.Fprintln(w)
	}

	if include("summary") {
		heading("SUMMARY")
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
		fmt.Fprintf(w, "- Total items: %d\n", len(d.Items))
		fmt.Fprintf(w, "- Completed: %d\n", completed)
		fmt.Fprintf(w, "- Completion rate: %.1f%%\n", rate)
		fmt.Fprintf(w, "- Current streak: %d days\n\n", derive.Streak(d.Items, d.Now))
	}

	if include("velocity") {
		heading(fmt.Sprintf("VELOCITY (last %d weeks)", weeks))
		rows := make([][]string, 0, weeks)
		for _, b := range derive.Velocity(d.Items, d.Now, weeks) {
			rows = append(rows, []string{b.Label, fmt.Sprintf("%d", b.Count), b.Start.Format(model.DateLayout)})
		}
		table([]string{"WEEK", "COMPLETED", "WEEK OF"}, rows)
	}

	if include("accuracy") {
		heading("TIME ACCURACY")
		rows := make([][]string, 0)
		for _, r := range derive.Accuracy(d.Items) {
			rows = append(rows, []string{
				r.Type, Hours(r.Estimated), Hours(r.Actual),
				fmt.Sprintf("%s%%", r.VarianceText), fmt.Sprintf("%d", r.Count),
			})
		}
		table([]string{"TYPE", "ESTIMATED", "ACTUAL", "VARIANCE", "ITEMS"}, rows)
	}

	if include("projects") {
		heading("PROJECT BREAKDOWN (last 30 days)")
		rows := make([][]string, 0)
		for _, r := range derive.ProjectBreakdown(d.Items, d.Projects, d.Now, 30) {
			rows = append(rows, []string{Truncate(r.Title, 32), Hours(r.Hours), fmt.Sprintf("%d", r.Tasks)})
		}
		table([]string{"PROJECT", "HOURS", "ITEMS"}, rows)
	}

	if include("calibration") {
		heading(fmt.Sprintf("AI CALIBRATION (last %d weeks)", weeks))
		rows := make([][]string, 0)
		for _, r := range derive.Calibration(d.Items, weeks) {
			rows = append(rows, []string{
				r.Week, fmt.Sprintf("%.0f", r.AvgPriority),
				fmt.Sprintf("%.1f", r.AvgDifficulty), fmt.Sprintf("%d", r.Count),
			})
		}
		table([]string{"WEEK", "AVG PRIORITY", "AVG DIFFICULTY", "ITEMS"}, rows)
	}

	if include("insights") {
		heading("INSIGHTS")
		ins := derive.BuildInsights(d.Items)
		if ins.Samples < 3 {
			fmt.Fprintf(w, "Need at least 3 completed items with recorded hours (have %d).\n", ins.Samples)
			return
		}
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
			fmt.Fprintf(w, "- %s %s (%d items)\n", t.Type, verb, t.Count)
		}
	}
}

func mdCell(s string) string {
	return strings.ReplaceAll(s, "|", `\|`)
}

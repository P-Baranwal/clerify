package render

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/P-Baranwal/clerify/internal/model"
)

// ProjectNamer resolves a project id for display.
type ProjectNamer func(id string) string

// ItemTable writes the standard listing.
func ItemTable(w io.Writer, items []model.Item, namer ProjectNamer, now time.Time) {
	headers := []string{"ID", "TITLE", "DUE", "URG", "STATUS", "PRIO", "EST", "IMP", "PROJECT"}
	rows := make([][]string, 0, len(items))
	boosted := false

	for _, it := range items {
		if it.Boost.Active && it.Boost.BoostedPriorityScore != nil {
			boosted = true
		}
		project := "—"
		if it.HasProject() {
			if namer != nil {
				project = namer(*it.ProjectID)
			} else {
				project = *it.ProjectID
			}
		}
		impact := "—"
		if it.ImpactScore != nil {
			impact = fmt.Sprintf("%d", *it.ImpactScore)
		}
		due := "—"
		if it.Deadline != nil && *it.Deadline != "" {
			due = *it.Deadline
		}
		score := ScoreCell(it)
		if strings.Contains(score, "*") {
			score = colorize(score, "36")
		}
		rows = append(rows, []string{
			ShortID(it.ID),
			Truncate(it.Title, 44),
			due,
			colorUrgency(UrgencyCell(it, now)),
			colorStatus(StatusCell(it)),
			score,
			Hours(it.EstimatedHours),
			impact,
			Truncate(project, 18),
		})
	}
	Table(w, headers, rows)
	if boosted {
		fmt.Fprintln(w, "* boosted priority score")
	}
}

// ItemDetail writes the full record for `show`.
func ItemDetail(w io.Writer, it model.Item, namer ProjectNamer, now time.Time) {
	field := func(label, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		fmt.Fprintf(w, "%-16s %s\n", label, value)
	}

	fmt.Fprintf(w, "%s\n", it.Title)
	fmt.Fprintf(w, "%s\n\n", strings.Repeat("─", Width(it.Title)))

	field("id", it.ID)
	field("type", it.Type)
	field("status", it.Status)
	if it.Deadline != nil {
		field("deadline", *it.Deadline+" ("+UrgencyCell(it, now)+")")
	}
	if it.HasProject() {
		title := *it.ProjectID
		if namer != nil {
			title = namer(*it.ProjectID)
		}
		field("project", title)
	}
	field("priority", ScoreCell(it))
	if it.Boost.Active {
		reason := ""
		if it.Boost.Reason != nil {
			reason = *it.Boost.Reason
		}
		field("boost", fmt.Sprintf("%d (%s)", derefInt(it.Boost.BoostedPriorityScore), reason))
	}
	field("difficulty", fmt.Sprintf("%d/10  %s", it.Difficulty, it.DifficultyReasoning))
	field("estimate", fmt.Sprintf("%s  %s", Hours(it.EstimatedHours), it.EstimatedHoursReasoning))
	if it.ActualHours > 0 {
		field("actual", Hours(it.ActualHours))
	}
	if it.ImpactScore != nil {
		field("impact", fmt.Sprintf("%d/10", *it.ImpactScore))
	}
	if it.BlockerNote != nil {
		field("blocker", *it.BlockerNote)
	}
	if len(it.Checklist) > 0 {
		done := 0
		for _, c := range it.Checklist {
			if c.Done {
				done++
			}
		}
		field("checklist", fmt.Sprintf("%d/%d", done, len(it.Checklist)))
		for _, c := range it.Checklist {
			mark := "[ ]"
			if c.Done {
				mark = "[x]"
			}
			fmt.Fprintf(w, "  %s %s\n", mark, c.Text)
		}
	}
	if it.Notes != "" {
		fmt.Fprintf(w, "\nnotes\n  %s\n", it.Notes)
	}
	if it.Description != "" {
		fmt.Fprintf(w, "\ndescription\n  %s\n", it.Description)
	}
	if it.PriorityReasoning != "" {
		fmt.Fprintf(w, "\nwhy\n  %s\n", it.PriorityReasoning)
	}
	field("created", FormatTimestamp(it.CreatedAt))
	field("updated", FormatTimestamp(it.UpdatedAt))
	if it.AnalyzedAt != nil {
		field("analyzed", FormatTimestamp(*it.AnalyzedAt))
	}
}

// ProjectTable writes the project listing.
func ProjectTable(w io.Writer, projects []model.Project, counts map[string]int) {
	headers := []string{"ID", "TITLE", "STATUS", "ITEMS", "CREATED"}
	rows := make([][]string, 0, len(projects))
	for _, p := range projects {
		rows = append(rows, []string{
			ShortID(p.ID),
			Truncate(p.Title, 44),
			p.Status,
			fmt.Sprintf("%d", counts[p.ID]),
			FormatTimestamp(p.CreatedAt),
		})
	}
	Table(w, headers, rows)
}

func derefInt(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

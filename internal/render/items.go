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
	itemTable(w, items, namer, now, nil)
}

// ItemTableNumbered writes the standard listing with a stable reference number
// in the first column. numbers maps an item id to a number that callers can
// resolve later with `clerify <command> #N`.
func ItemTableNumbered(w io.Writer, items []model.Item, namer ProjectNamer, now time.Time, numbers map[string]int) {
	itemTable(w, items, namer, now, numbers)
}

func itemTable(w io.Writer, items []model.Item, namer ProjectNamer, now time.Time, numbers map[string]int) {
	hasImpact := false
	hasProject := false
	for _, it := range items {
		if it.ImpactScore != nil {
			hasImpact = true
		}
		if it.HasProject() {
			hasProject = true
		}
	}

	headers := make([]string, 0, 9)
	if numbers != nil {
		headers = append(headers, "#")
	}
	headers = append(headers, "ID", "TITLE", "DUE IN", "URG", "STATUS", "PRIORITY", "ESTIMATE")
	if hasImpact {
		headers = append(headers, "IMPACT")
	}
	if hasProject {
		headers = append(headers, "PROJECT")
	}

	rows := make([][]string, 0, len(items))
	boosted := false

	for _, it := range items {
		if it.Boost.Active && it.Boost.BoostedPriorityScore != nil {
			boosted = true
		}
		row := make([]string, 0, len(headers))
		if numbers != nil {
			if n, ok := numbers[it.ID]; ok {
				row = append(row, fmt.Sprintf("#%d", n))
			} else {
				row = append(row, "—")
			}
		}

		due := "—"
		if it.Deadline != nil && *it.Deadline != "" {
			due = *it.Deadline
		}
		priority := PriorityLabel(it.EffectiveScore())
		if it.Boost.Active && it.Boost.BoostedPriorityScore != nil {
			priority += "*"
		}
		row = append(row,
			ShortID(it.ID),
			Truncate(it.Title, 44),
			due,
			colorUrgency(UrgencyCell(it, now)),
			colorStatus(StatusCell(it)),
			priority,
			Hours(it.EstimatedHours),
		)
		if hasImpact {
			impact := "—"
			if it.ImpactScore != nil {
				impact = fmt.Sprintf("%d", *it.ImpactScore)
			}
			row = append(row, impact)
		}
		if hasProject {
			project := "—"
			if it.HasProject() {
				if namer != nil {
					project = namer(*it.ProjectID)
				} else {
					project = *it.ProjectID
				}
			}
			row = append(row, Truncate(project, 18))
		}
		rows = append(rows, row)
	}
	Table(w, headers, rows)
	if boosted {
		fmt.Fprintln(w, "* boosted priority: temporarily raised above the normal score")
	}
}

// PriorityLabel turns the 0-100 score into a word most people can act on.
func PriorityLabel(score int) string {
	switch {
	case score >= 70:
		return "High"
	case score >= 40:
		return "Medium"
	default:
		return "Low"
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

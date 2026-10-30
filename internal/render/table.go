package render

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/P-Baranwal/clerify/internal/format"
	"github.com/P-Baranwal/clerify/internal/model"
)

// Table writes an aligned table with a dashed header rule. Widths are
// measured in terminal columns, not bytes, so non-ASCII titles stay aligned.
func Table(w io.Writer, headers []string, rows [][]string) {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = Width(h)
	}
	for _, row := range rows {
		for i := range headers {
			if i < len(row) {
				if n := Width(row[i]); n > widths[i] {
					widths[i] = n
				}
			}
		}
	}

	separator := "  "
	ruleChar := "-"
	switch current.TableStyle {
	case "ascii":
		separator = " | "
	case "unicode":
		separator = " │ "
		ruleChar = "─"
	}

	writeRow := func(cells []string) {
		for i := range headers {
			if i > 0 {
				fmt.Fprint(w, separator)
			}
			value := ""
			if i < len(cells) {
				value = cells[i]
			}
			fmt.Fprint(w, PadRight(value, widths[i]))
		}
		fmt.Fprintln(w)
	}

	writeRow(headers)
	rule := make([]string, len(headers))
	for i := range rule {
		rule[i] = strings.Repeat(ruleChar, widths[i])
	}
	writeRow(rule)
	for _, row := range rows {
		writeRow(row)
	}
}

// ShortID renders the six-character id prefix used in listings.
func ShortID(id string) string {
	if len(id) <= 6 {
		return id
	}
	return id[:6]
}

// Truncate shortens s to n runes, adding an ellipsis when it was cut.
func Truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 1 {
		return string(runes[:n])
	}
	return string(runes[:n-1]) + "…"
}

// Hours renders a duration without trailing zeros.
func Hours(v float64) string { return format.Hours(v) }

// UrgencyCell renders the compact urgency column.
func UrgencyCell(it model.Item, now time.Time) string {
	u := model.Urgency(deadlineOf(it), now)
	switch u {
	case "Overdue":
		return "overdue"
	case "Due Today":
		return "today"
	case "Due Tomorrow":
		return "tmrw"
	case "This Week":
		return "soon"
	}
	if days, ok := model.DaysUntil(deadlineOf(it), now); ok {
		return fmt.Sprintf("in %dd", days)
	}
	return "—"
}

// StatusCell is the short status label used in tables.
func StatusCell(it model.Item) string {
	switch model.NormalizeStatus(it.Status) {
	case model.StatusDone:
		return "done"
	case model.StatusInProgress:
		return "doing"
	case model.StatusBlocked:
		return "blocked"
	default:
		return "todo"
	}
}

// ScoreCell marks a boosted score with an asterisk.
func ScoreCell(it model.Item) string {
	if it.Boost.Active && it.Boost.BoostedPriorityScore != nil {
		return fmt.Sprintf("%d*", *it.Boost.BoostedPriorityScore)
	}
	return fmt.Sprintf("%d", it.PriorityScore)
}

func deadlineOf(it model.Item) string {
	if it.Deadline == nil {
		return ""
	}
	return *it.Deadline
}

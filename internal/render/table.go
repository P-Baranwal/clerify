package render

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/P-Baranwal/clerify/internal/model"
)

// Table writes an aligned table with a dashed header rule.
func Table(w io.Writer, headers []string, rows [][]string) {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i := range headers {
			if i < len(row) && len(row[i]) > widths[i] {
				widths[i] = len(row[i])
			}
		}
	}

	rule := make([]string, len(headers))
	for i := range rule {
		rule[i] = strings.Repeat("-", widths[i])
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	fmt.Fprintln(tw, strings.Join(rule, "\t"))
	for _, row := range rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	_ = tw.Flush()
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
func Hours(v float64) string {
	s := fmt.Sprintf("%.2f", v)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s + "h"
}

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

package render

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/P-Baranwal/clerify/internal/derive"
	"github.com/P-Baranwal/clerify/internal/model"
)

const (
	calendarCellWidth = 18
	calendarCellLines = 4
)

// CalendarMonth renders an ASCII month grid.
func CalendarMonth(w io.Writer, year int, month time.Month, leading int, days []derive.CalendarDay) {
	title := fmt.Sprintf("%s %d", month.String(), year)
	fmt.Fprintln(w, title)
	fmt.Fprintln(w, strings.Repeat("─", len(title)))
	fmt.Fprintln(w, weekdayHeader(calendarCellWidth))
	fmt.Fprintln(w, strings.Repeat("─", 7*calendarCellWidth))

	cells := make([][]string, 0, leading+len(days))
	for i := 0; i < leading; i++ {
		cells = append(cells, blankCell())
	}
	for _, day := range days {
		cells = append(cells, dayCell(day))
	}

	for row := 0; row*7 < len(cells); row++ {
		for line := 0; line < calendarCellLines; line++ {
			var b strings.Builder
			for col := 0; col < 7; col++ {
				idx := row*7 + col
				if idx < len(cells) {
					b.WriteString(cells[idx][line])
				} else {
					b.WriteString(strings.Repeat(" ", calendarCellWidth))
				}
			}
			fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
		}
	}
}

func weekdayHeader(width int) string {
	names := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	var b strings.Builder
	for _, n := range names {
		b.WriteString(padRight(n, width))
	}
	return strings.TrimRight(b.String(), " ")
}

func blankCell() []string {
	out := make([]string, calendarCellLines)
	for i := range out {
		out[i] = strings.Repeat(" ", calendarCellWidth)
	}
	return out
}

func dayCell(day derive.CalendarDay) []string {
	label := fmt.Sprintf("%2d", day.Date)
	if day.Today {
		label += "*"
	}
	lines := []string{padRight(label, calendarCellWidth)}

	const maxChips = 2
	for i, it := range day.Items {
		if i >= maxChips {
			lines = append(lines, padRight(fmt.Sprintf("  +%d more", len(day.Items)-maxChips), calendarCellWidth))
			break
		}
		mark := ""
		if it.IsDone() {
			mark = "✓"
		}
		lines = append(lines, padRight("  "+Truncate(it.Title, calendarCellWidth-5)+mark, calendarCellWidth))
	}

	for len(lines) < calendarCellLines {
		lines = append(lines, strings.Repeat(" ", calendarCellWidth))
	}
	return lines[:calendarCellLines]
}

func padRight(s string, width int) string {
	n := len([]rune(s))
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}

// PlanView renders the weekly plan.
func PlanView(w io.Writer, plan *model.Plan, availableHours int) {
	if plan == nil || len(plan.Days) == 0 {
		fmt.Fprintln(w, "No plan cached. Run `clerify plan --refresh`.")
		return
	}

	total := 0.0
	for _, d := range plan.Days {
		total += d.TotalHours
	}
	fmt.Fprintf(w, "Week plan (%d days, %s total, %dh/day available)\n", len(plan.Days), Hours(total), availableHours)
	fmt.Fprintf(w, "generated %s\n\n", plan.GeneratedAt)

	for _, d := range plan.Days {
		header := d.Day
		if d.Date != nil && *d.Date != "" {
			header += "  " + *d.Date
		}
		load := Hours(d.TotalHours)
		if availableHours > 0 && d.TotalHours > float64(availableHours) {
			load += "  OVERLOADED"
		}
		fmt.Fprintf(w, "%s  [%s]\n", header, load)
		if len(d.Blocks) == 0 {
			fmt.Fprintln(w, "  (nothing scheduled)")
		}
		for _, b := range d.Blocks {
			fmt.Fprintf(w, "  %s %-9s %s  (%s)\n", slotIcon(b.Slot), b.Slot, b.Task, Hours(b.Hours))
		}
		if d.Note != "" {
			fmt.Fprintf(w, "  note: %s\n", d.Note)
		}
		fmt.Fprintln(w)
	}
}

func slotIcon(slot string) string {
	lower := strings.ToLower(slot)
	switch {
	case strings.Contains(lower, "morning"):
		return "🌅"
	case strings.Contains(lower, "afternoon"):
		return "☀️"
	case strings.Contains(lower, "evening"):
		return "🌙"
	default:
		return "⏰"
	}
}

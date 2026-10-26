package derive

import (
	"sort"
	"time"

	"github.com/P-Baranwal/clerify/internal/model"
)

// CalendarDay is one cell of the month grid.
type CalendarDay struct {
	Date  int
	Today bool
	Items []model.Item
}

// MonthGrid lays out a month, returning the number of leading blank cells and
// one entry per day. Items are ordered priority-first, then recently completed,
// matching Calendar.svelte.
func MonthGrid(items []model.Item, year int, month time.Month, now time.Time) (leadingBlanks int, days []CalendarDay) {
	first := time.Date(year, month, 1, 0, 0, 0, 0, time.Local)
	leadingBlanks = int(first.Weekday())

	daysInMonth := time.Date(year, month+1, 0, 0, 0, 0, 0, time.Local).Day()
	today := model.StartOfDay(now)

	days = make([]CalendarDay, 0, daysInMonth)
	for d := 1; d <= daysInMonth; d++ {
		cell := CalendarDay{
			Date:  d,
			Today: today.Equal(first.AddDate(0, 0, d-1)),
		}
		for _, it := range items {
			if it.Deadline == nil {
				continue
			}
			t, ok := model.ParseDeadline(*it.Deadline)
			if !ok {
				continue
			}
			if t.Year() == year && t.Month() == month && t.Day() == d {
				cell.Items = append(cell.Items, it)
			}
		}
		sort.SliceStable(cell.Items, func(a, b int) bool {
			da, db := cell.Items[a].IsDone(), cell.Items[b].IsDone()
			if da != db {
				return !da
			}
			if da {
				return timeValue(cell.Items[a].UpdatedAt).After(timeValue(cell.Items[b].UpdatedAt))
			}
			return cell.Items[a].EffectiveScore() > cell.Items[b].EffectiveScore()
		})
		days = append(days, cell)
	}
	return leadingBlanks, days
}

// Undated returns items with no deadline, priority-first.
func Undated(items []model.Item) []model.Item {
	out := make([]model.Item, 0)
	for _, it := range items {
		if it.Deadline == nil || *it.Deadline == "" {
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		return out[a].EffectiveScore() > out[b].EffectiveScore()
	})
	return out
}

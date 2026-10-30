package derive

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/P-Baranwal/clerify/internal/model"
)

// VelocityBucket is one week's completion count.
type VelocityBucket struct {
	Label string
	Count int
	Start time.Time
	End   time.Time
}

// Velocity returns the last n Sunday-start week buckets, oldest first.
func Velocity(items []model.Item, now time.Time, weeks int) []VelocityBucket {
	if weeks <= 0 {
		weeks = 8
	}
	out := make([]VelocityBucket, 0, weeks)
	for i := weeks - 1; i >= 0; i-- {
		offset := i*7 + int(now.Weekday())
		start := model.StartOfDay(now.AddDate(0, 0, -offset))
		end := start.AddDate(0, 0, 6).Add(23*time.Hour + 59*time.Minute + 59*time.Second + 999*time.Millisecond)
		count := 0
		for _, it := range items {
			if !it.IsDone() {
				continue
			}
			t := it.CompletionTime()
			if !t.IsZero() && !t.Before(start) && !t.After(end) {
				count++
			}
		}
		out = append(out, VelocityBucket{
			Label: fmt.Sprintf("Week %d", weeks-i),
			Count: count,
			Start: start,
			End:   end,
		})
	}
	return out
}

// AccuracyRow aggregates estimate-vs-actual effort for one item type.
type AccuracyRow struct {
	Type         string
	Estimated    float64
	Actual       float64
	Variance     float64
	VarianceText string
	Count        int
}

// Accuracy groups completed items with a known estimate by type, preserving
// first-appearance order like Object.entries in the web app.
func Accuracy(items []model.Item) []AccuracyRow {
	order := make([]string, 0)
	agg := make(map[string]*AccuracyRow)

	for _, it := range items {
		if !it.IsDone() || it.EstimatedHours <= 0 || it.ActualHours <= 0 {
			continue
		}
		kind := it.Type
		if kind == "" {
			kind = model.TypeOther
		}
		row, ok := agg[kind]
		if !ok {
			row = &AccuracyRow{Type: kind}
			agg[kind] = row
			order = append(order, kind)
		}
		row.Estimated += it.EstimatedHours
		row.Actual += it.ActualHours
		row.Count++
	}

	out := make([]AccuracyRow, 0, len(order))
	for _, kind := range order {
		row := *agg[kind]
		if row.Estimated > 0 {
			row.Variance = (row.Actual - row.Estimated) / row.Estimated * 100
		}
		row.VarianceText = fmt.Sprintf("%.1f", row.Variance)
		out = append(out, row)
	}
	return out
}

// ProjectRow is one project's completed effort in the reporting window.
type ProjectRow struct {
	ProjectID string
	Title     string
	Hours     float64
	Tasks     int
}

// ProjectBreakdown sums completed effort per project over the last n days.
func ProjectBreakdown(items []model.Item, projects []model.Project, now time.Time, days int) []ProjectRow {
	if days <= 0 {
		days = 30
	}
	cutoff := now.AddDate(0, 0, -days)

	order := make([]string, 0)
	agg := make(map[string]*ProjectRow)

	for _, it := range items {
		if !it.IsDone() {
			continue
		}
		t := it.CompletionTime()
		if t.IsZero() || t.Before(cutoff) {
			continue
		}
		key := "unassigned"
		title := "No Project"
		if it.HasProject() {
			key = *it.ProjectID
			title = "Unknown Project"
			for _, p := range projects {
				if p.ID == key {
					title = p.Title
					break
				}
			}
		}
		row, ok := agg[key]
		if !ok {
			row = &ProjectRow{ProjectID: key, Title: title}
			agg[key] = row
			order = append(order, key)
		}
		hours := it.ActualHours
		if hours == 0 {
			hours = it.EstimatedHours
		}
		row.Hours += hours
		row.Tasks++
	}

	out := make([]ProjectRow, 0, len(order))
	for _, key := range order {
		out = append(out, *agg[key])
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Hours != out[b].Hours {
			return out[a].Hours > out[b].Hours
		}
		return out[a].Title < out[b].Title
	})
	return out
}

// Streak counts consecutive days ending today with at least one completion.
func Streak(items []model.Item, now time.Time) int {
	days := make(map[string]bool)
	for _, it := range items {
		if !it.IsDone() {
			continue
		}
		t := it.CompletionTime()
		if t.IsZero() {
			continue
		}
		days[t.In(time.Local).Format(model.DateLayout)] = true
	}

	streak := 0
	cursor := model.StartOfDay(now)
	for {
		if !days[cursor.Format(model.DateLayout)] {
			break
		}
		streak++
		if streak > len(days) {
			break
		}
		cursor = cursor.AddDate(0, 0, -1)
	}
	return streak
}

// CalibrationRow is one week of AI score averages.
type CalibrationRow struct {
	Week          string
	AvgPriority   float64
	AvgDifficulty float64
	Count         int
}

// Calibration groups analyzed items by Sunday-start week and averages their
// priority and difficulty. Only the last n weeks are returned.
func Calibration(items []model.Item, weeks int) []CalibrationRow {
	order := make([]string, 0)
	agg := make(map[string]*CalibrationRow)

	for _, it := range items {
		if it.AnalyzedAt == nil {
			continue
		}
		t := timeValue(*it.AnalyzedAt)
		if t.IsZero() {
			continue
		}
		start := model.StartOfDay(t).AddDate(0, 0, -int(t.Weekday()))
		key := start.Format(model.DateLayout)
		row, ok := agg[key]
		if !ok {
			row = &CalibrationRow{Week: "Week of " + key}
			agg[key] = row
			order = append(order, key)
		}
		row.AvgPriority += float64(it.PriorityScore)
		row.AvgDifficulty += float64(it.Difficulty)
		row.Count++
	}

	sort.Strings(order)
	out := make([]CalibrationRow, 0, len(order))
	for _, key := range order {
		row := *agg[key]
		if row.Count > 0 {
			row.AvgPriority /= float64(row.Count)
			row.AvgDifficulty /= float64(row.Count)
		}
		out = append(out, row)
	}
	if weeks > 0 && len(out) > weeks {
		out = out[len(out)-weeks:]
	}
	return out
}

// TypeInsight is one type's average estimate error.
type TypeInsight struct {
	Type        string
	Count       int
	AvgVariance float64
}

// Insights summarises estimate accuracy once at least three items have both an
// estimate and a recorded actual duration.
type Insights struct {
	Samples     int
	AvgVariance float64
	Types       []TypeInsight
}

// BuildInsights mirrors the AI Insights block in Settings.svelte.
func BuildInsights(items []model.Item) Insights {
	totalVariance := 0.0
	samples := 0
	typeVar := make(map[string]float64)
	typeCount := make(map[string]int)
	typeOrder := make([]string, 0)

	for _, it := range items {
		if !it.IsDone() || it.EstimatedHours <= 0 || it.ActualHours <= 0 {
			continue
		}
		variance := (it.ActualHours - it.EstimatedHours) / it.EstimatedHours * 100
		totalVariance += variance
		samples++

		kind := it.Type
		if kind == "" {
			kind = model.TypeOther
		}
		if _, ok := typeCount[kind]; !ok {
			typeOrder = append(typeOrder, kind)
		}
		typeVar[kind] += variance
		typeCount[kind]++
	}

	out := Insights{Samples: samples}
	if samples == 0 {
		return out
	}
	out.AvgVariance = totalVariance / float64(samples)
	for _, kind := range typeOrder {
		if typeCount[kind] < 2 {
			continue
		}
		out.Types = append(out.Types, TypeInsight{
			Type:        kind,
			Count:       typeCount[kind],
			AvgVariance: typeVar[kind] / float64(typeCount[kind]),
		})
	}
	sort.SliceStable(out.Types, func(a, b int) bool {
		return math.Abs(out.Types[a].AvgVariance) > math.Abs(out.Types[b].AvgVariance)
	})
	return out
}

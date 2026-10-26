package derive

import (
	"github.com/P-Baranwal/clerify/internal/model"
	"time"
)

// RiskReport mirrors the RiskBanner calculation.
type RiskReport struct {
	TotalHours     float64
	TaskCount      int
	AvailableHours float64
	Overloaded     bool
	Ratio          float64
	Urgent         bool
}

// Risk sums estimated effort for non-done items due in the next seven days and
// compares it with the user's weekly capacity.
func Risk(items []model.Item, now time.Time, availableHoursPerDay int) RiskReport {
	if availableHoursPerDay <= 0 {
		availableHoursPerDay = model.DefaultAvailableHours
	}
	rep := RiskReport{AvailableHours: float64(availableHoursPerDay) * 7}

	for _, it := range items {
		if it.IsDone() || it.Deadline == nil {
			continue
		}
		if !model.WithinDays(*it.Deadline, now, 7) {
			continue
		}
		hours := it.EstimatedHours
		if hours == 0 {
			// The oracle uses `estimatedHours || 1`, so an unset value counts
			// as one hour rather than zero.
			hours = 1
		}
		rep.TotalHours += hours
		rep.TaskCount++
	}

	if rep.AvailableHours > 0 {
		rep.Ratio = rep.TotalHours / rep.AvailableHours
	}
	rep.Overloaded = rep.TotalHours > rep.AvailableHours
	rep.Urgent = rep.Ratio > 1.5
	return rep
}

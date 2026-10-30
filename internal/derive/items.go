package derive

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/P-Baranwal/clerify/internal/model"
)

// Sort keys accepted by ls --sort.
const (
	SortPriority = "priority"
	SortDue      = "due"
	SortUpdated  = "updated"
	SortROI      = "roi"
	SortTitle    = "title"
)

// PriorityList returns non-done items ordered by effective score, with
// deterministic tiebreaks (deadline ascending, then updatedAt descending).
func PriorityList(items []model.Item) []model.Item {
	out := make([]model.Item, 0, len(items))
	for _, it := range items {
		if !it.IsDone() {
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		sa, sb := out[a].EffectiveScore(), out[b].EffectiveScore()
		if sa != sb {
			return sa > sb
		}
		if c := compareDeadline(out[a].Deadline, out[b].Deadline); c != 0 {
			return c < 0
		}
		return timeValue(out[a].UpdatedAt).After(timeValue(out[b].UpdatedAt))
	})
	return out
}

// Completed returns done items, most recently updated first.
func Completed(items []model.Item) []model.Item {
	out := make([]model.Item, 0)
	for _, it := range items {
		if it.IsDone() {
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		return out[a].CompletionTime().After(out[b].CompletionTime())
	})
	return out
}

// Filter narrows a list. Zero-value fields do not constrain.
type Filter struct {
	Statuses    []string
	ProjectID   *string
	Type        string
	Overdue     bool
	DueWithin   *int
	Query       string
	IncludeDone bool
	OnlyDone    bool
	Unprojected bool
}

// Apply filters items and sorts them by key (default priority).
func Apply(items []model.Item, f Filter, key string, now time.Time) []model.Item {
	out := make([]model.Item, 0, len(items))
	query := strings.ToLower(strings.TrimSpace(f.Query))

	for _, it := range items {
		if f.OnlyDone {
			if !it.IsDone() {
				continue
			}
		} else if !f.IncludeDone && it.IsDone() {
			continue
		}
		if len(f.Statuses) > 0 {
			matched := false
			for _, s := range f.Statuses {
				if it.Status == model.NormalizeStatus(s) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		if f.ProjectID != nil {
			if it.ProjectID == nil || *it.ProjectID != *f.ProjectID {
				continue
			}
		}
		if f.Unprojected && it.HasProject() {
			continue
		}
		if f.Type != "" && !strings.EqualFold(it.Type, f.Type) {
			continue
		}
		if f.Overdue && !model.IsOverdue(deadlineValue(it.Deadline), now) {
			continue
		}
		if f.DueWithin != nil && !model.WithinDays(deadlineValue(it.Deadline), now, *f.DueWithin) {
			continue
		}
		if query != "" {
			haystack := strings.ToLower(it.Title + " " + it.Description + " " + it.Notes)
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		out = append(out, it)
	}

	SortBy(out, key)
	return out
}

// SortBy orders a slice in place.
func SortBy(items []model.Item, key string) {
	switch key {
	case SortDue:
		sort.SliceStable(items, func(a, b int) bool {
			c := compareDeadline(items[a].Deadline, items[b].Deadline)
			if c != 0 {
				return c < 0
			}
			return items[a].EffectiveScore() > items[b].EffectiveScore()
		})
	case SortUpdated:
		sort.SliceStable(items, func(a, b int) bool {
			return timeValue(items[a].UpdatedAt).After(timeValue(items[b].UpdatedAt))
		})
	case SortROI:
		sort.SliceStable(items, func(a, b int) bool {
			ra, oka := ROIScore(items[a])
			rb, okb := ROIScore(items[b])
			if oka != okb {
				return oka
			}
			if !oka {
				return items[a].EffectiveScore() > items[b].EffectiveScore()
			}
			return ra > rb
		})
	case SortTitle:
		sort.SliceStable(items, func(a, b int) bool {
			return strings.ToLower(items[a].Title) < strings.ToLower(items[b].Title)
		})
	default:
		sort.SliceStable(items, func(a, b int) bool {
			sa, sb := items[a].EffectiveScore(), items[b].EffectiveScore()
			if sa != sb {
				return sa > sb
			}
			if c := compareDeadline(items[a].Deadline, items[b].Deadline); c != 0 {
				return c < 0
			}
			return timeValue(items[a].UpdatedAt).After(timeValue(items[b].UpdatedAt))
		})
	}
}

// ROIScore returns impact divided by effort, matching getRoi().
func ROIScore(it model.Item) (float64, bool) {
	if it.ImpactScore == nil || it.EstimatedHours <= 0 {
		return 0, false
	}
	return float64(*it.ImpactScore) / math.Max(it.EstimatedHours, 0.5), true
}

// HighROI returns the top-N non-done items by ROI.
func HighROI(items []model.Item, limit int) []model.Item {
	out := make([]model.Item, 0)
	for _, it := range items {
		if it.IsDone() {
			continue
		}
		if _, ok := ROIScore(it); ok {
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		ra, _ := ROIScore(out[a])
		rb, _ := ROIScore(out[b])
		return ra > rb
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Inbox returns non-done items with no project.
func Inbox(items []model.Item) []model.Item {
	out := make([]model.Item, 0)
	for _, it := range items {
		if !it.IsDone() && !it.HasProject() {
			out = append(out, it)
		}
	}
	return out
}

// Blocked returns every blocked item.
func Blocked(items []model.Item) []model.Item {
	out := make([]model.Item, 0)
	for _, it := range items {
		if it.Status == model.StatusBlocked {
			out = append(out, it)
		}
	}
	return out
}

// Board is the four-column kanban view.
type Board struct {
	Todo       []model.Item
	InProgress []model.Item
	Blocked    []model.Item
	Done       []model.Item
}

// Kanban groups items by status. The done column is capped at 10, mirroring
// ProDashboard.
func Kanban(items []model.Item) Board {
	var b Board
	for _, it := range items {
		switch model.NormalizeStatus(it.Status) {
		case model.StatusTodo:
			b.Todo = append(b.Todo, it)
		case model.StatusInProgress:
			b.InProgress = append(b.InProgress, it)
		case model.StatusBlocked:
			b.Blocked = append(b.Blocked, it)
		case model.StatusDone:
			b.Done = append(b.Done, it)
		}
	}
	sort.SliceStable(b.Done, func(i, j int) bool {
		return b.Done[i].CompletionTime().After(b.Done[j].CompletionTime())
	})
	if len(b.Done) > 10 {
		b.Done = b.Done[:10]
	}
	return b
}

// MaxUpdatedAt returns the newest updatedAt timestamp in the collection, used
// to detect that a cached weekly plan is stale.
func MaxUpdatedAt(items []model.Item) string {
	newest := time.Time{}
	for _, it := range items {
		if t := timeValue(it.UpdatedAt); t.After(newest) {
			newest = t
		}
	}
	if newest.IsZero() {
		return ""
	}
	return model.NowRFC3339(newest)
}

// ── helpers ──────────────────────────────────────────────────────────────────

func deadlineValue(d *string) string {
	if d == nil {
		return ""
	}
	return *d
}

// compareDeadline orders YYYY-MM-DD strings, pushing nil/empty deadlines last.
func compareDeadline(a, b *string) int {
	av, bv := deadlineValue(a), deadlineValue(b)
	switch {
	case av == "" && bv == "":
		return 0
	case av == "":
		return 1
	case bv == "":
		return -1
	case av < bv:
		return -1
	case av > bv:
		return 1
	default:
		return 0
	}
}

func timeValue(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

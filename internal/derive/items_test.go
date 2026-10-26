package derive

import (
	"testing"
	"time"

	"github.com/P-Baranwal/clerify/internal/model"
)

func item(title string, score int, opts ...func(*model.Item)) model.Item {
	it := model.NewItem(title)
	it.PriorityScore = score
	for _, opt := range opts {
		opt(&it)
	}
	return it
}

func withDeadline(d string) func(*model.Item) {
	return func(it *model.Item) { it.Deadline = &d }
}

func withStatus(s string) func(*model.Item) { return func(it *model.Item) { it.Status = s } }

func withImpact(v int) func(*model.Item) {
	return func(it *model.Item) {
		it.ImpactScore = &v
	}
}

func withHours(h float64) func(*model.Item) { return func(it *model.Item) { it.EstimatedHours = h } }

func withUpdated(t time.Time) func(*model.Item) {
	return func(it *model.Item) { it.UpdatedAt = model.NowRFC3339(t) }
}

func TestPriorityListSortsAndTiebreaks(t *testing.T) {
	items := []model.Item{
		item("low", 10),
		item("high", 90),
		item("mid", 50),
		item("done", 100, withStatus(model.StatusDone)),
		item("soon", 90, withDeadline("2026-10-27")),
	}
	got := PriorityList(items)
	if len(got) != 4 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Title != "soon" {
		t.Errorf("first = %q, want soon (same score, earlier deadline)", got[0].Title)
	}
	if got[1].Title != "high" || got[2].Title != "mid" || got[3].Title != "low" {
		t.Errorf("order = %v", titles(got))
	}
}

func TestROIScoreAndHighROI(t *testing.T) {
	items := []model.Item{
		item("a", 0, withImpact(8), withHours(2)),
		item("b", 0, withImpact(10), withHours(1)),
		item("c", 0, withImpact(3), withHours(0)),
		item("d", 0, withHours(1)),
	}
	if _, ok := ROIScore(items[2]); ok {
		t.Error("zero hours should not produce an ROI")
	}
	if _, ok := ROIScore(items[3]); ok {
		t.Error("missing impact should not produce an ROI")
	}
	got := HighROI(items, 5)
	if len(got) != 2 || got[0].Title != "b" {
		t.Errorf("highROI = %v", titles(got))
	}
}

func TestKanbanBucketsAndDoneCap(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.Local)
	items := []model.Item{
		item("todo", 1),
		item("doing", 1, withStatus(model.StatusInProgress)),
		item("blocked", 1, withStatus(model.StatusBlocked)),
	}
	for i := 0; i < 12; i++ {
		items = append(items, item("done", 1, withStatus(model.StatusDone), withUpdated(now.Add(time.Duration(i)*time.Minute))))
	}

	board := Kanban(items)
	if len(board.Todo) != 1 || len(board.InProgress) != 1 || len(board.Blocked) != 1 {
		t.Errorf("buckets = %d %d %d", len(board.Todo), len(board.InProgress), len(board.Blocked))
	}
	if len(board.Done) != 10 {
		t.Errorf("done = %d, want the 10 cap", len(board.Done))
	}
	if board.Done[0].UpdatedAt <= board.Done[9].UpdatedAt {
		t.Errorf("done should be newest first")
	}
}

func TestInboxExcludesDoneAndProjectItems(t *testing.T) {
	pid := "p1"
	items := []model.Item{
		item("inbox", 1),
		item("project", 1, func(it *model.Item) { it.ProjectID = &pid }),
		item("done inbox", 1, withStatus(model.StatusDone)),
	}
	got := Inbox(items)
	if len(got) != 1 || got[0].Title != "inbox" {
		t.Errorf("inbox = %v", titles(got))
	}
}

func TestRiskRatio(t *testing.T) {
	now := time.Date(2026, 10, 26, 9, 0, 0, 0, time.Local)
	build := func(hours float64) []model.Item {
		return []model.Item{
			item("a", 1, withHours(hours), withDeadline("2026-10-28")),
			item("done", 1, withHours(100), withDeadline("2026-10-27"), withStatus(model.StatusDone)),
			item("later", 1, withHours(100), withDeadline("2026-12-01")),
		}
	}
	rep := Risk(build(30), now, 6) // capacity 42
	if rep.TotalHours != 30 || rep.TaskCount != 1 {
		t.Errorf("totals = %+v", rep)
	}
	if rep.Overloaded || rep.Urgent {
		t.Errorf("30/42 should not be overloaded: %+v", rep)
	}

	rep = Risk(build(70), now, 6)
	if !rep.Overloaded || !rep.Urgent {
		t.Errorf("70/42 should be overloaded and urgent: %+v", rep)
	}

	// estimatedHours of 0 counts as 1, mirroring `estimatedHours || 1`.
	rep = Risk([]model.Item{item("zero", 1, withHours(0), withDeadline("2026-10-27"))}, now, 6)
	if rep.TotalHours != 1 {
		t.Errorf("zero hours should count as 1: %+v", rep)
	}
}

func TestVelocityBuckets(t *testing.T) {
	now := time.Date(2026, 10, 26, 9, 0, 0, 0, time.Local) // Monday
	items := []model.Item{
		item("this week", 1, withStatus(model.StatusDone), withUpdated(now.Add(-time.Hour))),
		item("last week", 1, withStatus(model.StatusDone), withUpdated(now.AddDate(0, 0, -7))),
		item("open", 1, withUpdated(now)),
	}
	buckets := Velocity(items, now, 8)
	if len(buckets) != 8 {
		t.Fatalf("buckets = %d", len(buckets))
	}
	if buckets[7].Count != 1 {
		t.Errorf("newest bucket = %d, want 1", buckets[7].Count)
	}
	if buckets[6].Count != 1 {
		t.Errorf("previous bucket = %d, want 1", buckets[6].Count)
	}
	if buckets[7].Label != "Week 8" {
		t.Errorf("label = %q", buckets[7].Label)
	}
}

func TestAccuracyVariance(t *testing.T) {
	a := item("essay", 1, withStatus(model.StatusDone), withHours(4))
	a.ActualHours = 6
	b := item("no actual", 1, withStatus(model.StatusDone), withHours(2))
	rows := Accuracy([]model.Item{a, b})
	if len(rows) != 1 {
		t.Fatalf("rows = %d", len(rows))
	}
	if rows[0].Estimated != 6 || rows[0].Actual != 8 {
		t.Errorf("totals = %+v", rows[0])
	}
	if rows[0].VarianceText != "33.3" {
		t.Errorf("variance = %q", rows[0].VarianceText)
	}
}

func TestStreakRequiresToday(t *testing.T) {
	now := time.Date(2026, 10, 26, 20, 0, 0, 0, time.Local)
	yesterday := now.AddDate(0, 0, -1)
	items := []model.Item{
		item("y", 1, withStatus(model.StatusDone), withUpdated(yesterday)),
	}
	if got := Streak(items, now); got != 0 {
		t.Errorf("streak = %d, want 0 (no completion today)", got)
	}

	items = append(items, item("t", 1, withStatus(model.StatusDone), withUpdated(now)))
	if got := Streak(items, now); got != 2 {
		t.Errorf("streak = %d, want 2", got)
	}
}

func TestMonthGrid(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.Local)
	items := []model.Item{
		item("nov 2", 9, withDeadline("2026-11-02")),
		item("nov 2 done", 1, withDeadline("2026-11-02"), withStatus(model.StatusDone)),
		item("dec", 5, withDeadline("2026-12-02")),
		item("undated", 3),
	}
	leading, days := MonthGrid(items, 2026, time.November, now)
	if leading != 0 {
		t.Errorf("Nov 1 2026 is a Sunday, leading = %d", leading)
	}
	if len(days) != 30 {
		t.Fatalf("days = %d", len(days))
	}
	if len(days[1].Items) != 2 {
		t.Errorf("Nov 2 items = %d", len(days[1].Items))
	}
	if days[1].Items[0].IsDone() {
		t.Errorf("open items should sort before completed ones")
	}
	if len(Undated(items)) != 1 {
		t.Errorf("undated = %v", titles(Undated(items)))
	}
}

func TestApplyFilters(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.Local)
	pid := "p1"
	items := []model.Item{
		item("Overdue essay", 90, withDeadline("2026-10-20")),
		item("Project task", 50, func(it *model.Item) { it.ProjectID = &pid }),
		item("Done thing", 10, withStatus(model.StatusDone)),
	}

	if got := Apply(items, Filter{Overdue: true}, SortPriority, now); len(got) != 1 {
		t.Errorf("overdue = %v", titles(got))
	}
	if got := Apply(items, Filter{IncludeDone: true}, SortPriority, now); len(got) != 3 {
		t.Errorf("include done = %v", titles(got))
	}
	if got := Apply(items, Filter{OnlyDone: true}, SortPriority, now); len(got) != 1 {
		t.Errorf("only done = %v", titles(got))
	}
	if got := Apply(items, Filter{Query: "essay"}, SortPriority, now); len(got) != 1 {
		t.Errorf("query = %v", titles(got))
	}
	if got := Apply(items, Filter{Unprojected: true}, SortPriority, now); len(got) != 1 {
		t.Errorf("unprojected = %v", titles(got))
	}
}

func titles(items []model.Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Title
	}
	return out
}

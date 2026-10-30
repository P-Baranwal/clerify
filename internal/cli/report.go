package cli

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/P-Baranwal/clerify/internal/atomicfile"
	"github.com/P-Baranwal/clerify/internal/derive"
	"github.com/P-Baranwal/clerify/internal/format"
	"github.com/P-Baranwal/clerify/internal/model"
	"github.com/P-Baranwal/clerify/internal/render"
	"github.com/P-Baranwal/clerify/internal/store"
)

var csvHeader = []string{
	"ID", "Title", "Type", "Status", "Priority Score",
	"Estimated Hours", "Actual Hours", "Deadline", "Created", "Updated",
	"Impact Score", "Difficulty", "Notes", "Description", "Project ID",
	"Checklist", "Analyzed At", "Blocker Note", "Boost Active", "Boost Reason",
	"Boosted Priority Score", "Raw Content",
}

func (a *App) reportCmd() *cobra.Command {
	var (
		asCSV bool
		asMD  bool
		last  int
	)
	cmd := &cobra.Command{
		Use:   "report [section...]",
		Short: "Analytics: pace, estimate accuracy, projects, streaks, calibration, insights",
		Long: `A plain-language report. "Velocity" is your finishing pace, "accuracy"
compares estimates with actual hours, and "calibration" shows whether you tend
to underestimate or overestimate.`,
		Example: `  clerify report
  clerify report velocity accuracy --last 4
  clerify report --md`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			if asCSV && (asMD || a.flags.JSON) {
				return fail(2, errors.New("--csv cannot be combined with --md or --json"))
			}
			if asMD && a.flags.JSON {
				return fail(2, errors.New("--md cannot be combined with --json"))
			}
			if last < 1 {
				return fail(2, errors.New("--last must be at least 1 week"))
			}

			if asCSV {
				return writeCSV(a.Out, a.DB.Items())
			}

			data := render.ReportData{
				Items:                a.DB.Items(),
				Projects:             a.DB.Projects(),
				Now:                  a.now(),
				AvailableHoursPerDay: a.DB.Data.Profile.AvailableHoursPerDay,
			}

			if a.flags.JSON {
				return a.printJSON(buildReportJSON(data, last))
			}
			if asMD {
				render.ReportMarkdown(a.Out, data, args, last)
				return nil
			}
			render.ReportWithWeeks(a.Out, data, args, last)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asCSV, "csv", false, "emit the item CSV instead of the report")
	cmd.Flags().BoolVar(&asMD, "md", false, "emit Markdown instead of the plain report")
	cmd.Flags().IntVar(&last, "last", 8, "weeks of history")
	return cmd
}

func buildReportJSON(d render.ReportData, last int) map[string]any {
	completed := 0
	for _, it := range d.Items {
		if it.IsDone() {
			completed++
		}
	}
	return map[string]any{
		"generatedAt": model.NowRFC3339(d.Now),
		"summary": map[string]any{
			"total":      len(d.Items),
			"completed":  completed,
			"streakDays": derive.Streak(d.Items, d.Now),
		},
		"velocity":    derive.Velocity(d.Items, d.Now, last),
		"accuracy":    derive.Accuracy(d.Items),
		"projects":    derive.ProjectBreakdown(d.Items, d.Projects, d.Now, 30),
		"calibration": derive.Calibration(d.Items, last),
		"insights":    derive.BuildInsights(d.Items),
		"risk":        derive.Risk(d.Items, d.Now, d.AvailableHoursPerDay),
	}
}

type digestOutput struct {
	Date     string                  `json:"date"`
	Overdue  int                     `json:"overdue"`
	DueToday int                     `json:"dueToday"`
	Open     int                     `json:"open"`
	Risk     derive.RiskReport       `json:"risk"`
	Top      []model.Item            `json:"top,omitempty"`
	Velocity []derive.VelocityBucket `json:"velocity,omitempty"`
}

func (a *App) digestCmd() *cobra.Command {
	var week bool
	cmd := &cobra.Command{
		Use:     "digest",
		Short:   "One-screen summary, suitable for cron",
		Example: "  clerify digest\n  clerify digest --week",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			now := a.now()
			out := digestOutput{Date: now.Format(model.DateLayout)}

			for _, it := range a.DB.Items() {
				if it.IsDone() || it.Deadline == nil {
					continue
				}
				if model.IsOverdue(*it.Deadline, now) {
					out.Overdue++
				} else if days, ok := model.DaysUntil(*it.Deadline, now); ok && days == 0 {
					out.DueToday++
				}
			}
			top := derive.PriorityList(a.DB.Items())
			out.Open = len(top)
			if len(top) > 5 {
				top = top[:5]
			}
			out.Top = top
			out.Risk = derive.Risk(a.DB.Items(), now, a.DB.Data.Profile.AvailableHoursPerDay)
			if week {
				out.Velocity = derive.Velocity(a.DB.Items(), now, 8)
			}

			if a.flags.JSON {
				return a.printJSON(out)
			}

			a.printf("clerify digest  %s\n\n", out.Date)
			a.printf("Overdue: %d   Due today: %d   Open: %d\n", out.Overdue, out.DueToday, out.Open)
			if out.Risk.Overloaded {
				a.printf("Overload: %sh due in 7d vs %sh capacity (%.2fx)\n",
					format.Hours(out.Risk.TotalHours), format.Hours(out.Risk.AvailableHours), out.Risk.Ratio)
			}
			if len(out.Top) > 0 {
				a.printf("\nTop priority:\n")
				for i, it := range out.Top {
					due := "—"
					if it.Deadline != nil {
						due = *it.Deadline
					}
					a.printf("  %d. %s  (%d, due %s)\n", i+1, it.Title, it.EffectiveScore(), due)
				}
			}
			if week {
				a.printf("\n")
				render.Report(a.Out, render.ReportData{
					Items:                a.DB.Items(),
					Projects:             a.DB.Projects(),
					Now:                  now,
					AvailableHoursPerDay: a.DB.Data.Profile.AvailableHoursPerDay,
				}, []string{"velocity"})
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&week, "week", false, "also include the velocity table")
	return cmd
}

func (a *App) exportCmd() *cobra.Command {
	var (
		asCSV bool
		asICS bool
		out   string
	)
	cmd := &cobra.Command{
		Use:     "export",
		Short:   "Dump the store or item data",
		Example: "  clerify export --csv > tasks.csv\n  clerify export --ics > deadlines.ics",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			if asCSV && asICS {
				return fail(2, errors.New("--csv and --ics are mutually exclusive"))
			}
			if (asCSV || asICS) && a.flags.JSON {
				return fail(2, errors.New("--json cannot be combined with --csv or --ics"))
			}

			var buf bytes.Buffer
			switch {
			case asCSV:
				if err := writeCSV(&buf, a.DB.Items()); err != nil {
					return err
				}
			case asICS:
				if err := writeICS(&buf, a.DB.Items(), a.now()); err != nil {
					return err
				}
			default:
				enc := json.NewEncoder(&buf)
				enc.SetIndent("", "  ")
				if err := enc.Encode(a.DB.Data); err != nil {
					return err
				}
			}

			if strings.TrimSpace(out) != "" {
				return atomicfile.WriteFile(out, buf.Bytes(), 0o644)
			}
			_, err := a.Out.Write(buf.Bytes())
			return err
		},
	}
	cmd.Flags().BoolVar(&asCSV, "csv", false, "item CSV")
	cmd.Flags().BoolVar(&asICS, "ics", false, "iCalendar feed with deadlines")
	cmd.Flags().StringVar(&out, "out", "", "write to a file instead of stdout")
	return cmd
}

func (a *App) importCmd() *cobra.Command {
	var (
		merge   bool
		fromCSV bool
	)
	cmd := &cobra.Command{
		Use:     "import <file>",
		Short:   "Restore a store, a web-app export, or a CSV",
		Example: "  clerify import backup.json\n  clerify import tasks.csv --from-csv --merge",
		Args:    exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			return a.runImport(args[0], merge, fromCSV)
		},
	}
	cmd.Flags().BoolVar(&merge, "merge", false, "merge by id instead of replacing everything")
	cmd.Flags().BoolVar(&fromCSV, "from-csv", false, "treat the file as an item CSV")
	return cmd
}

func (a *App) runImport(path string, merge, fromCSV bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fail(2, err)
	}

	var incoming model.Store
	var warnings []string
	source := "native"

	switch {
	case fromCSV:
		source = "csv"
		items, csvWarnings, err := parseCSV(data)
		if err != nil {
			return fail(2, err)
		}
		incoming = model.Store{Items: items}
		warnings = csvWarnings
	case store.IsLegacyExport(data):
		source = "legacy"
		res, err := store.ParseLegacy(data)
		if err != nil {
			return err
		}
		incoming = res.Store
		warnings = res.Warnings
	case store.IsNativeStore(data):
		if err := json.Unmarshal(data, &incoming); err != nil {
			return fail(2, fmt.Errorf("parse store: %w", err))
		}
		if incoming.SchemaVersion > model.SchemaVersion {
			return fail(2, fmt.Errorf("file schema %d is newer than this binary (%d)", incoming.SchemaVersion, model.SchemaVersion))
		}
		warnings = append(warnings, store.Upgrade(&incoming)...)
	default:
		return fail(2, errors.New("unrecognised file: expected a clerify store, a web-app export, or --from-csv"))
	}

	for _, w := range warnings {
		if !a.flags.Quiet {
			fmt.Fprintln(a.Err, "warning:", w)
		}
	}

	if !a.flags.Yes && !a.confirm(fmt.Sprintf("Import %d item(s) and %d project(s)?", len(incoming.Items), len(incoming.Projects))) {
		return fail(1, errors.New("aborted (pass --yes to skip confirmation)"))
	}

	backup, err := a.backupStore()
	if err != nil {
		return err
	}

	itemAdded, itemReplaced, itemSkipped := 0, 0, 0
	projectAdded, projectReplaced, projectSkipped := 0, 0, 0
	if merge {
		itemAdded, itemReplaced, itemSkipped, err = a.DB.UpsertItemsIfNewer(incoming.Items)
		if err != nil {
			return err
		}
		projectAdded, projectReplaced, projectSkipped, err = a.DB.UpsertProjectsIfNewer(incoming.Projects)
		if err != nil {
			return err
		}
	} else {
		if err := a.DB.ReplaceItems(incoming.Items); err != nil {
			return err
		}
		// A CSV has no project collection. Replacing items must not wipe the
		// projects that those items still reference.
		if source != "csv" {
			if err := a.DB.ReplaceProjects(incoming.Projects); err != nil {
				return err
			}
		}
		if source != "csv" && (incoming.Profile.Skills != "" || incoming.Profile.PriorityPreset != "") {
			if _, err := a.DB.SetProfile(incoming.Profile); err != nil {
				return err
			}
		}
		itemAdded = len(incoming.Items)
	}

	if a.flags.JSON {
		return a.printJSON(map[string]any{
			"mode":   map[bool]string{true: "merge", false: "replace"}[merge],
			"source": source,
			"items":  len(incoming.Items),
			"itemStats": map[string]int{
				"added": itemAdded, "replaced": itemReplaced, "skipped": itemSkipped,
			},
			"projects": len(incoming.Projects),
			"projectStats": map[string]int{
				"added": projectAdded, "replaced": projectReplaced, "skipped": projectSkipped,
			},
			"backup": backup,
		})
	}
	a.printf("imported %d item(s), %d project(s)", len(incoming.Items), len(incoming.Projects))
	if merge {
		a.printf(" (items: %d added, %d replaced, %d skipped; projects: %d added, %d replaced, %d skipped)",
			itemAdded, itemReplaced, itemSkipped, projectAdded, projectReplaced, projectSkipped)
	}
	a.printf("\n")
	if backup != "" {
		a.printf("previous store saved to %s\n", backup)
	}
	return nil
}

func (a *App) backupStore() (string, error) {
	data, err := os.ReadFile(a.Paths.StoreFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	target := filepath.Join(a.Paths.Dir, "store.json.bak."+a.now().Format("20060102-150405"))
	return target, atomicfile.WriteFile(target, data, 0o600)
}

func writeCSV(w io.Writer, items []model.Item) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(csvHeader); err != nil {
		return err
	}
	for _, it := range items {
		deadline := ""
		if it.Deadline != nil {
			deadline = *it.Deadline
		}
		actual := ""
		if it.ActualHours != 0 {
			actual = format.TrimFloat(it.ActualHours)
		}
		impact := ""
		if it.ImpactScore != nil {
			impact = strconv.Itoa(*it.ImpactScore)
		}
		checklist := ""
		if len(it.Checklist) > 0 {
			if raw, err := json.Marshal(it.Checklist); err == nil {
				checklist = string(raw)
			}
		}
		analyzed := ""
		if it.AnalyzedAt != nil {
			analyzed = *it.AnalyzedAt
		}
		blocker := ""
		if it.BlockerNote != nil {
			blocker = *it.BlockerNote
		}
		boostReason, boosted := "", ""
		if it.Boost.Reason != nil {
			boostReason = *it.Boost.Reason
		}
		if it.Boost.BoostedPriorityScore != nil {
			boosted = strconv.Itoa(*it.Boost.BoostedPriorityScore)
		}
		row := []string{
			sanitizeCSVCell(it.ID),
			sanitizeCSVCell(it.Title),
			sanitizeCSVCell(it.Type),
			sanitizeCSVCell(it.Status),
			strconv.Itoa(it.PriorityScore),
			format.TrimFloat(it.EstimatedHours),
			actual,
			deadline,
			it.CreatedAt,
			it.UpdatedAt,
			impact,
			strconv.Itoa(it.Difficulty),
			sanitizeCSVCell(it.Notes),
			sanitizeCSVCell(it.Description),
			sanitizeCSVCell(deref(it.ProjectID)),
			sanitizeCSVCell(checklist),
			analyzed,
			sanitizeCSVCell(blocker),
			strconv.FormatBool(it.Boost.Active),
			sanitizeCSVCell(boostReason),
			boosted,
			sanitizeCSVCell(it.RawContent),
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func parseCSV(data []byte) ([]model.Item, []string, error) {
	r := csv.NewReader(strings.NewReader(string(data)))
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		return nil, nil, err
	}
	if len(records) == 0 {
		return nil, nil, errors.New("empty CSV")
	}

	index := make(map[string]int)
	for i, name := range records[0] {
		index[normalizeHeader(name)] = i
	}
	get := func(row []string, name string) string {
		i, ok := index[normalizeHeader(name)]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}

	var warnings []string
	items := make([]model.Item, 0, len(records)-1)
	for rowNum, row := range records[1:] {
		if len(row) < 2 {
			continue
		}
		it := model.NewItem(unescapeCSVCell(get(row, "Title")))
		if id := unescapeCSVCell(get(row, "ID")); id != "" {
			it.ID = id
		}
		if typ := get(row, "Type"); typ != "" {
			it.Type = model.NormalizeType(unescapeCSVCell(typ))
		}
		if status := get(row, "Status"); status != "" {
			it.Status = model.NormalizeStatus(unescapeCSVCell(status))
		}
		if v := get(row, "Priority Score"); v != "" {
			it.PriorityScore = atoiOr(v, model.DefaultPriority)
		}
		if v := get(row, "Estimated Hours"); v != "" {
			it.EstimatedHours = atofOr(v, model.DefaultEstimatedHours)
		}
		if v := get(row, "Actual Hours"); v != "" {
			it.ActualHours = atofOr(v, 0)
		}
		if v := get(row, "Deadline"); v != "" {
			due, err := model.ParseDeadlineStrict(v)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("row %d: %v", rowNum+2, err))
			} else if due != "" {
				it.Deadline = &due
			}
		}
		it.CreatedAt = get(row, "Created")
		it.UpdatedAt = get(row, "Updated")
		if v := get(row, "Impact Score"); v != "" {
			impact := atoiOr(v, model.MinImpact)
			it.ImpactScore = &impact
		}
		if v := get(row, "Difficulty"); v != "" {
			it.Difficulty = atoiOr(v, model.DefaultDifficulty)
		}
		it.Notes = unescapeCSVCell(get(row, "Notes"))
		it.Description = unescapeCSVCell(get(row, "Description"))
		if v := unescapeCSVCell(get(row, "Project ID")); v != "" {
			it.ProjectID = &v
		}
		if v := get(row, "Checklist"); v != "" {
			var checklist []model.ChecklistItem
			if err := json.Unmarshal([]byte(unescapeCSVCell(v)), &checklist); err != nil {
				warnings = append(warnings, fmt.Sprintf("row %d: invalid checklist JSON", rowNum+2))
			} else {
				it.Checklist = checklist
			}
		}
		if v := get(row, "Analyzed At"); v != "" {
			it.AnalyzedAt = &v
		}
		if v := unescapeCSVCell(get(row, "Blocker Note")); v != "" {
			it.BlockerNote = &v
		}
		it.Boost.Active = strings.EqualFold(get(row, "Boost Active"), "true")
		if v := unescapeCSVCell(get(row, "Boost Reason")); v != "" {
			it.Boost.Reason = &v
		}
		if v := get(row, "Boosted Priority Score"); v != "" {
			score := atoiOr(v, model.DefaultPriority)
			it.Boost.BoostedPriorityScore = &score
		}
		it.RawContent = unescapeCSVCell(get(row, "Raw Content"))
		items = append(items, model.NormalizeItem(it))
	}
	return items, warnings, nil
}

func normalizeHeader(s string) string {
	s = strings.TrimPrefix(strings.TrimSpace(s), "\ufeff")
	return strings.ToLower(s)
}

func sanitizeCSVCell(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

func unescapeCSVCell(s string) string {
	if len(s) >= 2 && s[0] == '\'' {
		switch s[1] {
		case '=', '+', '-', '@', '\t', '\r':
			return s[1:]
		}
	}
	return s
}

type icsWriter struct {
	w   io.Writer
	err error
}

func (x *icsWriter) line(s string) {
	if x.err != nil {
		return
	}
	for _, part := range foldICS(s) {
		if _, err := fmt.Fprintf(x.w, "%s\r\n", part); err != nil {
			x.err = err
			return
		}
	}
}

func foldICS(s string) []string {
	const limit = 75
	var (
		lines []string
		b     strings.Builder
	)
	for _, r := range s {
		size := utf8.RuneLen(r)
		if b.Len()+size > limit {
			lines = append(lines, b.String())
			b.Reset()
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	lines = append(lines, b.String())
	return lines
}

func writeICS(w io.Writer, items []model.Item, now time.Time) error {
	x := &icsWriter{w: w}
	x.line("BEGIN:VCALENDAR")
	x.line("VERSION:2.0")
	x.line("PRODID:-//clerify//EN")
	x.line("CALSCALE:GREGORIAN")

	stamp := now.UTC().Format("20060102T150405Z")
	for _, it := range items {
		if it.Deadline == nil || *it.Deadline == "" {
			continue
		}
		t, ok := model.ParseDeadline(*it.Deadline)
		if !ok {
			continue
		}
		x.line("BEGIN:VEVENT")
		x.line("UID:" + it.ID + "@clerify")
		x.line("DTSTAMP:" + stamp)
		x.line("DTSTART;VALUE=DATE:" + t.Format("20060102"))
		x.line("SUMMARY:" + icsEscape(it.Title))
		if it.Notes != "" {
			x.line("DESCRIPTION:" + icsEscape(it.Notes))
		}
		x.line("STATUS:CONFIRMED")
		if it.IsDone() {
			x.line("X-CLERIFY-COMPLETED:TRUE")
		}
		x.line("END:VEVENT")
	}
	x.line("END:VCALENDAR")
	return x.err
}

func icsEscape(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\n", `\n`)
	return replacer.Replace(s)
}

func atoiOr(s string, fallback int) int {
	var v int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &v); err != nil {
		return fallback
	}
	return v
}

func atofOr(s string, fallback float64) float64 {
	var v float64
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%f", &v); err != nil {
		return fallback
	}
	return v
}

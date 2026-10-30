package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/P-Baranwal/clerify/internal/ai"
	"github.com/P-Baranwal/clerify/internal/capture"
	"github.com/P-Baranwal/clerify/internal/format"
	"github.com/P-Baranwal/clerify/internal/model"
	"github.com/P-Baranwal/clerify/internal/pdf"
	"github.com/P-Baranwal/clerify/internal/store"
)

type addFlags struct {
	title   string
	typ     string
	due     string
	project string
	impact  int
	est     float64
	actual  float64
	checks  []string
	note    string
	desc    string
	file    string
	stdin   bool
	noAI    bool
	analyze bool
}

func (a *App) addCmd() *cobra.Command {
	var f addFlags
	cmd := &cobra.Command{
		Use:   "add [text]",
		Short: "Create an item, optionally analyzed by AI",
		Long: `Create an item. Without --analyze the item is created with neutral
defaults; with --analyze the AI fills in difficulty, effort, priority and a
checklist from the text, a file, or stdin.

Short natural-language captures work out of the box: a deadline and an effort
are pulled out of "essay due friday, about 5h".`,
		Example: `  clerify add "Write report" --due friday --est 3
  clerify add "essay due friday, about 5h"
  clerify add --file brief.pdf --analyze`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			return a.runAdd(cmd, f, args)
		},
	}

	fl := cmd.Flags()
	fl.StringVar(&f.title, "title", "", "item title (defaults to the AI title or the first line)")
	fl.StringVar(&f.typ, "type", "", "Essay|Coding|Math|Research|Other")
	fl.StringVar(&f.due, "due", "", "deadline: YYYY-MM-DD, tomorrow, fri, +3d, or next week")
	fl.StringVar(&f.project, "project", "", "project id, id prefix, or title")
	fl.IntVar(&f.impact, "impact", 0, "impact score 1-10 (switches this item to impact-for-effort scoring)")
	fl.Float64Var(&f.est, "est", 0, "estimated hours")
	fl.Float64Var(&f.actual, "actual", 0, "actual hours spent")
	fl.StringArrayVar(&f.checks, "check", nil, "checklist item (repeatable; commas are literal)")
	fl.StringVar(&f.note, "note", "", "short note")
	fl.StringVar(&f.desc, "desc", "", "description")
	fl.StringVar(&f.file, "file", "", "read text from a file (PDF or plain text)")
	fl.BoolVar(&f.stdin, "stdin", false, "read text from stdin")
	fl.BoolVar(&f.noAI, "no-ai", false, "never call the AI")
	fl.BoolVar(&f.analyze, "analyze", false, "analyze the input with AI")
	_ = cmd.RegisterFlagCompletionFunc("project", a.completeProjectRefs)
	_ = cmd.RegisterFlagCompletionFunc("type", a.completeTypes)
	return cmd
}

func (a *App) runAdd(cmd *cobra.Command, f addFlags, args []string) error {
	raw, err := a.gatherInputAllowTitle(f.file, f.stdin, f.title, args)
	if err != nil {
		if !a.isInteractive() || f.file != "" || f.stdin || len(args) > 0 || strings.TrimSpace(f.title) != "" {
			return err
		}
		title, due, promptErr := a.promptNewItem()
		if promptErr != nil {
			return promptErr
		}
		if title == "" {
			return err
		}
		f.title = title
		if f.due == "" {
			f.due = due
		}
		raw = ""
	}

	useAI := f.analyze && !f.noAI
	var result ai.AnalysisResult
	if useAI {
		input := raw
		if strings.TrimSpace(input) == "" {
			input = f.title
		}
		res, ok, err := a.analyzeText(input, nil, "")
		if err != nil {
			return err
		}
		if !ok {
			return nil // --dry-run printed the prompt
		}
		result = res
	}

	// Quick capture: "essay due friday, about 5h". Explicit flags always win,
	// and plain titles with no date/effort cue are left untouched.
	var captured capture.Result
	captureOK := false
	if !useAI && raw != "" && strings.TrimSpace(f.title) == "" &&
		!cmd.Flags().Changed("title") && !cmd.Flags().Changed("due") &&
		!cmd.Flags().Changed("est") && !cmd.Flags().Changed("type") {
		captured, captureOK = capture.Parse(raw, a.now())
	}
	if captureOK && !f.noAI && !a.flags.DryRun &&
		(ai.ResolveKey(a.Cfg.AI, a.flags.APIKey) != "" || a.Cfg.AI.NoAuth) {
		ext, err := a.extractTask(raw)
		if err != nil {
			if !a.flags.Quiet {
				fmt.Fprintf(a.Err, "warning: AI capture failed, using the offline result: %v\n", err)
			}
		} else {
			mergeExtracted(&captured, ext)
		}
	}

	it := model.NewItem(f.title)
	if captureOK {
		it.Title = captured.Title
		if captured.Type != "" {
			it.Type = captured.Type
		}
		if captured.Due != nil {
			it.Deadline = captured.Due
		}
		if captured.EstimatedHours != nil {
			it.EstimatedHours = *captured.EstimatedHours
		}
	}
	if useAI {
		applyAnalysisNew(&it, result)
		it.AnalyzedAt = strPtr(model.NowRFC3339(a.now()))
		if strings.TrimSpace(f.title) != "" {
			it.Title = strings.TrimSpace(f.title)
		}
	} else if strings.TrimSpace(f.title) == "" && !captureOK {
		it.Title = deriveTitle(raw)
	}

	if f.typ != "" {
		typ, err := model.ParseType(f.typ)
		if err != nil {
			return fail(2, err)
		}
		it.Type = typ
	}
	if f.due != "" {
		due, err := model.ParseDeadlineInput(f.due, a.now())
		if err != nil {
			return fail(2, err)
		}
		it.Deadline = &due
	}
	if cmd.Flags().Changed("est") {
		it.EstimatedHours = f.est
	}
	if f.actual > 0 {
		it.ActualHours = f.actual
	}
	if f.impact > 0 {
		v := f.impact
		it.ImpactScore = &v
	}
	if f.note != "" {
		it.Notes = f.note
	}
	if f.desc != "" {
		it.Description = f.desc
	}
	if raw != "" {
		it.RawContent = raw
	}

	if len(f.checks) > 0 {
		it.Checklist = model.ReconcileChecklist(nil, f.checks)
	} else if useAI && len(result.Checklist) > 0 {
		it.Checklist = model.ReconcileChecklist(nil, result.Checklist)
	}

	projectID, err := a.resolveProject(f.project)
	if err != nil {
		return notFound("project", f.project, err)
	}
	if projectID != nil {
		it.ProjectID = projectID
	}

	if a.flags.DryRun {
		return a.printJSON(it)
	}

	var saved model.Item
	err = a.DB.Mutate(func(tx *store.Tx) error {
		now := model.NowRFC3339(tx.Now())
		it = model.NormalizeItem(it)
		if it.ID == "" {
			it.ID = model.NewID()
		}
		if it.CreatedAt == "" {
			it.CreatedAt = now
		}
		it.UpdatedAt = now
		if it.Status == model.StatusDone && it.CompletedAt == nil {
			it.CompletedAt = &now
		}
		tx.Store().Items = append(tx.Store().Items, it)
		saved = it.Clone()
		tx.Journal(store.Entry{
			TS:    now,
			Op:    store.OpCreate,
			Label: "Added \"" + saved.Title + "\"",
			After: &saved,
		})
		return nil
	})
	if err != nil {
		return err
	}

	if a.flags.JSON {
		return a.printJSON(saved)
	}
	switch {
	case saved.ImpactScore != nil:
		a.printf("added %s  %s  (priority %d computed from impact ÷ hours, est %s)\n",
			shortID(saved.ID), saved.Title, saved.EffectiveScore(), format.Hours(saved.EstimatedHours))
	case !useAI:
		a.printf("added %s  %s  (priority %d is the default; run `clerify analyze %s` to score it with AI, est %s)\n",
			shortID(saved.ID), saved.Title, saved.EffectiveScore(), shortID(saved.ID), format.Hours(saved.EstimatedHours))
	default:
		a.printf("added %s  %s  (priority %d, est %s)\n", shortID(saved.ID), saved.Title, saved.EffectiveScore(), format.Hours(saved.EstimatedHours))
	}
	return nil
}

func (a *App) analyzeCmd() *cobra.Command {
	var (
		file  string
		stdin bool
		redo  bool
	)
	cmd := &cobra.Command{
		Use:     "analyze [id|file|-]",
		Short:   "Run AI analysis on an existing item or new text",
		Long:    "Ask the AI to fill in difficulty, effort, priority, and a checklist. With no arguments it reads stdin.",
		Example: "  clerify analyze #1\n  clerify analyze --file brief.pdf",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			return a.runAnalyze(args, file, stdin, redo)
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "analyze a file instead of an id")
	cmd.Flags().BoolVar(&stdin, "stdin", false, "analyze stdin instead of an id")
	cmd.Flags().BoolVar(&redo, "re", false, "re-analyze, preserving checklist progress by text")
	return cmd
}

func (a *App) runAnalyze(args []string, file string, stdin, redo bool) error {
	// Existing item?
	if len(args) == 1 && file == "" && !stdin && args[0] != "-" {
		if _, statErr := os.Stat(args[0]); statErr != nil {
			it, err := a.requireItem(args[0])
			if err != nil {
				return err
			}
			raw := it.RawContent
			if strings.TrimSpace(raw) == "" {
				raw = strings.TrimSpace(it.Title + "\n" + it.Description)
			}
			res, ok, err := a.analyzeText(raw, nil, "")
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
			return a.applyAnalysisToItem(it, res, redo)
		}
	}

	raw, err := a.gatherInput(file, stdin, args)
	if err != nil {
		return err
	}
	res, ok, err := a.analyzeText(raw, nil, "")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	it := model.NewItem(res.Title)
	applyAnalysisNew(&it, res)
	it.RawContent = raw
	it.Checklist = model.ReconcileChecklist(nil, res.Checklist)

	if a.flags.DryRun {
		return a.printJSON(it)
	}
	var saved model.Item
	err = a.DB.Mutate(func(tx *store.Tx) error {
		now := model.NowRFC3339(tx.Now())
		it = model.NormalizeItem(it)
		if it.ID == "" {
			it.ID = model.NewID()
		}
		if it.CreatedAt == "" {
			it.CreatedAt = now
		}
		it.UpdatedAt = now
		if it.Status == model.StatusDone && it.CompletedAt == nil {
			it.CompletedAt = &now
		}
		tx.Store().Items = append(tx.Store().Items, it)
		saved = it.Clone()
		tx.Journal(store.Entry{
			TS:    now,
			Op:    store.OpCreate,
			Label: "Added \"" + saved.Title + "\"",
			After: &saved,
		})
		return nil
	})
	if err != nil {
		return err
	}
	if a.flags.JSON {
		return a.printJSON(saved)
	}
	a.printf("added %s  %s  (priority %d, est %s, difficulty %d)\n",
		shortID(saved.ID), saved.Title, saved.EffectiveScore(), format.Hours(saved.EstimatedHours), saved.Difficulty)
	return nil
}

func (a *App) applyAnalysisToItem(it model.Item, res ai.AnalysisResult, redo bool) error {
	var saved model.Item
	err := a.DB.Mutate(func(tx *store.Tx) error {
		cur, ok := tx.Item(it.ID)
		if !ok {
			return store.ErrNotFound
		}
		before := cur.Clone()

		// Plain analyze fills unset fields; --re deliberately leaves user-edited
		// title/type/deadline/estimate alone and only refreshes AI-owned fields.
		applyAnalysisRefresh(cur, res, !redo)
		cur.AnalyzedAt = strPtr(model.NowRFC3339(tx.Now()))
		cur.Checklist = model.ReconcileChecklist(cur.Checklist, res.Checklist)
		*cur = model.NormalizeItem(*cur)
		cur.UpdatedAt = model.NowRFC3339(tx.Now())
		saved = cur.Clone()
		tx.Journal(store.Entry{
			TS:     model.NowRFC3339(tx.Now()),
			Op:     store.OpUpdate,
			Label:  "Analyzed \"" + saved.Title + "\"",
			Before: &before,
			After:  &saved,
		})
		return nil
	})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return notFound("item", it.ID, err)
		}
		return err
	}

	if a.flags.JSON {
		return a.printJSON(saved)
	}
	a.printf("analyzed %s  %s  (priority %d, est %s, difficulty %d)\n",
		shortID(saved.ID), saved.Title, saved.EffectiveScore(), format.Hours(saved.EstimatedHours), saved.Difficulty)
	return nil
}

// analyzeText renders and sends the analysis prompt. ok is false when
// --dry-run printed the prompt instead of calling the provider.
func (a *App) analyzeText(raw string, existing *model.Item, boostReason string) (ai.AnalysisResult, bool, error) {
	raw = a.truncate(raw)
	if existing != nil {
		trimmed := existing.Clone()
		trimmed.RawContent = a.truncate(trimmed.RawContent)
		trimmed.Description = a.truncate(trimmed.Description)
		trimmed.Notes = a.truncate(trimmed.Notes)
		existing = &trimmed
	}
	in := ai.AnalyzeInput{
		RawContent:      raw,
		Profile:         a.DB.Data.Profile,
		BoostReason:     boostReason,
		ExistingContext: existing,
	}
	system, user, feature := ai.BuildAnalyzePrompt(in, a.now())

	if a.flags.DryRun {
		fmt.Fprintln(a.Out, "--- system ---")
		fmt.Fprintln(a.Out, system)
		fmt.Fprintln(a.Out, "--- user ---")
		fmt.Fprintln(a.Out, user)
		return ai.AnalysisResult{}, false, nil
	}

	client := a.aiClient()
	if a.flags.Verbose {
		fmt.Fprintf(a.Err, "calling %s model %s (%s)\n", client.Config().BaseURL, client.Config().Model, feature)
	}
	out, err := client.Analyze(context.Background(), system, user, feature)
	if err != nil {
		return ai.AnalysisResult{}, false, err
	}
	res, err := ai.ParseAnalysis(out)
	if err != nil {
		return ai.AnalysisResult{}, false, err
	}
	return res, true, nil
}

func (a *App) truncate(s string) string {
	limit := a.Cfg.AI.MaxInputChars
	if limit <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	if !a.flags.Quiet {
		fmt.Fprintf(a.Err, "note: input truncated to %d characters\n", limit)
	}
	return string(runes[:limit])
}

// applyAnalysisNew fills a brand-new item from an AI result.
func applyAnalysisNew(it *model.Item, r ai.AnalysisResult) {
	if r.Title != "" {
		it.Title = r.Title
	}
	it.Type = model.NormalizeType(r.Type)
	if r.Deadline != nil {
		it.Deadline = r.Deadline
	}
	it.EstimatedHours = r.EstimatedHours
	applyAnalysisScores(it, r)
}

// applyAnalysisRefresh updates AI-owned fields on an existing item. When
// fillUnset is true it may also fill empty title/type/deadline/estimate; when
// false it never touches those fields, so manual edits survive re-analysis.
func applyAnalysisRefresh(it *model.Item, r ai.AnalysisResult, fillUnset bool) {
	if fillUnset {
		if strings.TrimSpace(it.Title) == "" || it.Title == "Untitled" {
			if r.Title != "" {
				it.Title = r.Title
			}
		}
		if strings.TrimSpace(it.Type) == "" || it.Type == model.TypeOther {
			it.Type = model.NormalizeType(r.Type)
		}
		if it.Deadline == nil && r.Deadline != nil {
			it.Deadline = r.Deadline
		}
		if it.EstimatedHours == 0 {
			it.EstimatedHours = r.EstimatedHours
		}
	}
	applyAnalysisScores(it, r)
}

func applyAnalysisScores(it *model.Item, r ai.AnalysisResult) {
	it.Difficulty = r.Difficulty
	it.DifficultyReasoning = r.DifficultyReasoning
	it.EstimatedHoursReasoning = r.EstimatedHoursReasoning
	it.PriorityScore = r.PriorityScore
	it.PriorityReasoning = r.PriorityReasoning
}

// gatherInputAllowTitle permits `add --title X` with no body.
func (a *App) gatherInputAllowTitle(file string, stdin bool, title string, args []string) (string, error) {
	if file == "" && !stdin && len(args) == 0 && strings.TrimSpace(title) != "" {
		return "", nil
	}
	return a.gatherInput(file, stdin, args)
}

// promptNewItem is the interactive fallback for a bare `clerify add`.
func (a *App) promptNewItem() (string, string, error) {
	r := bufio.NewReader(a.Stdin)
	title := a.promptLine(r, "Task title: ")
	if title == "" {
		return "", "", nil
	}
	due := a.promptLine(r, "Due date (optional, e.g. tomorrow): ")
	return title, due, nil
}

// extractTask asks the provider for a structured task from a short line.
func (a *App) extractTask(raw string) (ai.ExtractedTask, error) {
	system, user := ai.BuildExtractTaskPrompt(a.truncate(raw), a.now())
	out, err := a.aiClient().Analyze(context.Background(), system, user, "extract_task")
	if err != nil {
		return ai.ExtractedTask{}, err
	}
	return ai.ParseExtractedTask(out)
}

func mergeExtracted(r *capture.Result, ext ai.ExtractedTask) {
	if title := strings.TrimSpace(ext.Title); title != "" && title != "Untitled Task" {
		r.Title = title
	}
	if ext.Type != "" {
		r.Type = ext.Type
	}
	if ext.Deadline != nil {
		r.Due = ext.Deadline
	}
	if ext.EstimatedHours != nil {
		r.EstimatedHours = ext.EstimatedHours
	}
}

// gatherInput resolves the text to work on from a file, stdin, or arguments.
func (a *App) gatherInput(file string, stdin bool, args []string) (string, error) {
	switch {
	case file != "":
		text, err := pdf.Extract(file)
		if err != nil {
			return "", fail(2, err)
		}
		return strings.TrimSpace(text), nil
	case stdin || (len(args) == 1 && args[0] == "-"):
		data, err := io.ReadAll(a.Stdin)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	case len(args) > 0:
		return strings.TrimSpace(strings.Join(args, " ")), nil
	default:
		return "", fail(2, fmt.Errorf("nothing to add: pass text, --file, or --stdin"))
	}
}

func deriveTitle(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			if len([]rune(line)) > 72 {
				return string([]rune(line)[:72])
			}
			return line
		}
	}
	return "Untitled"
}

func strPtr(s string) *string { return &s }

func shortID(id string) string {
	if len(id) <= 6 {
		return id
	}
	return id[:6]
}

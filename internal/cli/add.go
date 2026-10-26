package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/P-Baranwal/clerify/internal/ai"
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
checklist from the text, a file, or stdin.`,
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
	fl.StringVar(&f.due, "due", "", "deadline as YYYY-MM-DD")
	fl.StringVar(&f.project, "project", "", "project id, id prefix, or title")
	fl.IntVar(&f.impact, "impact", 0, "impact score 1-10 (enables ROI priority)")
	fl.Float64Var(&f.est, "est", 0, "estimated hours")
	fl.Float64Var(&f.actual, "actual", 0, "actual hours spent")
	fl.StringSliceVar(&f.checks, "check", nil, "checklist item (repeatable)")
	fl.StringVar(&f.note, "note", "", "short note")
	fl.StringVar(&f.desc, "desc", "", "description")
	fl.StringVar(&f.file, "file", "", "read text from a file (PDF or plain text)")
	fl.BoolVar(&f.stdin, "stdin", false, "read text from stdin")
	fl.BoolVar(&f.noAI, "no-ai", false, "never call the AI")
	fl.BoolVar(&f.analyze, "analyze", false, "analyze the input with AI")
	return cmd
}

func (a *App) runAdd(cmd *cobra.Command, f addFlags, args []string) error {
	raw, err := a.gatherInput(f.file, f.stdin, args)
	if err != nil {
		return err
	}

	useAI := f.analyze && !f.noAI
	var result ai.AnalysisResult
	if useAI {
		res, ok, err := a.analyzeText(raw, nil, "")
		if err != nil {
			return err
		}
		if !ok {
			return nil // --dry-run printed the prompt
		}
		result = res
	}

	it := model.NewItem(f.title)
	if useAI {
		applyAnalysis(&it, result)
		it.AnalyzedAt = strPtr(model.NowRFC3339(a.now()))
		if strings.TrimSpace(f.title) != "" {
			it.Title = strings.TrimSpace(f.title)
		}
	} else if strings.TrimSpace(f.title) == "" {
		it.Title = deriveTitle(raw)
	}

	if f.typ != "" {
		it.Type = model.NormalizeType(f.typ)
	}
	if f.due != "" {
		due := strings.TrimSpace(f.due)
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

	saved, err := a.DB.AddItem(it)
	if err != nil {
		return err
	}
	a.journalItem(store.OpCreate, "Added \""+saved.Title+"\"", nil, &saved)

	if a.flags.JSON {
		return a.printJSON(saved)
	}
	a.printf("added %s  %s  (priority %d, est %s)\n", shortID(saved.ID), saved.Title, saved.EffectiveScore(), hours(saved.EstimatedHours))
	return nil
}

func (a *App) analyzeCmd() *cobra.Command {
	var (
		file  string
		stdin bool
		redo  bool
	)
	cmd := &cobra.Command{
		Use:   "analyze [id|file|-]",
		Short: "Run AI analysis on an existing item or new text",
		Args:  cobra.MaximumNArgs(1),
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
	applyAnalysis(&it, res)
	it.RawContent = raw
	it.Checklist = model.ReconcileChecklist(nil, res.Checklist)

	if a.flags.DryRun {
		return a.printJSON(it)
	}
	saved, err := a.DB.AddItem(it)
	if err != nil {
		return err
	}
	a.journalItem(store.OpCreate, "Added \""+saved.Title+"\"", nil, &saved)
	if a.flags.JSON {
		return a.printJSON(saved)
	}
	a.printf("added %s  %s  (priority %d, est %s, difficulty %d)\n",
		shortID(saved.ID), saved.Title, saved.EffectiveScore(), hours(saved.EstimatedHours), saved.Difficulty)
	return nil
}

func (a *App) applyAnalysisToItem(it model.Item, res ai.AnalysisResult, redo bool) error {
	before := it.Clone()
	applyAnalysis(&it, res)
	it.AnalyzedAt = strPtr(model.NowRFC3339(a.now()))

	if redo {
		lines := make([]string, 0, len(res.Checklist))
		lines = append(lines, res.Checklist...)
		it.Checklist = model.ReconcileChecklist(it.Checklist, lines)
	} else {
		it.Checklist = model.ReconcileChecklist(it.Checklist, res.Checklist)
	}

	if a.flags.DryRun {
		return a.printJSON(it)
	}
	saved, err := a.DB.UpdateItem(it)
	if err != nil {
		return err
	}
	a.journalItem(store.OpUpdate, "Analyzed \""+saved.Title+"\"", &before, &saved)

	if a.flags.JSON {
		return a.printJSON(saved)
	}
	a.printf("analyzed %s  %s  (priority %d, est %s, difficulty %d)\n",
		shortID(saved.ID), saved.Title, saved.EffectiveScore(), hours(saved.EstimatedHours), saved.Difficulty)
	return nil
}

// analyzeText renders and sends the analysis prompt. ok is false when
// --dry-run printed the prompt instead of calling the provider.
func (a *App) analyzeText(raw string, existing *model.Item, boostReason string) (ai.AnalysisResult, bool, error) {
	raw = a.truncate(raw)
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

func applyAnalysis(it *model.Item, r ai.AnalysisResult) {
	if r.Title != "" {
		it.Title = r.Title
	}
	it.Type = model.NormalizeType(r.Type)
	if r.Deadline != nil {
		it.Deadline = r.Deadline
	}
	it.Difficulty = r.Difficulty
	it.DifficultyReasoning = r.DifficultyReasoning
	it.EstimatedHours = r.EstimatedHours
	it.EstimatedHoursReasoning = r.EstimatedHoursReasoning
	it.PriorityScore = r.PriorityScore
	it.PriorityReasoning = r.PriorityReasoning
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

func hours(v float64) string {
	s := fmt.Sprintf("%.2f", v)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s + "h"
}

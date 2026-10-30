package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/P-Baranwal/clerify/internal/ai"
	"github.com/P-Baranwal/clerify/internal/config"
	"github.com/P-Baranwal/clerify/internal/model"
	"github.com/P-Baranwal/clerify/internal/render"
	"github.com/P-Baranwal/clerify/internal/store"
)

// Version is set at build time with -ldflags.
var Version = "dev"

// ExitError carries an explicit process exit code.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("exit %d", e.Code)
}

func (e *ExitError) Unwrap() error { return e.Err }

func fail(code int, err error) error { return &ExitError{Code: code, Err: err} }

type globalFlags struct {
	DataDir string
	JSON    bool
	NoColor bool
	Quiet   bool
	Verbose bool
	DryRun  bool
	Yes     bool
	Timeout time.Duration
	APIKey  string
}

// App carries process-wide state for every command.
type App struct {
	flags globalFlags
	Paths store.Paths
	DB    *store.DB
	Cfg   config.Config
	Out   io.Writer
	Err   io.Writer
	Now   func() time.Time
	Stdin io.Reader
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	return ExecuteArgs(nil, os.Stdout, os.Stderr, os.Stdin)
}

// ExecuteArgs runs the CLI with explicit streams and arguments, which is what
// the integration tests use.
func ExecuteArgs(args []string, out, errOut io.Writer, in io.Reader) int {
	app := &App{
		Out:   out,
		Err:   errOut,
		Now:   time.Now,
		Stdin: in,
	}
	root := app.rootCmd()
	if len(args) > 0 {
		root.SetArgs(args)
	}
	if err := root.Execute(); err != nil {
		var exitErr *ExitError
		if errors.As(err, &exitErr) {
			if exitErr.Err != nil {
				fmt.Fprintln(app.Err, "error:", exitErr.Err)
			}
			return exitErr.Code
		}
		fmt.Fprintln(app.Err, "error:", err)
		return codeFor(err)
	}
	return 0
}

func codeFor(err error) int {
	var ambiguous *store.AmbiguousError
	if errors.As(err, &ambiguous) {
		return 3
	}
	if errors.Is(err, store.ErrNotFound) {
		return 3
	}
	if errors.Is(err, store.ErrLockTimeout) {
		return 6
	}
	if errors.Is(err, store.ErrUnreadable) {
		return 5
	}
	var cfgErr *ai.ConfigError
	var apiErr *ai.APIError
	var parseErr *ai.ParseError
	if errors.As(err, &cfgErr) || errors.As(err, &apiErr) || errors.As(err, &parseErr) {
		return 4
	}
	return 1
}

func (a *App) rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "clerify",
		Short:         "Personal, local-first task and project manager with AI scoring",
		Long:          "clerify is a single-user, no-account task and project manager. Everything lives in one JSON file you own.",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	pf := root.PersistentFlags()
	pf.StringVar(&a.flags.DataDir, "data-dir", "", "data directory (default $CLERIFY_HOME or the user config dir)")
	pf.BoolVar(&a.flags.JSON, "json", false, "emit machine-readable JSON")
	pf.BoolVar(&a.flags.NoColor, "no-color", false, "disable color output")
	pf.BoolVar(&a.flags.Quiet, "quiet", false, "suppress non-essential output")
	pf.BoolVar(&a.flags.Verbose, "verbose", false, "show extra detail on stderr")
	pf.BoolVar(&a.flags.DryRun, "dry-run", false, "print what would happen without calling the API or writing")
	pf.BoolVar(&a.flags.Yes, "yes", false, "assume yes for confirmations")
	pf.DurationVar(&a.flags.Timeout, "timeout", 0, "override the AI request timeout (e.g. 90s)")
	pf.StringVar(&a.flags.APIKey, "api-key", "", "API key for this invocation only (never persisted)")

	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return fail(2, err)
	})

	root.AddCommand(
		a.addCmd(),
		a.analyzeCmd(),
		a.lsCmd(),
		a.showCmd(),
		a.editCmd(),
		a.rmCmd(),
		a.undoCmd(),
		a.boostCmd(),
		a.rescoreCmd(),
		a.todayCmd(),
		a.planCmd(),
		a.calendarCmd(),
		a.projectCmd(),
		a.wbsCmd(),
		a.reportCmd(),
		a.digestCmd(),
		a.exportCmd(),
		a.importCmd(),
		a.configCmd(),
		a.aiCmd(),
		a.doctorCmd(),
		a.tuiCmd(),
	)
	root.AddCommand(a.statusCmds()...)
	return root
}

// ensure lazily opens the store and config so commands like `completion` do
// not need a data directory.
func (a *App) ensure() error {
	if a.DB != nil {
		return nil
	}
	paths, err := store.ResolvePaths(a.flags.DataDir)
	if err != nil {
		return err
	}
	cfg, cfgErr := config.Load(paths.ConfigFile)
	if cfgErr != nil && !a.flags.Quiet {
		fmt.Fprintln(a.Err, "warning:", cfgErr)
	}
	db, err := store.Open(paths)
	if err != nil {
		return err
	}
	for _, w := range db.Warnings {
		if !a.flags.Quiet {
			fmt.Fprintln(a.Err, "warning:", w)
		}
	}
	a.Paths, a.Cfg, a.DB = paths, cfg, db
	render.SetOptions(render.Options{
		Color:      !a.flags.NoColor && os.Getenv("NO_COLOR") == "" && isTerminal(a.Out),
		TableStyle: cfg.UI.TableStyle,
		DateFormat: cfg.UI.DateFormat,
	})
	return nil
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// printf writes to stdout unless --quiet.
func (a *App) printf(format string, args ...any) {
	if a.flags.Quiet {
		return
	}
	fmt.Fprintf(a.Out, format, args...)
}

// printJSON renders any value as indented JSON.
func (a *App) printJSON(v any) error {
	enc := json.NewEncoder(a.Out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// aiClient builds a client from config plus per-invocation overrides.
func (a *App) aiClient() *ai.Client {
	cfg := a.Cfg.AI
	if a.flags.Timeout > 0 {
		cfg.TimeoutSeconds = int(a.flags.Timeout.Seconds())
	}
	return ai.NewWithKey(cfg, a.flags.APIKey)
}

// now returns the injectable clock.
func (a *App) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// exactArgs returns a positional-args validator that exits with code 2.
func exactArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			return fail(2, fmt.Errorf("%s requires %d argument(s), got %d", cmd.CommandPath(), n, len(args)))
		}
		return nil
	}
}

// minArgs returns a positional-args validator requiring at least n arguments.
func minArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < n {
			return fail(2, fmt.Errorf("%s requires at least %d argument(s), got %d", cmd.CommandPath(), n, len(args)))
		}
		return nil
	}
}

// resolveProject turns a user-supplied project reference into an id.
func (a *App) resolveProject(ref string) (*string, error) {
	if strings.TrimSpace(ref) == "" {
		return nil, nil
	}
	p, err := a.DB.FindProject(ref)
	if err != nil {
		return nil, err
	}
	return &p.ID, nil
}

// requireItem resolves an item reference or returns an exit-code-3 error.
func (a *App) requireItem(ref string) (model.Item, error) {
	it, err := a.DB.FindItem(ref)
	if err != nil {
		return model.Item{}, notFound("item", ref, err)
	}
	return it, nil
}

func notFound(kind, ref string, err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return fail(3, fmt.Errorf("%s %q not found", kind, ref))
	}
	return fail(3, err)
}

package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/P-Baranwal/clerify/internal/ai"
	"github.com/P-Baranwal/clerify/internal/model"
	"github.com/P-Baranwal/clerify/internal/render"
)

// ── config ───────────────────────────────────────────────────────────────────

func (a *App) configCmd() *cobra.Command {
	parent := &cobra.Command{
		Use:   "config",
		Short: "Show or change profile settings",
	}
	parent.AddCommand(a.configShowCmd(), a.configSetCmd(), a.configPathCmd())
	return parent
}

func (a *App) configShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the profile and AI settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			p := a.DB.Data.Profile
			aiCfg := a.Cfg.AI
			aiCfg.APIKey = maskKey(ai.ResolveKey(aiCfg, a.flags.APIKey))

			if a.flags.JSON {
				aiCfg = redactAIConfig(aiCfg)
				return a.printJSON(map[string]any{"profile": p, "ai": aiCfg, "ui": a.Cfg.UI})
			}

			fmt.Fprintln(a.Out, "profile")
			fmt.Fprintf(a.Out, "  %-18s %s\n", "skills", orDash(p.Skills))
			fmt.Fprintf(a.Out, "  %-18s %s\n", "priorityPreset", p.PriorityPreset)
			fmt.Fprintf(a.Out, "  %-18s %s\n", "customRule", orDash(p.CustomPriorityRule))
			fmt.Fprintf(a.Out, "  %-18s %d\n", "hoursPerDay", p.AvailableHoursPerDay)
			fmt.Fprintf(a.Out, "  %-18s %s\n", "defaultProject", orDash(deref(p.DefaultProjectID)))
			fmt.Fprintf(a.Out, "  %-18s %s\n", "theme", p.Theme)
			fmt.Fprintf(a.Out, "  %-18s %s\n", "tableStyle", orDash(a.Cfg.UI.TableStyle))
			fmt.Fprintf(a.Out, "  %-18s %s\n", "dateFormat", orDash(a.Cfg.UI.DateFormat))

			fmt.Fprintln(a.Out, "\nai")
			fmt.Fprintf(a.Out, "  %-18s %s\n", "preset", orDash(aiCfg.Preset))
			fmt.Fprintf(a.Out, "  %-18s %s\n", "wire", aiCfg.Wire)
			fmt.Fprintf(a.Out, "  %-18s %s\n", "baseURL", orDash(aiCfg.BaseURL))
			fmt.Fprintf(a.Out, "  %-18s %s\n", "model", orDash(aiCfg.Model))
			fmt.Fprintf(a.Out, "  %-18s %s\n", "authHeader", orDash(aiCfg.AuthHeader))
			fmt.Fprintf(a.Out, "  %-18s %s\n", "apiKey", orDash(aiCfg.APIKey))
			fmt.Fprintf(a.Out, "  %-18s %g\n", "temperature", aiCfg.Temperature)
			fmt.Fprintf(a.Out, "  %-18s %d\n", "maxTokens", aiCfg.MaxTokens)
			fmt.Fprintf(a.Out, "  %-18s %ds\n", "timeout", aiCfg.TimeoutSeconds)
			fmt.Fprintf(a.Out, "  %-18s %s\n", "responseFormat", aiCfg.ResponseFormatJSON)
			fmt.Fprintf(a.Out, "  %-18s %d\n", "maxInputChars", aiCfg.MaxInputChars)
			return nil
		},
	}
}

func (a *App) configSetCmd() *cobra.Command {
	var (
		skills         string
		preset         string
		rule           string
		hours          int
		defaultProject string
		theme          string
		tableStyle     string
		dateFormat     string
	)
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Change profile settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			p := a.DB.Data.Profile
			if cmd.Flags().Changed("skills") {
				p.Skills = skills
			}
			if cmd.Flags().Changed("preset") {
				st, err := model.ParsePreset(preset)
				if err != nil {
					return fail(2, err)
				}
				p.PriorityPreset = st
			}
			if cmd.Flags().Changed("rule") {
				p.CustomPriorityRule = rule
			}
			if cmd.Flags().Changed("hours") {
				if err := model.ValidateHours(hours); err != nil {
					return fail(2, err)
				}
				p.AvailableHoursPerDay = hours
			}
			if cmd.Flags().Changed("theme") {
				th, err := model.ParseTheme(theme)
				if err != nil {
					return fail(2, err)
				}
				p.Theme = th
			}
			if cmd.Flags().Changed("default-project") {
				id, err := a.resolveProject(defaultProject)
				if err != nil {
					return notFound("project", defaultProject, err)
				}
				p.DefaultProjectID = id
			}

			uiChanged := false
			if cmd.Flags().Changed("table-style") {
				switch tableStyle {
				case "plain", "ascii", "unicode":
					a.Cfg.UI.TableStyle = tableStyle
					uiChanged = true
				default:
					return fail(2, fmt.Errorf("unknown table style %q (use plain, ascii, or unicode)", tableStyle))
				}
			}
			if cmd.Flags().Changed("date-format") {
				switch dateFormat {
				case model.DateLayout, "02/01/2006", "Jan 2, 2006":
					a.Cfg.UI.DateFormat = dateFormat
					uiChanged = true
				default:
					return fail(2, fmt.Errorf("unknown date format %q", dateFormat))
				}
			}

			saved, err := a.DB.SetProfile(p)
			if err != nil {
				return err
			}
			if uiChanged {
				if err := a.Cfg.Save(a.Paths.ConfigFile); err != nil {
					return err
				}
			}
			if a.flags.JSON {
				return a.printJSON(saved)
			}
			a.printf("profile updated\n")
			return nil
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&skills, "skills", "", "skills / context for AI scoring")
	fl.StringVar(&preset, "preset", "", "Deadline-first|Difficulty-first|Easiest-first|Balanced")
	fl.StringVar(&rule, "rule", "", "custom priority rule")
	fl.IntVar(&hours, "hours", 0, "available hours per day (1-16)")
	fl.StringVar(&defaultProject, "default-project", "", "default project id or title")
	fl.StringVar(&theme, "theme", "", "system|light|dark")
	fl.StringVar(&tableStyle, "table-style", "", "plain|ascii|unicode")
	fl.StringVar(&dateFormat, "date-format", "", "2006-01-02|02/01/2006|Jan 2, 2006")
	return cmd
}

func (a *App) configPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the files clerify uses",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			if a.flags.JSON {
				return a.printJSON(a.Paths)
			}
			fmt.Fprintf(a.Out, "%-10s %s\n", "dir", a.Paths.Dir)
			fmt.Fprintf(a.Out, "%-10s %s\n", "store", a.Paths.StoreFile)
			fmt.Fprintf(a.Out, "%-10s %s\n", "config", a.Paths.ConfigFile)
			fmt.Fprintf(a.Out, "%-10s %s\n", "journal", a.Paths.JournalFile)
			fmt.Fprintf(a.Out, "%-10s %s\n", "backup", a.Paths.BackupFile)
			return nil
		},
	}
}

// ── ai ───────────────────────────────────────────────────────────────────────

func (a *App) aiCmd() *cobra.Command {
	parent := &cobra.Command{
		Use:   "ai",
		Short: "Configure the AI provider",
	}
	parent.AddCommand(a.aiPresetCmd(), a.aiKeyCmd(), a.aiTestCmd(), a.aiModelsCmd(), a.aiShowCmd())
	return parent
}

func (a *App) aiPresetCmd() *cobra.Command {
	var (
		baseURL string
		modelID string
		header  string
		prefix  string
		wire    string
	)
	cmd := &cobra.Command{
		Use:   "preset <name>",
		Short: "Apply a provider preset (any OpenAI-compatible endpoint)",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			name := strings.ToLower(strings.TrimSpace(args[0]))
			previous := a.Cfg.AI
			cfg, ok := ai.ApplyPreset(name)
			if !ok {
				return fail(2, fmt.Errorf("unknown preset %q; known: %s", args[0], strings.Join(ai.PresetNames(), ", ")))
			}
			// Keep an existing key only when re-applying the same provider. A
			// key for one endpoint must never be sent to another.
			if previous.Preset == name && !cfg.NoAuth {
				cfg.APIKey = previous.APIKey
			}
			if cmd.Flags().Changed("base-url") {
				cfg.BaseURL = baseURL
			}
			if cmd.Flags().Changed("model") {
				cfg.Model = modelID
			}
			if cmd.Flags().Changed("header") {
				cfg.AuthHeader = header
			}
			if cmd.Flags().Changed("prefix") {
				cfg.AuthPrefix = prefix
			}
			if cmd.Flags().Changed("wire") {
				cfg.Wire = wire
			}
			if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.Model) == "" {
				return fail(2, fmt.Errorf("preset %q needs --base-url and --model", name))
			}
			cfg.Normalize()
			a.Cfg.AI = cfg
			if err := a.Cfg.Save(a.Paths.ConfigFile); err != nil {
				return err
			}
			if a.flags.JSON {
				masked := redactAIConfig(cfg)
				masked.APIKey = maskKey(cfg.APIKey)
				return a.printJSON(masked)
			}
			a.printf("provider set to %s (%s, model %s)\n", name, cfg.BaseURL, cfg.Model)
			if cfg.NoAuth {
				a.printf("this local preset ignores authentication\n")
			} else if ai.ResolveKey(cfg, a.flags.APIKey) == "" {
				a.printf("no key yet: run `clerify ai key --stdin` or set %s\n", strings.ToUpper(name)+"_API_KEY")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&baseURL, "base-url", "", "override the preset base URL")
	cmd.Flags().StringVar(&modelID, "model", "", "override the preset model")
	cmd.Flags().StringVar(&header, "header", "", "override the auth header name")
	cmd.Flags().StringVar(&prefix, "prefix", "", "override the auth prefix (e.g. Bearer)")
	cmd.Flags().StringVar(&wire, "wire", "", "openai|anthropic")
	return cmd
}

func (a *App) aiKeyCmd() *cobra.Command {
	var (
		stdin bool
		clear bool
	)
	cmd := &cobra.Command{
		Use:   "key",
		Short: "Store the API key in config.json (0600)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			cfg := a.Cfg.AI
			if clear {
				cfg.APIKey = ""
				a.Cfg.AI = cfg
				if err := a.Cfg.Save(a.Paths.ConfigFile); err != nil {
					return err
				}
				a.printf("stored key cleared\n")
				return nil
			}

			if !stdin {
				if info, err := os.Stdin.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
					return fail(2, fmt.Errorf("pipe the key and pass --stdin, or use --clear"))
				}
			}
			data, err := io.ReadAll(a.Stdin)
			if err != nil {
				return err
			}
			key := strings.TrimSpace(string(data))
			if key == "" {
				return fail(2, fmt.Errorf("empty key"))
			}
			cfg.APIKey = key
			a.Cfg.AI = cfg
			if err := a.Cfg.Save(a.Paths.ConfigFile); err != nil {
				return err
			}
			a.printf("key stored (%s)\n", maskKey(key))
			return nil
		},
	}
	cmd.Flags().BoolVar(&stdin, "stdin", false, "read the key from stdin")
	cmd.Flags().BoolVar(&clear, "clear", false, "remove the stored key")
	return cmd
}

func (a *App) aiTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test",
		Short: "Send one tiny request and report latency",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			client := a.aiClient()
			latency, err := client.Test(context.Background())
			if err != nil {
				return err
			}
			if a.flags.JSON {
				return a.printJSON(map[string]any{
					"ok":      true,
					"model":   client.Config().Model,
					"baseURL": client.Config().BaseURL,
					"ms":      latency.Milliseconds(),
				})
			}
			a.printf("ok  %s  %s  (%dms)\n", client.Config().Model, client.Config().BaseURL, latency.Milliseconds())
			return nil
		},
	}
}

func (a *App) aiModelsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "models",
		Short: "List models when the provider exposes /models",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			models, err := a.aiClient().ListModels(context.Background())
			if err != nil {
				return err
			}
			if a.flags.JSON {
				return a.printJSON(models)
			}
			for _, m := range models {
				fmt.Fprintln(a.Out, m)
			}
			return nil
		},
	}
}

func (a *App) aiShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the effective AI configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			cfg := a.Cfg.AI
			cfg.APIKey = maskKey(ai.ResolveKey(cfg, a.flags.APIKey))
			if a.flags.JSON {
				return a.printJSON(redactAIConfig(cfg))
			}
			fmt.Fprintf(a.Out, "%-16s %s\n", "preset", orDash(cfg.Preset))
			fmt.Fprintf(a.Out, "%-16s %s\n", "wire", cfg.Wire)
			fmt.Fprintf(a.Out, "%-16s %s\n", "base_url", orDash(cfg.BaseURL))
			fmt.Fprintf(a.Out, "%-16s %s\n", "model", orDash(cfg.Model))
			fmt.Fprintf(a.Out, "%-16s %s\n", "auth_header", orDash(cfg.AuthHeader))
			fmt.Fprintf(a.Out, "%-16s %s\n", "api_key", orDash(cfg.APIKey))
			fmt.Fprintf(a.Out, "%-16s %s\n", "key_env", a.aiClient().KeyEnvName())
			return nil
		},
	}
}

// ── doctor ───────────────────────────────────────────────────────────────────

type doctorCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Fatal  bool   `json:"fatal"`
}

func (a *App) doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the local setup",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}

			checks := []doctorCheck{
				a.checkWritable(),
				{Name: "store", OK: true, Detail: fmt.Sprintf("%d items, %d projects", len(a.DB.Items()), len(a.DB.Projects())), Fatal: true},
			}
			if a.DB.Recovered {
				checks = append(checks, doctorCheck{Name: "backup", OK: true, Detail: "store was recovered from store.json.bak", Fatal: false})
			}
			keyCheck := doctorCheck{Name: "api_key", OK: true, Detail: "not required for this local preset", Fatal: false}
			if !a.Cfg.AI.NoAuth {
				key := ai.ResolveKey(a.Cfg.AI, a.flags.APIKey)
				keyCheck = doctorCheck{Name: "api_key", OK: key != "", Detail: keyDetail(key, a.aiClient().KeyEnvName()), Fatal: false}
			}
			checks = append(checks,
				keyCheck,
				doctorCheck{Name: "endpoint", OK: strings.TrimSpace(a.Cfg.AI.BaseURL) != "", Detail: orDash(a.Cfg.AI.BaseURL), Fatal: false},
				a.checkPdftotext(),
			)

			ok := true
			for _, c := range checks {
				if !c.OK && c.Fatal {
					ok = false
				}
			}

			if a.flags.JSON {
				if err := a.printJSON(map[string]any{"ok": ok, "paths": a.Paths, "checks": checks}); err != nil {
					return err
				}
			} else {
				render.Table(a.Out, []string{"CHECK", "STATUS", "DETAIL"}, doctorRows(checks))
				fmt.Fprintf(a.Out, "\ndata directory: %s\n", a.Paths.Dir)
			}
			if !ok {
				return fail(1, nil)
			}
			return nil
		},
	}
}

func (a *App) checkWritable() doctorCheck {
	probe := a.Paths.Dir + "/.doctor-probe"
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return doctorCheck{Name: "data_dir", OK: false, Detail: a.Paths.Dir + ": " + err.Error(), Fatal: true}
	}
	_ = os.Remove(probe)
	return doctorCheck{Name: "data_dir", OK: true, Detail: a.Paths.Dir, Fatal: true}
}

func (a *App) checkPdftotext() doctorCheck {
	path, err := exec.LookPath("pdftotext")
	if err != nil {
		return doctorCheck{Name: "pdftotext", OK: false, Detail: "not found; PDF import falls back to the built-in parser", Fatal: false}
	}
	return doctorCheck{Name: "pdftotext", OK: true, Detail: path, Fatal: false}
}

func doctorRows(checks []doctorCheck) [][]string {
	rows := make([][]string, 0, len(checks))
	for _, c := range checks {
		status := "ok"
		if !c.OK {
			status = "warn"
			if c.Fatal {
				status = "FAIL"
			}
		}
		rows = append(rows, []string{c.Name, status, c.Detail})
	}
	return rows
}

// ── tui ──────────────────────────────────────────────────────────────────────

func (a *App) tuiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the interactive interface",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fail(2, fmt.Errorf("the interactive TUI is not implemented yet"))
		},
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func maskKey(k string) string {
	if k == "" {
		return ""
	}
	runes := []rune(k)
	if len(runes) <= 4 {
		return strings.Repeat("•", len(runes))
	}
	hidden := len(runes) - 4
	if hidden > 8 {
		hidden = 8
	}
	return strings.Repeat("•", hidden) + string(runes[len(runes)-4:])
}

// redactAIConfig masks the key and every extra header value before the config
// is printed as JSON, since extra headers can carry credentials.
func redactAIConfig(cfg ai.Config) ai.Config {
	cfg.APIKey = maskKey(cfg.APIKey)
	if cfg.ExtraHeaders != nil {
		redacted := make(map[string]string, len(cfg.ExtraHeaders))
		for k, v := range cfg.ExtraHeaders {
			redacted[k] = maskKey(v)
		}
		cfg.ExtraHeaders = redacted
	}
	return cfg
}

func keyDetail(key, envName string) string {
	if key == "" {
		return "not set; run `clerify ai key --stdin` or set " + envName
	}
	return "set (" + maskKey(key) + ")"
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

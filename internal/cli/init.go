package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/P-Baranwal/clerify/internal/ai"
)

// initProviderChoices is the short menu shown by `clerify init`. Any other
// preset name still works through --provider.
var initProviderChoices = []string{
	"openai", "anthropic", "gemini", "groq", "openrouter",
	"deepseek", "mistral", "xai", "ollama", "custom",
}

func (a *App) initCmd() *cobra.Command {
	var (
		noAI     bool
		provider string
		key      string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set up clerify in a few questions",
		Long: `Set up clerify. Everything works offline; the wizard only adds an AI
provider for scoring and planning. You can skip AI and run init again later.`,
		Example: "  clerify init\n  clerify init --no-ai\n  clerify init --provider openai --key \"$OPENAI_API_KEY\"",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.ensure(); err != nil {
				return err
			}
			return a.runInit(noAI, provider, key)
		},
	}
	cmd.Flags().BoolVar(&noAI, "no-ai", false, "skip AI setup; clerify works fully offline")
	cmd.Flags().StringVar(&provider, "provider", "", "provider name (openai, anthropic, gemini, ollama, ...)")
	cmd.Flags().StringVar(&key, "key", "", "API key (otherwise read from the terminal)")
	return cmd
}

func (a *App) runInit(noAI bool, provider, key string) error {
	if noAI {
		a.printOfflineSetup()
		return nil
	}

	if provider == "" && key == "" && !a.isInteractive() {
		a.printf("clerify init needs a terminal for the questions. Run `clerify init`\n")
		a.printf("interactively, or configure the provider directly:\n\n")
		a.printf("  clerify ai preset <name>\n")
		a.printf("  printf '%%s' \"$YOUR_KEY\" | clerify ai key --stdin\n")
		a.printf("  clerify ai test\n")
		return nil
	}

	reader := bufio.NewReader(a.Stdin)
	fmt.Fprintln(a.Out, "clerify setup")
	fmt.Fprintln(a.Out, "AI scoring is optional; capture, ls, today, done, and undo work offline.")
	fmt.Fprintln(a.Out)

	if provider == "" {
		if !a.promptYesNo(reader, "Set up AI scoring now?", true) {
			a.printOfflineSetup()
			return nil
		}
		provider = a.chooseProvider(reader)
		if provider == "" {
			return nil
		}
	}

	name := strings.ToLower(strings.TrimSpace(provider))
	cfg, ok := ai.ApplyPreset(name)
	if !ok {
		return fail(2, fmt.Errorf("unknown provider %q; known: %s", provider, strings.Join(ai.PresetNames(), ", ")))
	}

	if name == "custom" {
		cfg.BaseURL = a.promptLine(reader, "Base URL (e.g. https://api.example.com/v1): ")
		cfg.Model = a.promptLine(reader, "Model name: ")
		if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.Model) == "" {
			return fail(2, fmt.Errorf("custom provider needs a base URL and model"))
		}
	}

	if !cfg.NoAuth {
		if key == "" && a.isInteractive() {
			key, _ = a.readSecret(reader, "Paste your API key (input hidden): ")
		}
		if strings.TrimSpace(key) == "" {
			a.printf("No key entered. The provider was saved, but AI calls will fail until you add one.\n")
		} else {
			cfg.APIKey = strings.TrimSpace(key)
		}
	}

	cfg.Normalize()
	a.Cfg.AI = cfg
	if err := a.Cfg.Save(a.Paths.ConfigFile); err != nil {
		return err
	}
	a.printf("saved %s  (%s, model %s)\n", name, cfg.BaseURL, cfg.Model)

	if !cfg.NoAuth && ai.ResolveKey(cfg, a.flags.APIKey) == "" {
		a.printf("Add a key later with: clerify ai key --stdin\n")
		a.printNextSteps()
		return nil
	}

	fmt.Fprint(a.Out, "testing the connection... ")
	latency, err := a.aiClient().Test(context.Background())
	if err != nil {
		fmt.Fprintln(a.Out, "failed")
		fmt.Fprintf(a.Out, "The settings are saved, but the test failed: %v\n", err)
		a.printf("Check the key or run `clerify ai test` again.\n")
		return nil
	}
	fmt.Fprintf(a.Out, "ok (%dms)\n", latency.Milliseconds())
	a.printNextSteps()
	return nil
}

func (a *App) chooseProvider(r *bufio.Reader) string {
	fmt.Fprintln(a.Out, "Providers:")
	for i, name := range initProviderChoices {
		fmt.Fprintf(a.Out, "  %2d. %s\n", i+1, name)
	}
	answer := a.promptLine(r, "Provider [1]: ")
	if answer == "" {
		return "openai"
	}
	if n, err := strconv.Atoi(answer); err == nil && n >= 1 && n <= len(initProviderChoices) {
		return initProviderChoices[n-1]
	}
	return answer
}

func (a *App) promptLine(r *bufio.Reader, label string) string {
	fmt.Fprint(a.Out, label)
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(line)
}

func (a *App) promptYesNo(r *bufio.Reader, label string, def bool) bool {
	suffix := " [Y/n] "
	if !def {
		suffix = " [y/N] "
	}
	answer := strings.ToLower(a.promptLine(r, label+suffix))
	switch answer {
	case "":
		return def
	case "y", "yes":
		return true
	default:
		return false
	}
}

// readSecret disables terminal echo on unix while the key is typed.
func (a *App) readSecret(r *bufio.Reader, label string) (string, error) {
	fmt.Fprint(a.Out, label)
	f, ok := a.Stdin.(*os.File)
	if ok && runtime.GOOS != "windows" {
		disable := exec.Command("stty", "-echo")
		disable.Stdin = f
		_ = disable.Run()
		defer func() {
			enable := exec.Command("stty", "echo")
			enable.Stdin = f
			_ = enable.Run()
			fmt.Fprintln(a.Out)
		}()
	}
	line, err := r.ReadString('\n')
	return strings.TrimSpace(line), err
}

func (a *App) printOfflineSetup() {
	a.printf("AI is optional. These work without it:\n")
	a.printf("  clerify add \"Write report\" --due friday\n")
	a.printf("  clerify ls\n")
	a.printf("  clerify today\n")
	a.printf("  clerify done #1\n")
	a.printf("  clerify undo\n\n")
	a.printf("AI-only commands: analyze, rescore, plan --refresh, wbs.\n")
	a.printf("Run `clerify init` whenever you want to add a provider.\n")
}

func (a *App) printNextSteps() {
	a.printf("\nNext:\n")
	a.printf("  clerify add \"Write report\" --due friday\n")
	a.printf("  clerify today\n")
	a.printf("  clerify guide\n")
}

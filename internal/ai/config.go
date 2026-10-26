package ai

import (
	"os"
	"sort"
	"strings"
)

// Wire selects the request/response shape.
const (
	WireOpenAI    = "openai"
	WireAnthropic = "anthropic"
)

// Config describes one provider endpoint. Every field is overridable, which is
// what makes "any API key" work without shipping per-vendor code.
type Config struct {
	Preset             string            `json:"preset,omitempty"`
	Wire               string            `json:"wire,omitempty"`
	BaseURL            string            `json:"base_url,omitempty"`
	Model              string            `json:"model,omitempty"`
	APIKey             string            `json:"api_key,omitempty"`
	AuthHeader         string            `json:"auth_header,omitempty"`
	AuthPrefix         string            `json:"auth_prefix,omitempty"`
	ExtraHeaders       map[string]string `json:"extra_headers,omitempty"`
	ExtraBody          map[string]any    `json:"extra_body,omitempty"`
	Temperature        float64           `json:"temperature"`
	MaxTokens          int               `json:"max_tokens"`
	TimeoutSeconds     int               `json:"timeout_seconds"`
	ResponseFormatJSON string            `json:"response_format_json,omitempty"`
	MaxInputChars      int               `json:"max_input_chars,omitempty"`
	Retries            int               `json:"retries,omitempty"`
}

// Normalize fills defaults without touching values the user set.
func (c *Config) Normalize() {
	if c.Wire == "" {
		c.Wire = WireOpenAI
	}
	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = 120
	}
	if c.MaxTokens <= 0 {
		c.MaxTokens = 4096
	}
	if c.ResponseFormatJSON == "" {
		c.ResponseFormatJSON = "auto"
	}
	if c.MaxInputChars <= 0 {
		c.MaxInputChars = 30000
	}
	if c.AuthHeader == "" && c.Wire == WireOpenAI {
		c.AuthHeader = "Authorization"
		c.AuthPrefix = "Bearer"
	}
}

// Preset pre-fills an endpoint. Nothing is locked: every preset field can be
// overridden afterwards.
type Preset struct {
	Name       string
	Wire       string
	BaseURL    string
	Model      string
	AuthHeader string
	AuthPrefix string
}

var presets = map[string]Preset{
	"sarvam":     {"sarvam", WireOpenAI, "https://api.sarvam.ai/v1", "sarvam-105b", "api-subscription-key", ""},
	"openai":     {"openai", WireOpenAI, "https://api.openai.com/v1", "gpt-4o-mini", "Authorization", "Bearer"},
	"groq":       {"groq", WireOpenAI, "https://api.groq.com/openai/v1", "llama-3.3-70b-versatile", "Authorization", "Bearer"},
	"openrouter": {"openrouter", WireOpenAI, "https://openrouter.ai/api/v1", "", "Authorization", "Bearer"},
	"together":   {"together", WireOpenAI, "https://api.together.xyz/v1", "", "Authorization", "Bearer"},
	"deepseek":   {"deepseek", WireOpenAI, "https://api.deepseek.com/v1", "deepseek-chat", "Authorization", "Bearer"},
	"mistral":    {"mistral", WireOpenAI, "https://api.mistral.ai/v1", "mistral-large-latest", "Authorization", "Bearer"},
	"xai":        {"xai", WireOpenAI, "https://api.x.ai/v1", "grok-2-latest", "Authorization", "Bearer"},
	"gemini":     {"gemini", WireOpenAI, "https://generativelanguage.googleapis.com/v1beta/openai", "gemini-2.0-flash", "Authorization", "Bearer"},
	"ollama":     {"ollama", WireOpenAI, "http://localhost:11434/v1", "qwen2.5-coder:7b", "Authorization", "Bearer"},
	"lmstudio":   {"lmstudio", WireOpenAI, "http://localhost:1234/v1", "", "Authorization", "Bearer"},
	"llamacpp":   {"llamacpp", WireOpenAI, "http://localhost:8080/v1", "", "Authorization", "Bearer"},
	"vllm":       {"vllm", WireOpenAI, "http://localhost:8000/v1", "", "Authorization", "Bearer"},
	"anthropic":  {"anthropic", WireAnthropic, "https://api.anthropic.com/v1", "claude-3-5-sonnet-20240620", "x-api-key", ""},
	"custom":     {"custom", WireOpenAI, "", "", "Authorization", "Bearer"},
}

// PresetNames returns every preset name, sorted.
func PresetNames() []string {
	names := make([]string, 0, len(presets))
	for name := range presets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// LookupPreset returns a preset by name.
func LookupPreset(name string) (Preset, bool) {
	p, ok := presets[strings.ToLower(strings.TrimSpace(name))]
	return p, ok
}

// ApplyPreset returns a Config pre-filled from a preset, keeping the preset
// name so key lookup can find <PRESET>_API_KEY.
func ApplyPreset(name string) (Config, bool) {
	p, ok := LookupPreset(name)
	if !ok {
		return Config{}, false
	}
	cfg := DefaultConfig()
	cfg.Preset = p.Name
	cfg.Wire = p.Wire
	cfg.AuthHeader = p.AuthHeader
	cfg.AuthPrefix = p.AuthPrefix
	if p.BaseURL != "" {
		cfg.BaseURL = p.BaseURL
	}
	if p.Model != "" {
		cfg.Model = p.Model
	}
	return cfg, true
}

// DefaultConfig is the current app's default provider.
func DefaultConfig() Config {
	cfg := Config{
		Preset:             "sarvam",
		Wire:               WireOpenAI,
		BaseURL:            "https://api.sarvam.ai/v1",
		Model:              "sarvam-105b",
		AuthHeader:         "api-subscription-key",
		Temperature:        0.2,
		MaxTokens:          4096,
		TimeoutSeconds:     120,
		ResponseFormatJSON: "auto",
		MaxInputChars:      30000,
		ExtraBody:          map[string]any{"reasoning_effort": nil},
	}
	return cfg
}

// ResolveKey returns the API key using, in order: an explicit override, the
// CLERIFY_API_KEY environment variable, <PRESET>_API_KEY, then the config file.
func ResolveKey(cfg Config, override string) string {
	if v := strings.TrimSpace(override); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("CLERIFY_API_KEY")); v != "" {
		return v
	}
	if cfg.Preset != "" {
		env := strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(cfg.Preset)) + "_API_KEY"
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return v
		}
	}
	return strings.TrimSpace(cfg.APIKey)
}

// keyEnvName reports which environment variable would supply the key, for
// error messages and `doctor`.
func keyEnvName(cfg Config) string {
	if cfg.Preset == "" {
		return "CLERIFY_API_KEY"
	}
	return strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(cfg.Preset)) + "_API_KEY"
}

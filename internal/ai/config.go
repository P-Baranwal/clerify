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
	NoAuth             bool              `json:"no_auth,omitempty"`
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
	if c.NoAuth {
		c.AuthHeader = ""
		c.AuthPrefix = ""
		return
	}
	if c.AuthHeader == "" {
		if c.Wire == WireAnthropic {
			c.AuthHeader = "x-api-key"
			c.AuthPrefix = ""
		} else {
			c.AuthHeader = "Authorization"
			c.AuthPrefix = "Bearer"
		}
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
	NoAuth     bool
	ExtraBody  map[string]any
}

var presets = map[string]Preset{
	"sarvam": {
		Name: "sarvam", Wire: WireOpenAI, BaseURL: "https://api.sarvam.ai/v1",
		Model: "sarvam-105b", AuthHeader: "api-subscription-key",
		ExtraBody: map[string]any{"reasoning_effort": nil},
	},
	"openai": {
		Name: "openai", Wire: WireOpenAI, BaseURL: "https://api.openai.com/v1",
		Model: "gpt-4o-mini", AuthHeader: "Authorization", AuthPrefix: "Bearer",
	},
	"groq": {
		Name: "groq", Wire: WireOpenAI, BaseURL: "https://api.groq.com/openai/v1",
		Model: "llama-3.3-70b-versatile", AuthHeader: "Authorization", AuthPrefix: "Bearer",
	},
	"openrouter": {
		Name: "openrouter", Wire: WireOpenAI, BaseURL: "https://openrouter.ai/api/v1",
		AuthHeader: "Authorization", AuthPrefix: "Bearer",
	},
	"together": {
		Name: "together", Wire: WireOpenAI, BaseURL: "https://api.together.xyz/v1",
		AuthHeader: "Authorization", AuthPrefix: "Bearer",
	},
	"deepseek": {
		Name: "deepseek", Wire: WireOpenAI, BaseURL: "https://api.deepseek.com/v1",
		Model: "deepseek-chat", AuthHeader: "Authorization", AuthPrefix: "Bearer",
	},
	"mistral": {
		Name: "mistral", Wire: WireOpenAI, BaseURL: "https://api.mistral.ai/v1",
		Model: "mistral-large-latest", AuthHeader: "Authorization", AuthPrefix: "Bearer",
	},
	"xai": {
		Name: "xai", Wire: WireOpenAI, BaseURL: "https://api.x.ai/v1",
		Model: "grok-4.7", AuthHeader: "Authorization", AuthPrefix: "Bearer",
	},
	"gemini": {
		Name: "gemini", Wire: WireOpenAI, BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai",
		Model: "gemini-3.8-flash", AuthHeader: "Authorization", AuthPrefix: "Bearer",
	},
	"ollama": {
		Name: "ollama", Wire: WireOpenAI, BaseURL: "http://localhost:11434/v1",
		Model: "qwen2.5-coder:7b", NoAuth: true,
	},
	"lmstudio": {
		Name: "lmstudio", Wire: WireOpenAI, BaseURL: "http://localhost:1234/v1",
		NoAuth: true,
	},
	"llamacpp": {
		Name: "llamacpp", Wire: WireOpenAI, BaseURL: "http://localhost:8080/v1",
		NoAuth: true,
	},
	"vllm": {
		Name: "vllm", Wire: WireOpenAI, BaseURL: "http://localhost:8000/v1",
		NoAuth: true,
	},
	"anthropic": {
		Name: "anthropic", Wire: WireAnthropic, BaseURL: "https://api.anthropic.com/v1",
		Model: "claude-sonnet-5-5", AuthHeader: "x-api-key",
	},
	"custom": {
		Name: "custom", Wire: WireOpenAI, AuthHeader: "Authorization", AuthPrefix: "Bearer",
	},
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
	cfg := baseConfig()
	cfg.Preset = p.Name
	cfg.Wire = p.Wire
	cfg.AuthHeader = p.AuthHeader
	cfg.AuthPrefix = p.AuthPrefix
	cfg.NoAuth = p.NoAuth
	if p.NoAuth {
		cfg.AuthHeader = ""
		cfg.AuthPrefix = ""
	}
	if p.BaseURL != "" {
		cfg.BaseURL = p.BaseURL
	}
	if p.Model != "" {
		cfg.Model = p.Model
	}
	cfg.ExtraBody = cloneAnyMap(p.ExtraBody)
	return cfg, true
}

// DefaultConfig is the current app's default provider.
func DefaultConfig() Config {
	cfg := baseConfig()
	cfg.Preset = "sarvam"
	cfg.BaseURL = "https://api.sarvam.ai/v1"
	cfg.Model = "sarvam-105b"
	cfg.AuthHeader = "api-subscription-key"
	cfg.ExtraBody = map[string]any{"reasoning_effort": nil}
	return cfg
}

func baseConfig() Config {
	return Config{
		Wire:               WireOpenAI,
		Temperature:        0.2,
		MaxTokens:          4096,
		TimeoutSeconds:     120,
		ResponseFormatJSON: "auto",
		MaxInputChars:      30000,
	}
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
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

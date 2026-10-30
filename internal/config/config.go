// Package config holds the CLI's settings file. It is separate from store.json
// so the API key can have 0600 permissions while user data stays readable.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/P-Baranwal/clerify/internal/ai"
	"github.com/P-Baranwal/clerify/internal/atomicfile"
	"github.com/P-Baranwal/clerify/internal/model"
)

// UI holds presentation preferences.
type UI struct {
	Theme      string `json:"theme,omitempty"`
	TableStyle string `json:"table_style,omitempty"`
	DateFormat string `json:"date_format,omitempty"`
}

// Config is the whole config.json document.
type Config struct {
	AI ai.Config `json:"ai"`
	UI UI        `json:"ui"`
}

// Default returns a config pointing at the current app's default provider.
func Default() Config {
	return Config{
		AI: ai.DefaultConfig(),
		UI: UI{Theme: model.ThemeSystem, TableStyle: "plain", DateFormat: model.DateLayout},
	}
}

// Load reads config.json, returning defaults when the file does not exist.
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	// Maps are merged by encoding/json when unmarshalling into a non-nil map.
	// Clear them first so a config file can actually remove a provider field
	// such as Sarvam's reasoning_effort.
	cfg.AI.ExtraBody = nil
	cfg.AI.ExtraHeaders = nil
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	if cfg.AI.ExtraBody == nil && cfg.AI.Preset == "sarvam" {
		cfg.AI.ExtraBody = map[string]any{"reasoning_effort": nil}
	}
	cfg.AI.Normalize()
	if cfg.UI.Theme == "" {
		cfg.UI.Theme = model.ThemeSystem
	}
	if cfg.UI.DateFormat == "" {
		cfg.UI.DateFormat = model.DateLayout
	}
	return cfg, nil
}

// Save writes config.json atomically with owner-only permissions.
func (c Config) Save(path string) error {
	c.AI.Normalize()
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return atomicfile.WriteFile(path, data, 0o600)
}

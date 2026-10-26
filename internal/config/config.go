// Package config holds the CLI's settings file. It is separate from store.json
// so the API key can have 0600 permissions while user data stays readable.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/P-Baranwal/clerify/internal/ai"
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
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), fmt.Errorf("parse %s: %w", filepath.Base(path), err)
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
	f, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return nil
}

// Package globalconfig manages the user-level TOML config file controlling
// non-instance-specific behavior (currently: self-update checking).
package globalconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/caedis/gtnh-daily-updater/internal/paths"
)

// Config is the global user configuration.
type Config struct {
	AutoUpdateCheck    bool `toml:"auto_update_check"`
	IncludePrereleases bool `toml:"include_prereleases"`
}

const fileName = "config.toml"

const defaultTemplate = `# gtnh-daily-updater global config
# Set auto_update_check = true to check GitHub for new releases at startup.
# When a newer release is found, run "gtnh-daily-updater self-update" to install it.

auto_update_check = false
include_prereleases = false
`

// Path returns the absolute path to the global config file.
func Path() (string, error) {
	dir, err := paths.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// Load reads the global config file. A missing file yields a zero-value Config
// and a nil error.
func Load() (Config, error) {
	var cfg Config
	p, err := Path()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("reading %s: %w", p, err)
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing %s: %w", p, err)
	}
	return cfg, nil
}

// WriteDefaultIfMissing writes a commented default config file when one does
// not already exist. Errors are returned but callers may safely ignore them.
func WriteDefaultIfMissing() error {
	p, err := Path()
	if err != nil {
		return err
	}
	if _, err := os.Stat(p); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(defaultTemplate), 0o644)
}

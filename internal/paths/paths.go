// Package paths centralizes OS-native cache and config directory resolution
// for gtnh-daily-updater, with lazy migration from previously used
// XDG-hardcoded locations on macOS and Windows.
package paths

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/caedis/gtnh-daily-updater/internal/logging"
)

const appName = "gtnh-daily-updater"

// CacheDir returns <UserCacheDir>/gtnh-daily-updater, migrating from the
// legacy XDG_CACHE_HOME / ~/.cache location if present.
func CacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, appName)
	migrate(legacyCacheDir(), dir)
	return dir, nil
}

// ModsCacheDir returns the mod jar cache directory.
func ModsCacheDir() (string, error) {
	d, err := CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "mods"), nil
}

// LogsDir returns the log directory.
func LogsDir() (string, error) {
	d, err := CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "logs"), nil
}

// ConfigDir returns <UserConfigDir>/gtnh-daily-updater, migrating from the
// legacy XDG_CONFIG_HOME / ~/.config location if present.
func ConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, appName)
	migrate(legacyConfigDir(), dir)
	return dir, nil
}

// ProfilesDir returns the profiles directory.
func ProfilesDir() (string, error) {
	d, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "profiles"), nil
}

func legacyCacheDir() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, appName)
}

func legacyConfigDir() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, appName)
}

// migrate moves oldDir to newDir if oldDir exists, newDir does not, and the
// two paths differ. Failures are logged but non-fatal.
func migrate(oldDir, newDir string) {
	if oldDir == "" || oldDir == newDir {
		return
	}
	if _, err := os.Stat(oldDir); err != nil {
		return
	}
	if _, err := os.Stat(newDir); err == nil {
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		return
	}
	if err := os.MkdirAll(filepath.Dir(newDir), 0o755); err != nil {
		logging.Infof("  Warning: could not create %s: %v\n", filepath.Dir(newDir), err)
		return
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		logging.Infof("  Warning: could not migrate %s -> %s: %v\n", oldDir, newDir, err)
		return
	}
	logging.Infof("Migrated %s -> %s\n", oldDir, newDir)
}

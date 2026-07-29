package profile

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/caedis/gtnh-daily-updater/internal/paths"
)

// Profile holds saveable CLI options. All fields are pointers so we can
// distinguish "not set" from zero values.
type Profile struct {
	InstanceDir    *string `toml:"instance-dir,omitempty"`
	Side           *string `toml:"side,omitempty"`
	Mode           *string `toml:"mode,omitempty"`
	Concurrency    *int    `toml:"concurrency,omitempty"`
	Latest         *bool   `toml:"latest,omitempty"`
	CacheDir       *string `toml:"cache-dir,omitempty"`
	NoCache        *bool   `toml:"no-cache,omitempty"`
	NoVersionStamp *bool   `toml:"no-version-stamp,omitempty"`
	Verbose        *bool   `toml:"verbose,omitempty"`
	LogFile        *string `toml:"log-file,omitempty"`
}

// Dir returns the profiles directory under the OS-native user config dir.
func Dir() string {
	d, err := paths.ProfilesDir()
	if err != nil {
		return ""
	}
	return d
}

// Load reads a named profile from the profiles directory.
func Load(name string) (*Profile, error) {
	path := filepath.Join(Dir(), name+".toml")
	var p Profile
	if _, err := toml.DecodeFile(path, &p); err != nil {
		return nil, fmt.Errorf("loading profile %q: %w", name, err)
	}

	// Backward-compatibility migration:
	// old "mode" represented side (client/server).
	if p.Side == nil && p.Mode != nil {
		switch strings.ToLower(strings.TrimSpace(*p.Mode)) {
		case "client", "server":
			p.Side = p.Mode
			p.Mode = nil
		}
	}

	// Expand a leading "~" in stored path values; profiles are hand-editable
	// and a quoted "~" in the source is never expanded by the shell.
	for _, ptr := range []*string{p.InstanceDir, p.CacheDir, p.LogFile} {
		if ptr != nil {
			*ptr = paths.ExpandTilde(*ptr)
		}
	}

	return &p, nil
}

// Save writes a profile to the profiles directory, creating it if needed.
func Save(name string, p *Profile) error {
	dir := Dir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating profiles directory: %w", err)
	}
	path := filepath.Join(dir, name+".toml")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating profile file: %w", err)
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(p); err != nil {
		return fmt.Errorf("encoding profile: %w", err)
	}
	return nil
}

// List returns the names of all saved profiles.
func List() ([]string, error) {
	dir := Dir()

	var names []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if path == dir {
			return nil
		}
		if d.IsDir() {
			return filepath.SkipDir
		}
		if strings.HasSuffix(d.Name(), ".toml") {
			names = append(names, strings.TrimSuffix(d.Name(), ".toml"))
		}
		return nil
	})
	if err != nil && os.IsNotExist(err) {
		return nil, nil
	}
	return names, err
}

// Delete removes a named profile.
func Delete(name string) error {
	path := filepath.Join(Dir(), name+".toml")
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("deleting profile %q: %w", name, err)
	}
	return nil
}

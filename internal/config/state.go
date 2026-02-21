package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const StateFile = ".gtnh-daily-updater.json"

type LocalState struct {
	Mode          string                  `json:"mode"`
	ManifestDate  string                  `json:"manifest_date"`
	ConfigVersion string                  `json:"config_version"`
	ConfigHashes  map[string]string       `json:"config_hashes"`
	Mods          map[string]InstalledMod `json:"mods"`
	ExcludeMods   []string                `json:"exclude_mods,omitempty"`
	ExtraMods     map[string]ExtraModSpec `json:"extra_mods,omitempty"`
}

type ExtraModSpec struct {
	Version string `json:"version,omitempty"`
	Source  string `json:"source,omitempty"`
	Side    string `json:"side,omitempty"`
}

type InstalledMod struct {
	Version  string `json:"version"`
	Filename string `json:"filename"`
	Side     string `json:"side"`
}

// Load reads the local state from the instance directory.
func Load(instanceDir string) (*LocalState, error) {
	path := filepath.Join(instanceDir, StateFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no %s found - run 'init' first", StateFile)
		}
		return nil, fmt.Errorf("reading state: %w", err)
	}

	var state LocalState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing state: %w", err)
	}

	if state.ConfigHashes == nil {
		state.ConfigHashes = make(map[string]string)
	}
	if state.Mods == nil {
		state.Mods = make(map[string]InstalledMod)
	}

	return &state, nil
}

// GameDir returns the directory containing mods/ and config/.
// On Prism/MultiMC clients, this is <instanceDir>/.minecraft/.
// On servers and other layouts, this is just instanceDir.
func GameDir(instanceDir string) string {
	dotMC := filepath.Join(instanceDir, ".minecraft")
	if info, err := os.Stat(dotMC); err == nil && info.IsDir() {
		return dotMC
	}
	return instanceDir
}

// Save writes the local state to the instance directory.
func (s *LocalState) Save(instanceDir string) error {
	path := filepath.Join(instanceDir, StateFile)

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing state: %w", err)
	}

	return nil
}

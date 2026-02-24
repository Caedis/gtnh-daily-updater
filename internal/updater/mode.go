package updater

import (
	"strings"

	"github.com/caedis/gtnh-daily-updater/internal/config"
	"github.com/caedis/gtnh-daily-updater/internal/manifest"
)

func inferModeFromConfigVersion(configVersion string) string {
	if strings.Contains(strings.ToLower(configVersion), manifest.ModeExperimental) {
		return manifest.ModeExperimental
	}
	return manifest.ModeDaily
}

func resolveMode(state *config.LocalState) string {
	if state == nil {
		return manifest.ModeDaily
	}
	if state.Mode != "" {
		if mode, err := manifest.ParseMode(state.Mode); err == nil {
			return mode
		}
	}
	if state.Mode == "" && strings.Contains(strings.ToLower(state.ConfigVersion), manifest.ModeExperimental) {
		return manifest.ModeExperimental
	}
	return inferModeFromConfigVersion(state.ConfigVersion)
}

func resolveInitMode(configVersion, modeFlag string) (string, error) {
	if modeFlag != "" {
		return manifest.ParseMode(modeFlag)
	}
	return inferModeFromConfigVersion(configVersion), nil
}

// DetectMode returns the manifest mode inferred from local state.
func DetectMode(instanceDir string) (string, error) {
	state, err := config.Load(instanceDir)
	if err != nil {
		return "", err
	}
	return resolveMode(state), nil
}

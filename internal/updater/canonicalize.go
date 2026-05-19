package updater

import (
	"strings"

	"github.com/caedis/gtnh-daily-updater/internal/config"
	"github.com/caedis/gtnh-daily-updater/internal/manifest"
)

// canonicalizeStateNames rewrites case-sensitive name fields in state so they
// match the manifest's casing. This lets users type `exclude add journeymap`
// or `extra add JOURNEYMAP` and have the entry match a manifest mod named
// "JourneyMap". Names absent from the manifest are left unchanged.
//
// Returns true if any name was rewritten so the caller can persist the
// canonicalized state.
func canonicalizeStateNames(state *config.LocalState, m *manifest.DailyManifest) bool {
	if state == nil || m == nil {
		return false
	}

	manifestCanon := make(map[string]string, len(m.GithubMods)+len(m.ExternalMods))
	for k := range m.GithubMods {
		manifestCanon[strings.ToLower(k)] = k
	}
	for k := range m.ExternalMods {
		manifestCanon[strings.ToLower(k)] = k
	}

	changed := false

	// state.Mods keys
	if len(state.Mods) > 0 {
		newMods := make(map[string]config.InstalledMod, len(state.Mods))
		for k, v := range state.Mods {
			canonical := k
			if c, ok := manifestCanon[strings.ToLower(k)]; ok && c != k {
				canonical = c
				changed = true
			}
			newMods[canonical] = v
		}
		state.Mods = newMods
	}

	// state.ExcludeMods entries
	for i, name := range state.ExcludeMods {
		if c, ok := manifestCanon[strings.ToLower(name)]; ok && c != name {
			state.ExcludeMods[i] = c
			changed = true
		}
	}

	// state.ExtraMods keys
	if len(state.ExtraMods) > 0 {
		newExtras := make(map[string]config.ExtraModSpec, len(state.ExtraMods))
		for k, v := range state.ExtraMods {
			canonical := k
			if c, ok := manifestCanon[strings.ToLower(k)]; ok && c != k {
				canonical = c
				changed = true
			}
			newExtras[canonical] = v
		}
		state.ExtraMods = newExtras
	}

	return changed
}

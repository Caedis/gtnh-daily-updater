package updater

import (
	"testing"

	"github.com/caedis/gtnh-daily-updater/internal/config"
	"github.com/caedis/gtnh-daily-updater/internal/manifest"
)

func TestCanonicalizeStateNames(t *testing.T) {
	state := &config.LocalState{
		Mods: map[string]config.InstalledMod{
			"journeymap": {Version: "old", Side: "CLIENT"},
			"OtherMod":   {Version: "1.0", Side: "BOTH"},
		},
		ExcludeMods: []string{"JOURNEYMAP", "unknownmod"},
		ExtraMods: map[string]config.ExtraModSpec{
			"JourneyMap": {Source: "github:TeamJM/journeymap-legacy"},
			"customMod":  {Source: "https://example.com/x.jar"},
		},
	}
	m := &manifest.DailyManifest{
		GithubMods: map[string]manifest.ModInfo{
			"JourneyMap": {Version: "5.2.15", Side: "CLIENT"},
		},
		ExternalMods: map[string]manifest.ModInfo{
			"OtherMod": {Version: "1.0", Side: "BOTH"},
		},
	}

	changed := canonicalizeStateNames(state, m)
	if !changed {
		t.Fatal("expected canonicalizeStateNames to report changes")
	}

	if _, ok := state.Mods["JourneyMap"]; !ok {
		t.Errorf("state.Mods key not canonicalized: %v", state.Mods)
	}
	if _, ok := state.Mods["journeymap"]; ok {
		t.Errorf("old lowercase key still present in state.Mods")
	}
	if _, ok := state.Mods["OtherMod"]; !ok {
		t.Errorf("unchanged matching key was lost: %v", state.Mods)
	}

	if state.ExcludeMods[0] != "JourneyMap" {
		t.Errorf("ExcludeMods[0]=%q, want JourneyMap", state.ExcludeMods[0])
	}
	if state.ExcludeMods[1] != "unknownmod" {
		t.Errorf("ExcludeMods[1] should be unchanged when not in manifest, got %q", state.ExcludeMods[1])
	}

	if _, ok := state.ExtraMods["JourneyMap"]; !ok {
		t.Errorf("ExtraMods key not canonicalized: %v", state.ExtraMods)
	}
	if _, ok := state.ExtraMods["customMod"]; !ok {
		t.Errorf("ExtraMods key absent from manifest should be unchanged")
	}
}

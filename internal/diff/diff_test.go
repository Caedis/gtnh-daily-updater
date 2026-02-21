package diff

import (
	"testing"

	"github.com/caedis/gtnh-daily-updater/internal/config"
	"github.com/caedis/gtnh-daily-updater/internal/manifest"
)

func TestCompute_WithExcludesAndExtras(t *testing.T) {
	state := &config.LocalState{
		Mode: "client",
		Mods: map[string]config.InstalledMod{
			"alpha":              {Version: "1.0.0", Side: "BOTH"},
			"beta":               {Version: "1.0.0", Side: "CLIENT"},
			"gamma":              {Version: "1.0.0", Side: "BOTH"},
			"orphan":             {Version: "1.0.0", Side: "BOTH"},
			"extra-kept":         {Version: "9.0.0", Side: "BOTH"},
			"manifest-and-extra": {Version: "0.9.0", Side: "BOTH"},
		},
	}

	m := &manifest.DailyManifest{
		GithubMods: map[string]manifest.ModInfo{
			"alpha":              {Version: "2.0.0", Side: "BOTH"},
			"beta":               {Version: "1.0.0", Side: "CLIENT"},
			"gamma":              {Version: "1.0.0", Side: "BOTH"},
			"new-client":         {Version: "1.0.0", Side: "CLIENT"},
			"server-only":        {Version: "1.0.0", Side: "SERVER"},
			"manifest-and-extra": {Version: "1.0.0", Side: "BOTH"},
		},
	}

	opts := &ComputeOptions{
		ExcludeMods: []string{"beta"},
		ExtraMods: map[string]ResolvedExtraMod{
			"extra-new":          {Version: "5.0.0", Side: "BOTH"},
			"extra-kept":         {Version: "9.0.0", Side: "BOTH"},
			"manifest-and-extra": {Version: "2.0.0", Side: "BOTH"},
		},
	}

	changes := Compute(state, m, opts)
	got := make(map[string]ModChange, len(changes))
	for _, c := range changes {
		got[c.Name] = c
	}

	assertChange(t, got, "alpha", Updated, "1.0.0", "2.0.0")
	assertChange(t, got, "beta", Removed, "1.0.0", "")
	assertChange(t, got, "gamma", Unchanged, "1.0.0", "1.0.0")
	assertChange(t, got, "new-client", Added, "", "1.0.0")
	assertChange(t, got, "orphan", Removed, "1.0.0", "")
	assertChange(t, got, "extra-kept", Unchanged, "9.0.0", "9.0.0")
	assertChange(t, got, "extra-new", Added, "", "5.0.0")

	// Manifest takes precedence over same-named extra unless excluded.
	assertChange(t, got, "manifest-and-extra", Updated, "0.9.0", "1.0.0")

	if _, ok := got["server-only"]; ok {
		t.Fatalf("server-only mod should not be included in client mode")
	}

	added, removed, updated, unchanged := Summary(changes)
	if added != 2 || removed != 2 || updated != 2 || unchanged != 2 {
		t.Fatalf("unexpected summary counts: added=%d removed=%d updated=%d unchanged=%d", added, removed, updated, unchanged)
	}
}

func TestCompute_NilOptionsAndSideFiltering(t *testing.T) {
	state := &config.LocalState{
		Mode: "server",
		Mods: map[string]config.InstalledMod{
			"common": {Version: "1.0.0", Side: "BOTH"},
		},
	}
	m := &manifest.DailyManifest{
		GithubMods: map[string]manifest.ModInfo{
			"common":      {Version: "1.0.0", Side: "BOTH"},
			"client-only": {Version: "1.0.0", Side: "CLIENT"},
		},
	}

	changes := Compute(state, m, nil)
	if len(changes) != 1 {
		t.Fatalf("expected only one change, got %d", len(changes))
	}
	if changes[0].Name != "common" || changes[0].Type != Unchanged {
		t.Fatalf("unexpected change: %+v", changes[0])
	}
}

func assertChange(t *testing.T, changes map[string]ModChange, name string, typ ChangeType, oldVersion, newVersion string) {
	t.Helper()
	c, ok := changes[name]
	if !ok {
		t.Fatalf("expected change for %q", name)
	}
	if c.Type != typ {
		t.Fatalf("unexpected type for %q: got=%v want=%v", name, c.Type, typ)
	}
	if c.OldVersion != oldVersion {
		t.Fatalf("unexpected old version for %q: got=%q want=%q", name, c.OldVersion, oldVersion)
	}
	if c.NewVersion != newVersion {
		t.Fatalf("unexpected new version for %q: got=%q want=%q", name, c.NewVersion, newVersion)
	}
}

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingState(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	_, err := Load(tmp)
	if err == nil {
		t.Fatalf("expected error when state file is missing")
	}
	if !strings.Contains(err.Error(), "run 'init' first") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSaveAndLoadInitializesMaps(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	s := &LocalState{
		Side:          "client",
		ManifestDate:  "2026-01-01",
		ConfigVersion: "2.7.4",
		// Intentionally nil maps to verify Load defaults.
	}

	if err := s.Save(tmp); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.Mods == nil {
		t.Fatalf("Mods should be initialized")
	}
	if loaded.Side != "client" || loaded.ConfigVersion != "2.7.4" {
		t.Fatalf("unexpected loaded state: %+v", loaded)
	}
}

func TestLoadMigratesLegacyModeAndManifestTrack(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	legacy := `{
  "mode": "client",
  "manifest_track": "experimental",
  "manifest_date": "2026-01-01",
  "config_version": "2.7.4",
  "config_hashes": {},
  "mods": {}
}`
	if err := os.WriteFile(filepath.Join(tmp, StateFile), []byte(legacy), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	loaded, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Side != "client" {
		t.Fatalf("Side=%q want client", loaded.Side)
	}
	if loaded.Mode != "experimental" {
		t.Fatalf("Mode=%q want experimental", loaded.Mode)
	}
}

func TestGameDir(t *testing.T) {
	t.Parallel()

	t.Run("uses dot minecraft when present", func(t *testing.T) {
		tmp := t.TempDir()
		dotMC := filepath.Join(tmp, ".minecraft")
		if err := os.MkdirAll(dotMC, 0o755); err != nil {
			t.Fatalf("MkdirAll failed: %v", err)
		}

		if got := GameDir(tmp); got != dotMC {
			t.Fatalf("GameDir=%q want=%q", got, dotMC)
		}
	})

	t.Run("falls back to instance dir", func(t *testing.T) {
		tmp := t.TempDir()
		if got := GameDir(tmp); got != tmp {
			t.Fatalf("GameDir=%q want=%q", got, tmp)
		}
	})
}

func TestSaveAndLoadDisplayVersion(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	s := &LocalState{
		Side:           "client",
		ManifestDate:   "2026-07-28",
		ConfigVersion:  "2.9.0-nightly-2026-07-28",
		DisplayVersion: "2.9.x (Daily 648) - 2026-07-28",
	}
	if err := s.Save(tmp); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.DisplayVersion != "2.9.x (Daily 648) - 2026-07-28" {
		t.Errorf("DisplayVersion = %q, want %q", loaded.DisplayVersion, "2.9.x (Daily 648) - 2026-07-28")
	}
}

func TestLoadStateWithoutDisplayVersion(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	older := `{
  "side": "client",
  "manifest_date": "2026-07-27",
  "config_version": "2.9.0-nightly-2026-07-27",
  "mods": {}
}`
	if err := os.WriteFile(filepath.Join(tmp, StateFile), []byte(older), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.DisplayVersion != "" {
		t.Errorf("DisplayVersion = %q, want empty for a pre-feature state file", loaded.DisplayVersion)
	}
}

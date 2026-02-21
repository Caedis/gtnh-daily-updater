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
		Mode:          "client",
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

	if loaded.ConfigHashes == nil {
		t.Fatalf("ConfigHashes should be initialized")
	}
	if loaded.Mods == nil {
		t.Fatalf("Mods should be initialized")
	}
	if loaded.Mode != "client" || loaded.ConfigVersion != "2.7.4" {
		t.Fatalf("unexpected loaded state: %+v", loaded)
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

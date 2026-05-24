package profile

import (
	"path/filepath"
	"testing"
)

func TestLoadExpandsTildePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	inst := "~/instances/foo"
	cache := "~/cache/mods"
	logf := "~/logs/run.log"
	p := &Profile{InstanceDir: &inst, CacheDir: &cache, LogFile: &logf}
	if err := Save("tilde", p); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load("tilde")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	wantInst := filepath.Join(home, "instances", "foo")
	if got.InstanceDir == nil || *got.InstanceDir != wantInst {
		t.Errorf("InstanceDir = %v, want %q", got.InstanceDir, wantInst)
	}
	wantCache := filepath.Join(home, "cache", "mods")
	if got.CacheDir == nil || *got.CacheDir != wantCache {
		t.Errorf("CacheDir = %v, want %q", got.CacheDir, wantCache)
	}
	wantLog := filepath.Join(home, "logs", "run.log")
	if got.LogFile == nil || *got.LogFile != wantLog {
		t.Errorf("LogFile = %v, want %q", got.LogFile, wantLog)
	}
}

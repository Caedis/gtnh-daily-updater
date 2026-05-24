package cmd

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestExpandFlagPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	instanceDir = "~/.local/share/instances/foo"
	logFile = "~/logs/run.log"
	cacheDir = "~/cache"
	cacheDirAll = ".local/relative"
	t.Cleanup(func() {
		instanceDir, logFile, cacheDir, cacheDirAll = ".", "", "", ""
	})

	expandFlagPaths()

	if want := filepath.Join(home, ".local", "share", "instances", "foo"); instanceDir != want {
		t.Errorf("instanceDir = %q, want %q", instanceDir, want)
	}
	if want := filepath.Join(home, "logs", "run.log"); logFile != want {
		t.Errorf("logFile = %q, want %q", logFile, want)
	}
	if want := filepath.Join(home, "cache"); cacheDir != want {
		t.Errorf("cacheDir = %q, want %q", cacheDir, want)
	}
	if cacheDirAll != ".local/relative" {
		t.Errorf("cacheDirAll = %q, want unchanged relative path", cacheDirAll)
	}
}

func TestUsageArgsWrapsValidationErrors(t *testing.T) {
	wrapped := usageArgs(cobra.ExactArgs(1))
	cmd := &cobra.Command{Use: "test"}

	if err := wrapped(cmd, []string{"ok"}); err != nil {
		t.Fatalf("usageArgs returned unexpected error for valid args: %v", err)
	}

	err := wrapped(cmd, nil)
	if err == nil {
		t.Fatalf("usageArgs should return an error for invalid args")
	}
	if !isUsageError(err) {
		t.Fatalf("usageArgs error should be marked as usage error: %v", err)
	}
}

func TestIsUsageError(t *testing.T) {
	if !isUsageError(wrapUsageError(errors.New("bad args"))) {
		t.Fatalf("wrapped usage error not detected")
	}
	if !isUsageError(errors.New(`unknown command "foo" for "gtnh-daily-updater"`)) {
		t.Fatalf("unknown command error should be treated as usage error")
	}
	if isUsageError(errors.New("runtime failure")) {
		t.Fatalf("runtime failure should not be treated as usage error")
	}
}

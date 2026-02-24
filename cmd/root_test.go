package cmd

import (
	"errors"
	"testing"

	"github.com/spf13/cobra"
)

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

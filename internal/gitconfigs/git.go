package gitconfigs

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// runGit runs a git command in the given working directory.
func runGit(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %v: %w\n%s", args, err, stderr.String())
	}
	return nil
}

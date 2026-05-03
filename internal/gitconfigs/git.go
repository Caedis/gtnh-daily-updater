package gitconfigs

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
"github.com/caedis/gtnh-daily-updater/internal/logging"
)

// IsGitAvailable reports whether git is available on PATH.
func IsGitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// runGit runs a git command in the given working directory.
func runGit(ctx context.Context, dir string, args ...string) error {
	logging.Debugf("git %v (dir=%s)\n", args, dir)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		logging.Debugf("git %v failed: %v\n%s", args, err, out.String())
		return fmt.Errorf("git %v: %w\n%s", args, err, out.String())
	}
	return nil
}

// runGitOutput runs a git command and returns its combined stdout+stderr.
func runGitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	logging.Debugf("git %v (dir=%s)\n", args, dir)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		logging.Debugf("git %v failed: %v\n%s", args, err, out.String())
		return "", fmt.Errorf("git %v: %w\n%s", args, err, out.String())
	}
	return out.String(), nil
}

// logStagedDiff logs a stat summary of staged changes to the debug log.
func logStagedDiff(ctx context.Context, dir string) {
	stat, err := runGitOutput(ctx, dir, "diff", "--cached", "--stat")
	if err != nil || stat == "" {
		return
	}
	logging.Infof("staged diff:\n%s", stat)
}

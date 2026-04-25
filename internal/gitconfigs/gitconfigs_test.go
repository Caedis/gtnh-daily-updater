package gitconfigs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gitInit(t *testing.T, dir string) {
	t.Helper()
	ctx := context.Background()
	if err := runGit(ctx, dir, "init", "-b", "main"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := runGit(ctx, dir, "config", "user.name", "test"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "config", "user.email", "test@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "config", "commit.gpgsign", "false"); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setupConflictRepo builds a repo with `local` and `pack` branches diverged
// such that merging pack into local produces both a UD and a DU conflict.
func setupConflictRepo(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	gitInit(t, dir)

	// Base commit with two files
	writeFile(t, filepath.Join(dir, "config", "kept.cfg"), "base\n")
	writeFile(t, filepath.Join(dir, "config", "shared.cfg"), "base\n")
	if err := runGit(ctx, dir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "commit", "-m", "base"); err != nil {
		t.Fatal(err)
	}

	// pack branch: delete kept.cfg, modify shared.cfg
	if err := runGit(ctx, dir, "checkout", "-b", "pack"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "config", "kept.cfg")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "config", "shared.cfg"), "pack-version\n")
	if err := runGit(ctx, dir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "commit", "-m", "pack changes"); err != nil {
		t.Fatal(err)
	}

	// local branch: modify kept.cfg, delete shared.cfg
	if err := runGit(ctx, dir, "checkout", "main"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "checkout", "-b", "local"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "config", "kept.cfg"), "local-edit\n")
	if err := os.Remove(filepath.Join(dir, "config", "shared.cfg")); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "commit", "-m", "local changes"); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestResolveModifyDeleteConflicts(t *testing.T) {
	if !IsGitAvailable() {
		t.Skip("git not available")
	}
	ctx := context.Background()
	dir := setupConflictRepo(t)

	// Squash-merge pack into local; expect modify/delete conflicts.
	_ = runGit(ctx, dir, "merge", "--squash", "-X", "theirs", "pack")

	if err := resolveModifyDeleteConflicts(ctx, dir); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// Status should show no unmerged paths.
	out, err := runGitOutput(ctx, dir, "status", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if len(line) >= 2 && strings.ContainsAny(line[:2], "U") {
			t.Fatalf("unmerged path remains: %q", line)
		}
	}

	// Commit and assert worktree state: kept.cfg gone (pack deleted),
	// shared.cfg has pack content (pack kept).
	if err := runGit(ctx, dir, "commit", "-m", "merged"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config", "kept.cfg")); !os.IsNotExist(err) {
		t.Fatalf("kept.cfg should be deleted, stat err=%v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "config", "shared.cfg"))
	if err != nil {
		t.Fatalf("read shared.cfg: %v", err)
	}
	if string(got) != "pack-version\n" {
		t.Fatalf("shared.cfg = %q, want pack-version", got)
	}
}

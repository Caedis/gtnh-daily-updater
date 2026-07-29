package gitconfigs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caedis/gtnh-daily-updater/internal/versionstamp"
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

// setupRenameRenameRepo creates a repo where two files are renamed in swapped
// order on each branch, producing AA conflicts: both sides add content at the
// same destination paths (from different sources). This mirrors the GTNH quest
// rename pattern where the pack swaps file names across a questline reorganisation.
func setupRenameRenameRepo(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	gitInit(t, dir)

	// Base: two quest files
	writeFile(t, filepath.Join(dir, "config", "a.json"), "content-a\n")
	writeFile(t, filepath.Join(dir, "config", "b.json"), "content-b\n")
	if err := runGit(ctx, dir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "commit", "-m", "base"); err != nil {
		t.Fatal(err)
	}

	// pack branch: a→x.json, b→y.json (pack's naming)
	if err := runGit(ctx, dir, "checkout", "-b", "pack"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "mv", "config/a.json", "config/x.json"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "mv", "config/b.json", "config/y.json"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "config", "x.json"), "pack-x\n")
	writeFile(t, filepath.Join(dir, "config", "y.json"), "pack-y\n")
	if err := runGit(ctx, dir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "commit", "-m", "pack renames"); err != nil {
		t.Fatal(err)
	}

	// local branch: a→y.json, b→x.json (swapped — both sides add to x and y)
	if err := runGit(ctx, dir, "checkout", "main"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "checkout", "-b", "local"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "mv", "config/a.json", "config/y.json"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "mv", "config/b.json", "config/x.json"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "commit", "-m", "local renames"); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestResolveRenameRenameConflicts(t *testing.T) {
	if !IsGitAvailable() {
		t.Skip("git not available")
	}
	ctx := context.Background()
	dir := setupRenameRenameRepo(t)

	// Merge produces AA conflicts that -X theirs cannot resolve.
	_ = runGit(ctx, dir, "merge", "--squash", "-X", "theirs", "pack")

	if err := resolveRemainingConflicts(ctx, dir); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if err := runGit(ctx, dir, "commit", "-m", "merged"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Pack wins: x.json and y.json should have pack content.
	gotX, err := os.ReadFile(filepath.Join(dir, "config", "x.json"))
	if err != nil {
		t.Fatalf("x.json should exist: %v", err)
	}
	if strings.TrimSpace(string(gotX)) != "pack-x" {
		t.Fatalf("x.json = %q, want pack-x", gotX)
	}
	gotY, err := os.ReadFile(filepath.Join(dir, "config", "y.json"))
	if err != nil {
		t.Fatalf("y.json should exist: %v", err)
	}
	if strings.TrimSpace(string(gotY)) != "pack-y" {
		t.Fatalf("y.json = %q, want pack-y", gotY)
	}
}

// setupBothModifiedRepo creates a repo where both branches modify the same file
// at the same lines, producing a UU conflict that -X theirs cannot auto-resolve
// (simulated here by merging without -X theirs).
func setupBothModifiedRepo(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	gitInit(t, dir)

	writeFile(t, filepath.Join(dir, "config", "shared.cfg"), "base\n")
	if err := runGit(ctx, dir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "commit", "-m", "base"); err != nil {
		t.Fatal(err)
	}

	if err := runGit(ctx, dir, "checkout", "-b", "pack"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "config", "shared.cfg"), "pack-version\n")
	if err := runGit(ctx, dir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "commit", "-m", "pack change"); err != nil {
		t.Fatal(err)
	}

	if err := runGit(ctx, dir, "checkout", "main"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "checkout", "-b", "local"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "config", "shared.cfg"), "local-edit\n")
	if err := runGit(ctx, dir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "commit", "-m", "local change"); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestResolveBothModifiedConflicts(t *testing.T) {
	if !IsGitAvailable() {
		t.Skip("git not available")
	}
	ctx := context.Background()
	dir := setupBothModifiedRepo(t)

	// Plain squash merge (no -X theirs) leaves a UU conflict.
	_ = runGit(ctx, dir, "merge", "--squash", "pack")

	if err := resolveRemainingConflicts(ctx, dir); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if err := runGit(ctx, dir, "commit", "-m", "merged"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "config", "shared.cfg"))
	if err != nil {
		t.Fatalf("read shared.cfg: %v", err)
	}
	if strings.TrimSpace(string(got)) != "pack-version" {
		t.Fatalf("shared.cfg = %q, want pack-version", got)
	}
}

// TestMergeAdvancesBaseNoDuplication reproduces the config-churn bug. The clone
// point (v1) lacks a config block; the pack adds it at v2 (and leaves it
// untouched at v3); the game reorders that block on every launch. With a squash
// merge the base stays frozen at v1, so on the v3 update the pack's copy of the
// block (relative to a base that lacks it) is re-added beside the player's moved
// copy — two blocks. mergePackVersion uses a real merge, advancing the base to
// v2, so the v3 update sees no pack change and keeps a single block.
func TestMergeAdvancesBaseNoDuplication(t *testing.T) {
	if !IsGitAvailable() {
		t.Skip("git not available")
	}
	ctx := context.Background()
	dir := t.TempDir()
	gitInit(t, dir)

	cfg := filepath.Join(dir, "config", "Client.cfg")
	noBlock := "header\n\ntail=on\n"                            // v1: clone point, no block
	packBlock := "header\n\nblock {\n    val=1\n}\n\ntail=on\n" // v2/v3: pack adds block
	gameForm := "block {\n    val=1\n}\n\nheader\n\ntail=on\n"  // game reorders block to top

	// v1: clone point without the block.
	writeFile(t, cfg, noBlock)
	if err := runGit(ctx, dir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "commit", "-m", "pack v1"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "tag", "v1"); err != nil {
		t.Fatal(err)
	}

	// v2: pack introduces the block. v3: pack leaves it untouched.
	writeFile(t, cfg, packBlock)
	if err := runGit(ctx, dir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "commit", "-m", "pack v2 adds block"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "tag", "v2"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "config", "other.cfg"), "v3\n") // unrelated change so v3 differs
	if err := runGit(ctx, dir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "commit", "-m", "pack v3"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "tag", "v3"); err != nil {
		t.Fatal(err)
	}

	// local branch starts at the clone point (v1).
	if err := runGit(ctx, dir, "checkout", "-b", "local", "v1"); err != nil {
		t.Fatal(err)
	}

	blockCount := func() int {
		t.Helper()
		b, err := os.ReadFile(cfg)
		if err != nil {
			t.Fatalf("read cfg: %v", err)
		}
		return strings.Count(string(b), "block {")
	}
	gameRewriteAndSnapshot := func() {
		t.Helper()
		writeFile(t, cfg, gameForm)
		if err := runGit(ctx, dir, "add", "-A"); err != nil {
			t.Fatal(err)
		}
		if err := runGit(ctx, dir, "commit", "-m", "Snapshot player changes"); err != nil {
			t.Fatal(err)
		}
	}

	// Round 1: update to v2 (pack adds the block), then the game reorders it.
	if err := mergePackVersion(ctx, dir, "v2", "Update configs to v2"); err != nil {
		t.Fatalf("merge v2: %v", err)
	}
	if n := blockCount(); n != 1 {
		t.Fatalf("after v2 update: block count = %d, want 1", n)
	}
	gameRewriteAndSnapshot()

	// Round 2: update to v3. Pack never touched Client.cfg since v2. A frozen
	// base (v1, no block) would re-add the pack's block beside the player's
	// reordered one (count 2); a real merge advances the base to v2 (count 1).
	if err := mergePackVersion(ctx, dir, "v3", "Update configs to v3"); err != nil {
		t.Fatalf("merge v3: %v", err)
	}
	if n := blockCount(); n != 1 {
		t.Fatalf("after v3 update: block count = %d, want 1 (duplication regression)", n)
	}

	// The merge-base must have advanced past the clone point (v1) to v3.
	base, err := runGitOutput(ctx, dir, "merge-base", "local", "v3")
	if err != nil {
		t.Fatal(err)
	}
	v3, err := runGitOutput(ctx, dir, "rev-parse", "v3^{commit}")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(base) != strings.TrimSpace(v3) {
		t.Fatalf("merge-base = %s, want v3 %s (base did not advance)", strings.TrimSpace(base), strings.TrimSpace(v3))
	}
}

// TestEnsureBaseRecordedRebaselinesSquashRepo simulates an existing repo built
// by the old squash merges (no pack tag in its ancestry, base frozen at the
// clone root). ensureBaseRecorded should graft the previously applied tag into
// history so the next real merge bases off it — yielding a single block instead
// of the duplicate the frozen base would produce.
func TestEnsureBaseRecordedRebaselinesSquashRepo(t *testing.T) {
	if !IsGitAvailable() {
		t.Skip("git not available")
	}
	ctx := context.Background()
	dir := t.TempDir()
	gitInit(t, dir)

	cfg := filepath.Join(dir, "config", "Client.cfg")
	noBlock := "header\n\ntail=on\n"
	packBlock := "header\n\nblock {\n    val=1\n}\n\ntail=on\n"
	gameForm := "block {\n    val=1\n}\n\nheader\n\ntail=on\n"

	commit := func(msg string) {
		t.Helper()
		if err := runGit(ctx, dir, "add", "-A"); err != nil {
			t.Fatal(err)
		}
		if err := runGit(ctx, dir, "commit", "-m", msg); err != nil {
			t.Fatal(err)
		}
	}

	writeFile(t, cfg, noBlock)
	commit("pack v1")
	if err := runGit(ctx, dir, "tag", "v1"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, cfg, packBlock)
	commit("pack v2 adds block")
	if err := runGit(ctx, dir, "tag", "v2"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "config", "other.cfg"), "v3\n")
	commit("pack v3")
	if err := runGit(ctx, dir, "tag", "v3"); err != nil {
		t.Fatal(err)
	}

	// local built by the OLD squash path: branch at v1, squash-merge v2 (records
	// no parent), then the game reorders the block and we snapshot.
	if err := runGit(ctx, dir, "checkout", "-b", "local", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "merge", "--squash", "-X", "theirs", "v2"); err != nil {
		t.Fatal(err)
	}
	commit("Update configs to v2")
	writeFile(t, cfg, gameForm)
	commit("Snapshot player changes")

	// Sanity: v2 is NOT yet in local's ancestry (squash recorded no parent).
	if err := runGit(ctx, dir, "merge-base", "--is-ancestor", "v2^{commit}", "HEAD"); err == nil {
		t.Fatal("precondition failed: v2 should not be an ancestor of squash-built local")
	}

	// Re-baseline, then apply the v3 update the normal (real-merge) way.
	ensureBaseRecorded(ctx, dir, "v2")
	if err := mergePackVersion(ctx, dir, "v3", "Update configs to v3"); err != nil {
		t.Fatalf("merge v3: %v", err)
	}

	b, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "block {"); n != 1 {
		t.Fatalf("after re-baseline + v3 update: block count = %d, want 1", n)
	}

	// Base must now reach v3 (via the grafted v2), not the clone root.
	base, err := runGitOutput(ctx, dir, "merge-base", "local", "v3")
	if err != nil {
		t.Fatal(err)
	}
	v3, err := runGitOutput(ctx, dir, "rev-parse", "v3^{commit}")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(base) != strings.TrimSpace(v3) {
		t.Fatalf("merge-base = %s, want v3 %s", strings.TrimSpace(base), strings.TrimSpace(v3))
	}
}

// TestEnsureBaseRecordedNoopWhenAncestor verifies the fast path: when the
// previous tag is already in the local branch's history (repos created after the
// switch to real merges), ensureBaseRecorded creates no graft commit.
func TestEnsureBaseRecordedNoopWhenAncestor(t *testing.T) {
	if !IsGitAvailable() {
		t.Skip("git not available")
	}
	ctx := context.Background()
	dir := t.TempDir()
	gitInit(t, dir)

	writeFile(t, filepath.Join(dir, "config", "a.cfg"), "x\n")
	if err := runGit(ctx, dir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "commit", "-m", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "tag", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "checkout", "-b", "local"); err != nil {
		t.Fatal(err)
	}

	before, err := runGitOutput(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	// v1 is an ancestor of local → must be a no-op.
	ensureBaseRecorded(ctx, dir, "v1")
	after, err := runGitOutput(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(before) != strings.TrimSpace(after) {
		t.Fatalf("ensureBaseRecorded created a commit on the fast path: %s -> %s", before, after)
	}
}

func TestResolveRemainingConflicts(t *testing.T) {
	if !IsGitAvailable() {
		t.Skip("git not available")
	}
	ctx := context.Background()
	dir := setupConflictRepo(t)

	// Squash-merge pack into local; expect modify/delete conflicts.
	_ = runGit(ctx, dir, "merge", "--squash", "-X", "theirs", "pack")

	if err := resolveRemainingConflicts(ctx, dir); err != nil {
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
	if strings.TrimSpace(string(got)) != "pack-version" {
		t.Fatalf("shared.cfg = %q, want pack-version", got)
	}
}

func TestEnsureGitignoreCreatesAndAppends(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	gitInit(t, dir)

	// No .gitignore yet: should be created with all entries.
	if err := ensureGitignore(ctx, dir); err != nil {
		t.Fatalf("ensureGitignore: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	for _, entry := range gitignoreEntries {
		if !strings.Contains(string(got), entry) {
			t.Fatalf(".gitignore missing %q, got:\n%s", entry, got)
		}
	}

	// Pre-existing content without trailing newline: entry appended on its own line.
	writeFile(t, filepath.Join(dir, ".gitignore"), "*.tmp")
	if err := ensureGitignore(ctx, dir); err != nil {
		t.Fatalf("ensureGitignore append: %v", err)
	}
	got, _ = os.ReadFile(filepath.Join(dir, ".gitignore"))
	want := "*.tmp\n" + gitignoreEntries[0] + "\n"
	if string(got) != want {
		t.Fatalf(".gitignore = %q, want %q", got, want)
	}
}

func TestEnsureGitignoreIdempotent(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	gitInit(t, dir)

	if err := ensureGitignore(ctx, dir); err != nil {
		t.Fatalf("first ensureGitignore: %v", err)
	}
	first, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err := ensureGitignore(ctx, dir); err != nil {
		t.Fatalf("second ensureGitignore: %v", err)
	}
	second, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if string(first) != string(second) {
		t.Fatalf("not idempotent: %q vs %q", first, second)
	}
}

func TestEnsureGitignoreUntracksCommittedLog(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	gitInit(t, dir)

	// Commit the log before it is ignored.
	writeFile(t, filepath.Join(dir, "journeymap", "journeymap.log"), "old log\n")
	if err := runGit(ctx, dir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, dir, "commit", "-m", "tracked log"); err != nil {
		t.Fatal(err)
	}

	if err := ensureGitignore(ctx, dir); err != nil {
		t.Fatalf("ensureGitignore: %v", err)
	}

	// Log must no longer be tracked.
	out, err := runGitOutput(ctx, dir, "ls-files", "journeymap/journeymap.log")
	if err != nil {
		t.Fatalf("ls-files: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("journeymap.log still tracked: %q", out)
	}
}

// TestFetchForceUpdatesMovedUpstreamTag guards the nightly-tag mutation case:
// GTNH re-points nightly tags upstream; without --force, `git fetch` keeps the
// stale local ref, the next merge says "Already up to date", and the real pack
// changes are silently skipped. This reproduces an upstream re-tag and asserts
// the fetch line ApplyUpdate uses moves the local tag to the new commit.
func TestFetchForceUpdatesMovedUpstreamTag(t *testing.T) {
	if !IsGitAvailable() {
		t.Skip("git not available")
	}
	ctx := context.Background()

	// Upstream: commit A tagged T, then commit B (T not yet moved).
	upstream := t.TempDir()
	gitInit(t, upstream)
	writeFile(t, filepath.Join(upstream, "f"), "A\n")
	if err := runGit(ctx, upstream, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, upstream, "commit", "-m", "A"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, upstream, "tag", "T"); err != nil {
		t.Fatal(err)
	}
	commitA, err := runGitOutput(ctx, upstream, "rev-parse", "T^{commit}")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(upstream, "f"), "B\n")
	if err := runGit(ctx, upstream, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, upstream, "commit", "-m", "B"); err != nil {
		t.Fatal(err)
	}
	commitB, err := runGitOutput(ctx, upstream, "rev-parse", "HEAD"); if err != nil {
		t.Fatal(err)
	}

	// Local clones upstream, pinning tag T at commit A.
	local := t.TempDir()
	if err := runGit(ctx, local, "clone", upstream, "."); err != nil {
		t.Fatalf("clone: %v", err)
	}
	if err := runGit(ctx, local, "config", "user.name", "test"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, local, "config", "user.email", "test@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, local, "config", "commit.gpgsign", "false"); err != nil {
		t.Fatal(err)
	}
	localT, err := runGitOutput(ctx, local, "rev-parse", "T^{commit}")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(localT) != strings.TrimSpace(commitA) {
		t.Fatalf("setup: local T = %s, want A %s", strings.TrimSpace(localT), strings.TrimSpace(commitA))
	}

	// Upstream re-points T to commit B (mutable nightly tag).
	if err := runGit(ctx, upstream, "tag", "-f", "T", strings.TrimSpace(commitB)); err != nil {
		t.Fatalf("retag upstream: %v", err)
	}

	// Run the exact fetch line ApplyUpdate uses. Must update local T to B.
	if err := runGit(ctx, local, "fetch", "--force", "--no-tags", "origin", "tag", "T"); err != nil {
		t.Fatalf("fetch: %v", err)
	}

	got, err := runGitOutput(ctx, local, "rev-parse", "T^{commit}")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(got) != strings.TrimSpace(commitB) {
		t.Fatalf("after force-fetch: local T = %s, want B %s (stale tag, real pack changes would be skipped)", strings.TrimSpace(got), strings.TrimSpace(commitB))
	}
}

// stampedLine is what versionstamp writes into DreamCoreMod.properties for the
// version used below. Used by the merge test, which needs the line pre-placed
// rather than produced by a real Apply call.
const stampedLine = "displayedModpackVersion=2.9.x (Daily 648) - 2026-07-28\n"

// setupStampRepo builds a gameDir whose .gtnh-configs repo is on a `local`
// branch holding the pack's default DreamCoreMod.properties.
func setupStampRepo(t *testing.T) (gameDir, repoDir string) {
	t.Helper()
	ctx := context.Background()
	gameDir = t.TempDir()
	repoDir = ConfigRepoDir(gameDir)

	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, repoDir)

	writeFile(t, filepath.Join(repoDir, "config", "DreamCoreMod.properties"), "displayedModpackVersion=2.9.0\n")
	if err := runGit(ctx, repoDir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, repoDir, "commit", "-m", "pack v1"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, repoDir, "tag", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, repoDir, "checkout", "-b", "local", "v1"); err != nil {
		t.Fatal(err)
	}

	// The instance carries the same file, as it would after Init.
	writeFile(t, filepath.Join(gameDir, "config", "DreamCoreMod.properties"), "displayedModpackVersion=2.9.0\n")
	return gameDir, repoDir
}

// TestSnapshotOfStampedConfigDoesNotChurn proves the real stamp+snapshot round
// trip is idempotent: stamping and snapshotting twice in a row (same version)
// produces no tree change on the second round.
func TestSnapshotOfStampedConfigDoesNotChurn(t *testing.T) {
	if !IsGitAvailable() {
		t.Skip("git not available")
	}
	ctx := context.Background()
	gameDir, repoDir := setupStampRepo(t)
	v := versionstamp.DisplayVersion{Short: "2.9.x (Daily 648)", Long: "2.9.x (Daily 648) - 2026-07-28", Date: "2026-07-28"}

	// Round 1: stamp the instance for real, then snapshot.
	stamped, err := versionstamp.Apply(gameDir, gameDir, v)
	if err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	found := false
	for _, f := range stamped {
		if f == "config/DreamCoreMod.properties" {
			found = true
		}
	}
	if !found {
		t.Fatalf("first Apply did not stamp config/DreamCoreMod.properties, stamped = %v", stamped)
	}
	if err := Snapshot(ctx, gameDir, "server"); err != nil {
		t.Fatalf("first snapshot: %v", err)
	}

	// Round 2: same version. Apply is idempotent, so nothing changes on disk.
	if _, err := versionstamp.Apply(gameDir, gameDir, v); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if err := Snapshot(ctx, gameDir, "server"); err != nil {
		t.Fatalf("second snapshot: %v", err)
	}

	out, err := runGitOutput(ctx, repoDir, "diff", "HEAD~1", "HEAD", "--name-only")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("second snapshot changed files: %q", strings.TrimSpace(out))
	}
}

// TestStampedConfigMergesCleanlyAcrossPackUpdate checks that a stamped line does
// not conflict when the pack ships a new version of the same file, and that the
// pack's value wins (leaving the file ready to be restamped).
func TestStampedConfigMergesCleanlyAcrossPackUpdate(t *testing.T) {
	if !IsGitAvailable() {
		t.Skip("git not available")
	}
	ctx := context.Background()
	gameDir, repoDir := setupStampRepo(t)
	cfg := filepath.Join(repoDir, "config", "DreamCoreMod.properties")

	// Pack v2 ships its own default for the same key.
	if err := runGit(ctx, repoDir, "checkout", "main"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, cfg, "displayedModpackVersion=2.9.1\n")
	if err := runGit(ctx, repoDir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, repoDir, "commit", "-m", "pack v2"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, repoDir, "tag", "v2"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ctx, repoDir, "checkout", "local"); err != nil {
		t.Fatal(err)
	}

	// The instance's stamp is snapshotted onto `local`.
	writeFile(t, filepath.Join(gameDir, "config", "DreamCoreMod.properties"), stampedLine)
	if err := Snapshot(ctx, gameDir, "server"); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	if err := mergePackVersion(ctx, repoDir, "v2", "Update configs to v2"); err != nil {
		t.Fatalf("merge v2: %v", err)
	}

	got, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "<<<<<<<") {
		t.Fatalf("merge left conflict markers: %q", got)
	}
	if strings.TrimSpace(string(got)) != "displayedModpackVersion=2.9.1" {
		t.Fatalf("after merge = %q, want the pack default 2.9.1", strings.TrimSpace(string(got)))
	}
}

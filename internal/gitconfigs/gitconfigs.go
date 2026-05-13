package gitconfigs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caedis/gtnh-daily-updater/internal/fileutil"
	"github.com/caedis/gtnh-daily-updater/internal/logging"
)

const (
	RepoDir      = ".gtnh-configs"
	LocalBranch  = "local"
	RemoteURL    = "https://github.com/GTNewHorizons/GT-New-Horizons-Modpack"
	GitUserName  = "GTNH Daily Updater"
	GitUserEmail = "gtnh-daily-updater@localhost"
)

// ConfigRepoDir returns the path to the git repo inside gameDir.
func ConfigRepoDir(gameDir string) string {
	return filepath.Join(gameDir, RepoDir)
}

type trackedItem struct {
	Name       string // "config", "journeymap", "resourcepacks", "serverutilities", "servers.json"
	IsFile     bool   // true for servers.json
	ClientOnly bool
}

var allTrackedItems = []trackedItem{
	{Name: "config"},
	{Name: "journeymap"},
	{Name: "resourcepacks", ClientOnly: true},
	{Name: "serverutilities"},
	{Name: "servers.json", IsFile: true, ClientOnly: true},
}

func trackedItems(side string) []trackedItem {
	var result []trackedItem
	for _, item := range allTrackedItems {
		if item.ClientOnly && side != "client" {
			continue
		}
		result = append(result, item)
	}
	return result
}

// Init sets up the config git repo for the instance:
// 1. Backs up tracked items from gameDir
// 2. Clones the GTNH modpack repo at the given configVersion tag
// 3. Creates a 'local' branch and commits the instance's current configs
func Init(ctx context.Context, instanceDir, gameDir, side, configVersion string) error {
	repoDir := ConfigRepoDir(gameDir)
	logging.Debugf("Verbose: gitconfigs init gameDir=%q side=%s configVersion=%s repoDir=%q\n", gameDir, side, configVersion, repoDir)

	// Backup tracked items
	backupDir := filepath.Join(instanceDir, ".gtnh-configs-backup-"+time.Now().Format("2006-01-02"))
	logging.Debugf("Verbose: gitconfigs backing up tracked items to %q\n", backupDir)
	if err := backupTrackedItems(gameDir, backupDir, side); err != nil {
		return fmt.Errorf("backing up configs: %w", err)
	}

	// Remove any existing repo
	if err := os.RemoveAll(repoDir); err != nil {
		return fmt.Errorf("removing existing config repo: %w", err)
	}

	// Clone at the tag
	logging.Debugf("Verbose: gitconfigs cloning %s at tag %s\n", RemoteURL, configVersion)
	if err := runGit(ctx, gameDir, "clone", "--filter=blob:none", "--no-tags", "--single-branch", "--branch", configVersion, RemoteURL, repoDir); err != nil {
		return fmt.Errorf("cloning config repo: %w", err)
	}

	// Configure git identity
	if err := runGit(ctx, repoDir, "config", "user.name", GitUserName); err != nil {
		return fmt.Errorf("setting git user.name: %w", err)
	}
	if err := runGit(ctx, repoDir, "config", "user.email", GitUserEmail); err != nil {
		return fmt.Errorf("setting git user.email: %w", err)
	}

	// Create local branch from the cloned tag HEAD
	if err := runGit(ctx, repoDir, "checkout", "-b", LocalBranch); err != nil {
		return fmt.Errorf("creating local branch: %w", err)
	}
	logging.Debugf("Verbose: gitconfigs created branch %q\n", LocalBranch)

	// Copy instance configs into repo (overwriting pack versions)
	if err := copyTrackedItemsToRepo(gameDir, repoDir, side); err != nil {
		return fmt.Errorf("copying configs to repo: %w", err)
	}

	// Commit the local state
	if err := runGit(ctx, repoDir, "add", "-A"); err != nil {
		return fmt.Errorf("staging files: %w", err)
	}
	logStagedDiff(ctx, repoDir)
	msg := fmt.Sprintf("Local state at init (%s)", configVersion)
	if err := runGit(ctx, repoDir, "commit", "--allow-empty", "-m", msg); err != nil {
		return fmt.Errorf("committing local state: %w", err)
	}
	logging.Debugf("Verbose: gitconfigs init complete\n")

	return nil
}

// Snapshot captures current player changes in the git repo.
// Always commits (even if nothing changed) to record a checkpoint.
func Snapshot(ctx context.Context, gameDir, side string) error {
	repoDir := ConfigRepoDir(gameDir)
	logging.Debugf("Verbose: gitconfigs snapshot gameDir=%q side=%s\n", gameDir, side)

	if err := copyTrackedItemsToRepo(gameDir, repoDir, side); err != nil {
		return fmt.Errorf("copying configs to repo: %w", err)
	}

	if err := runGit(ctx, repoDir, "add", "-A"); err != nil {
		return fmt.Errorf("staging files: %w", err)
	}
	logStagedDiff(ctx, repoDir)
	if err := runGit(ctx, repoDir, "commit", "--allow-empty", "-m", "Snapshot player changes"); err != nil {
		return fmt.Errorf("committing snapshot: %w", err)
	}
	logging.Debugf("Verbose: gitconfigs snapshot committed\n")

	return nil
}

// ApplyUpdate fetches the new pack version and merges it into the local branch
// (pack wins on genuine conflicts), then copies updated files back to the instance.
func ApplyUpdate(ctx context.Context, gameDir, side, newConfigVersion string) error {
	repoDir := ConfigRepoDir(gameDir)
	logging.Debugf("Verbose: gitconfigs apply-update gameDir=%q side=%s newVersion=%s\n", gameDir, side, newConfigVersion)

	// Unshallow if the repo was previously cloned with --depth 1.
	// A shallow repo has no common ancestor visible, causing every file to be
	// treated as a conflict by git merge. Full history is required for a correct merge.
	shallow, _ := runGitOutput(ctx, repoDir, "rev-parse", "--is-shallow-repository")
	if strings.TrimSpace(shallow) == "true" {
		logging.Debugf("Verbose: gitconfigs repo is shallow, unshallowing for correct merge\n")
		if err := runGit(ctx, repoDir, "fetch", "--unshallow"); err != nil {
			return fmt.Errorf("unshallowing config repo: %w", err)
		}
	}

	// Fetch the new tag
	if err := runGit(ctx, repoDir, "fetch", "--no-tags", "origin", "tag", newConfigVersion); err != nil {
		return fmt.Errorf("fetching tag %s: %w", newConfigVersion, err)
	}

	// Merge — pack wins on genuine conflicts; user changes on untouched lines are preserved
	logging.Debugf("Verbose: gitconfigs merging %s (pack wins on genuine conflicts)\n", newConfigVersion)
	mergeErr := runGit(ctx, repoDir, "merge", "--squash", "-X", "theirs", newConfigVersion)
	if mergeErr != nil {
		// `-X theirs` does not auto-resolve modify/delete conflicts. Apply the
		// same pack-wins rule to those entries; if anything else is unmerged,
		// surface the original error.
		if resolveErr := resolveRemainingConflicts(ctx, repoDir); resolveErr != nil {
			return fmt.Errorf("merging config update: %w (%v)", mergeErr, resolveErr)
		}
	}

	logStagedDiff(ctx, repoDir)
	msg := fmt.Sprintf("Update configs to %s", newConfigVersion)
	if err := runGit(ctx, repoDir, "commit", "--allow-empty", "-m", msg); err != nil {
		return fmt.Errorf("committing config update: %w", err)
	}
	logging.Debugf("Verbose: gitconfigs merge committed, replacing instance files\n")

	// Atomically replace only changed instance dirs from repo
	changedOut, err := runGitOutput(ctx, repoDir, "diff", "--name-only", "HEAD~1", "HEAD")
	if err != nil {
		logging.Debugf("Verbose: gitconfigs could not determine changed files, replacing all: %v\n", err)
		if err := atomicReplaceFromRepo(gameDir, repoDir, trackedItems(side)); err != nil {
			return fmt.Errorf("applying updated configs: %w", err)
		}
	} else {
		items := filterChangedItems(trackedItems(side), strings.Fields(changedOut))
		if len(items) == 0 {
			logging.Debugf("Verbose: gitconfigs merge changed no tracked items, skipping file replacement\n")
		} else {
			logging.Debugf("Verbose: gitconfigs replacing %d changed tracked item(s)\n", len(items))
			if err := atomicReplaceFromRepo(gameDir, repoDir, items); err != nil {
				return fmt.Errorf("applying updated configs: %w", err)
			}
		}
	}
	logging.Debugf("Verbose: gitconfigs apply-update complete\n")

	return nil
}

// resolveRemainingConflicts handles conflict types left over by `merge -X theirs`
// (which only resolves content conflicts, not structural ones) using pack-wins:
//   - UU (both modified, e.g. binary content conflict) → take pack version
//   - UD (modified by us, deleted by them) → remove the path
//   - DU (deleted by us, modified by them) → take pack version
//   - AA (rename/rename: both sides added here) → take pack version
//   - AU (rename/rename: our rename, pack did not) → remove the path
//   - UA (rename/rename: pack rename, we did not) → accept pack rename
//   - DD (rename/rename: both sides deleted/renamed original) → remove from index
//
// Returns nil only when every unmerged path was resolved. Any other unmerged
// state is reported as an error.
func resolveRemainingConflicts(ctx context.Context, repoDir string) error {
	out, err := runGitOutput(ctx, repoDir, "status", "--porcelain=v1", "-z")
	if err != nil {
		return fmt.Errorf("reading git status: %w", err)
	}
	if out == "" {
		return nil
	}

	var unresolved []string
	resolved := 0
	for entry := range strings.SplitSeq(strings.TrimRight(out, "\x00"), "\x00") {
		if len(entry) < 4 {
			continue
		}
		code := entry[:2]
		path := entry[3:]
		switch code {
		case "UU":
			// Both sides modified, content conflict not auto-resolved by -X theirs
			// (typically binary files). Pack wins: take theirs.
			logging.Debugf("Verbose: gitconfigs resolving UU (pack wins) %q\n", path)
			if err := runGit(ctx, repoDir, "checkout", "--theirs", "--", path); err != nil {
				return fmt.Errorf("checking out %s: %w", path, err)
			}
			if err := runGit(ctx, repoDir, "add", "--", path); err != nil {
				return fmt.Errorf("adding %s: %w", path, err)
			}
			resolved++
		case "UD":
			logging.Debugf("Verbose: gitconfigs resolving UD (pack deleted) %q\n", path)
			if err := runGit(ctx, repoDir, "rm", "--", path); err != nil {
				return fmt.Errorf("removing %s: %w", path, err)
			}
			resolved++
		case "DU":
			logging.Debugf("Verbose: gitconfigs resolving DU (pack kept) %q\n", path)
			if err := runGit(ctx, repoDir, "checkout", "--theirs", "--", path); err != nil {
				return fmt.Errorf("checking out %s: %w", path, err)
			}
			if err := runGit(ctx, repoDir, "add", "--", path); err != nil {
				return fmt.Errorf("adding %s: %w", path, err)
			}
			resolved++
		case "AA":
			// Both sides added content at this path (e.g. two different source files
			// both renamed here). Pack wins: take theirs (stage 3).
			logging.Debugf("Verbose: gitconfigs resolving AA (pack wins) %q\n", path)
			if err := runGit(ctx, repoDir, "checkout", "--theirs", "--", path); err != nil {
				return fmt.Errorf("checking out %s: %w", path, err)
			}
			if err := runGit(ctx, repoDir, "add", "--", path); err != nil {
				return fmt.Errorf("adding %s: %w", path, err)
			}
			resolved++
		case "AU":
			// rename/rename: we renamed to this path, pack did not. Pack wins: remove it.
			logging.Debugf("Verbose: gitconfigs resolving AU (our rename, remove) %q\n", path)
			if err := runGit(ctx, repoDir, "rm", "-f", "--", path); err != nil {
				return fmt.Errorf("removing %s: %w", path, err)
			}
			resolved++
		case "UA":
			// rename/rename: pack renamed to this path, we did not. Pack wins: accept it.
			logging.Debugf("Verbose: gitconfigs resolving UA (pack rename, keep) %q\n", path)
			if err := runGit(ctx, repoDir, "add", "--", path); err != nil {
				return fmt.Errorf("adding %s: %w", path, err)
			}
			resolved++
		case "DD":
			// rename/rename: original path renamed away by both sides. Remove from index.
			logging.Debugf("Verbose: gitconfigs resolving DD (both renamed original) %q\n", path)
			if err := runGit(ctx, repoDir, "rm", "--cached", "--ignore-unmatch", "--", path); err != nil {
				return fmt.Errorf("removing %s from index: %w", path, err)
			}
			resolved++
		default:
			if strings.ContainsAny(code, "U") {
				unresolved = append(unresolved, code+" "+path)
			}
		}
	}

	if len(unresolved) > 0 {
		return fmt.Errorf("unresolved conflicts: %s", strings.Join(unresolved, ", "))
	}
	logging.Debugf("Verbose: gitconfigs resolved %d modify/delete conflict(s)\n", resolved)
	return nil
}

// filterChangedItems returns only the items that have at least one path in changedPaths.
func filterChangedItems(items []trackedItem, changedPaths []string) []trackedItem {
	var result []trackedItem
	for _, item := range items {
		for _, p := range changedPaths {
			if item.IsFile {
				if p == item.Name {
					result = append(result, item)
					break
				}
			} else {
				if strings.HasPrefix(p, item.Name+"/") || p == item.Name {
					result = append(result, item)
					break
				}
			}
		}
	}
	return result
}

// atomicReplaceFromRepo copies the given items from repoDir back to gameDir atomically.
// For each item: rename existing to .bak, copy from repo, remove .bak on success.
// On failure: restore from .bak.
func atomicReplaceFromRepo(gameDir, repoDir string, items []trackedItem) error {
	var backed []string

	rollback := func() {
		for _, name := range backed {
			dst := filepath.Join(gameDir, name)
			bak := dst + ".bak"
			_ = os.RemoveAll(dst)
			_ = os.Rename(bak, dst)
		}
	}

	// Phase 1: rename existing to .bak
	for _, item := range items {
		dst := filepath.Join(gameDir, item.Name)
		bak := dst + ".bak"

		if _, err := os.Stat(dst); err == nil {
			if err := os.Rename(dst, bak); err != nil {
				rollback()
				return fmt.Errorf("renaming %s to backup: %w", item.Name, err)
			}
		}
		backed = append(backed, item.Name)
	}

	// Phase 2: copy from repo
	for _, item := range items {
		src := filepath.Join(repoDir, item.Name)
		dst := filepath.Join(gameDir, item.Name)
		bak := dst + ".bak"

		var copyErr error
		if item.IsFile {
			copyErr = fileutil.CopyFile(src, dst)
		} else {
			if item.Name == "journeymap" {
				// Preserve journeymap/data from bak
				copyErr = fileutil.CopyDirExcluding(src, dst, "data")
				if copyErr == nil && fileExists(bak) {
					// Restore data subdir from backup
					bakData := filepath.Join(bak, "data")
					if fileExists(bakData) {
						copyErr = fileutil.CopyDirExcluding(bakData, filepath.Join(dst, "data"))
					}
				}
			} else {
				copyErr = fileutil.CopyDirExcluding(src, dst)
			}
		}

		if copyErr != nil {
			rollback()
			return fmt.Errorf("copying %s from repo: %w", item.Name, copyErr)
		}
	}

	// Phase 3: remove backups on success
	for _, item := range items {
		bak := filepath.Join(gameDir, item.Name) + ".bak"
		_ = os.RemoveAll(bak)
	}

	return nil
}

// backupTrackedItems copies tracked items from gameDir to backupDir.
func backupTrackedItems(gameDir, backupDir, side string) error {
	items := trackedItems(side)
	for _, item := range items {
		src := filepath.Join(gameDir, item.Name)
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		}
		dst := filepath.Join(backupDir, item.Name)
		if item.IsFile {
			if err := fileutil.CopyFile(src, dst); err != nil {
				return fmt.Errorf("backing up %s: %w", item.Name, err)
			}
		} else {
			if err := fileutil.CopyDirExcluding(src, dst); err != nil {
				return fmt.Errorf("backing up %s: %w", item.Name, err)
			}
		}
	}
	return nil
}

// clearDirExcluding removes all direct children of dir except those named in preserve.
// Returns nil if dir does not exist.
func clearDirExcluding(dir string, preserve ...string) error {
	keep := make(map[string]bool, len(preserve))
	for _, p := range preserve {
		keep[p] = true
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if keep[entry.Name()] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

// copyTrackedItemsToRepo syncs tracked items from gameDir into repoDir,
// including propagating deletions. Skips journeymap/data/.
func copyTrackedItemsToRepo(gameDir, repoDir, side string) error {
	for _, item := range trackedItems(side) {
		src := filepath.Join(gameDir, item.Name)
		dst := filepath.Join(repoDir, item.Name)
		srcExists := fileExists(src)

		if item.IsFile {
			if !srcExists {
				if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("removing deleted %s from repo: %w", item.Name, err)
				}
				continue
			}
			if err := fileutil.CopyFile(src, dst); err != nil {
				return fmt.Errorf("copying %s to repo: %w", item.Name, err)
			}
		} else if item.Name == "journeymap" {
			// Preserve repo's journeymap/data (pack-provided; never written from instance)
			if err := clearDirExcluding(dst, "data"); err != nil {
				return fmt.Errorf("clearing journeymap repo dir: %w", err)
			}
			if srcExists {
				if err := fileutil.CopyDirExcluding(src, dst, "data"); err != nil {
					return fmt.Errorf("copying %s to repo: %w", item.Name, err)
				}
			}
		} else {
			// Full sync: remove repo dir and recopy from instance
			if err := os.RemoveAll(dst); err != nil {
				return fmt.Errorf("clearing %s from repo: %w", item.Name, err)
			}
			if srcExists {
				if err := fileutil.CopyDirExcluding(src, dst); err != nil {
					return fmt.Errorf("copying %s to repo: %w", item.Name, err)
				}
			}
		}
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

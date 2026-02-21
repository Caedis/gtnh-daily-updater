package configmerge

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type DiffStatus string

const (
	DiffAdded     DiffStatus = "added"
	DiffRemoved   DiffStatus = "removed"
	DiffModified  DiffStatus = "modified"
	DiffUnchanged DiffStatus = "unchanged"
)

type ConfigDiff struct {
	Path   string
	Status DiffStatus
}

// DiffConfigFiles compares current tracked file hashes against
// a baseline hash map (typically state.ConfigHashes from the last successful update).
func DiffConfigFiles(gameDir string, baselineHashes map[string]string, includeUnchanged bool) ([]ConfigDiff, error) {
	currentHashes, err := computeCurrentHashes(gameDir, baselineHashes)
	if err != nil {
		return nil, err
	}

	allPaths := make(map[string]struct{}, len(baselineHashes)+len(currentHashes))
	for p := range baselineHashes {
		allPaths[p] = struct{}{}
	}
	for p := range currentHashes {
		allPaths[p] = struct{}{}
	}

	paths := make([]string, 0, len(allPaths))
	for p := range allPaths {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	diffs := make([]ConfigDiff, 0, len(paths))
	for _, p := range paths {
		baseHash, inBase := baselineHashes[p]
		curHash, inCurrent := currentHashes[p]

		var status DiffStatus
		switch {
		case !inBase && inCurrent:
			status = DiffAdded
		case inBase && !inCurrent:
			status = DiffRemoved
		case inBase && inCurrent && baseHash != curHash:
			status = DiffModified
		default:
			status = DiffUnchanged
		}

		if status == DiffUnchanged && !includeUnchanged {
			continue
		}
		diffs = append(diffs, ConfigDiff{Path: p, Status: status})
	}

	return diffs, nil
}

func computeCurrentHashes(gameDir string, baselineHashes map[string]string) (map[string]string, error) {
	// Backward compatibility for legacy state files where config paths were stored
	// relative to config/ (without a config/ prefix).
	if !hasConfigPrefix(baselineHashes) {
		return ComputeConfigHashes(gameDir)
	}

	hashes := make(map[string]string)
	for root := range trackedRoots(baselineHashes) {
		rootPath := filepath.Join(gameDir, filepath.FromSlash(root))
		info, err := os.Stat(rootPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			content, err := os.ReadFile(rootPath)
			if err != nil {
				return nil, err
			}
			hashes[root] = hashBytes(content)
			continue
		}

		err = filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}

			relPath, err := filepath.Rel(gameDir, path)
			if err != nil {
				return err
			}

			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			hashes[relPath] = hashBytes(content)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	return hashes, nil
}

func hasConfigPrefix(paths map[string]string) bool {
	for p := range paths {
		normalized := filepath.ToSlash(p)
		if normalized == "config" || strings.HasPrefix(normalized, "config/") {
			return true
		}
	}
	return false
}

func trackedRoots(paths map[string]string) map[string]struct{} {
	roots := make(map[string]struct{})
	for p := range paths {
		normalized := filepath.ToSlash(p)
		if normalized == "" {
			continue
		}
		root := normalized
		if slash := strings.IndexRune(normalized, '/'); slash != -1 {
			root = normalized[:slash]
		}
		roots[root] = struct{}{}
	}
	return roots
}

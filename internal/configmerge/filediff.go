package configmerge

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/caedis/gtnh-daily-updater/internal/assets"
)

// FileDiffResult describes a single file diff against the tracked config pack version.
type FileDiffResult struct {
	RequestedPath string
	ResolvedPath  string
	Status        DiffStatus
	Diff          string
}

// DiffFileAgainstConfigVersion diffs a local file against the specified GTNH config version.
// The requested path may be either "config/..." or relative to config/ (for example "GregTech/Pollution.cfg").
func DiffFileAgainstConfigVersion(
	ctx context.Context,
	gameDir string,
	db *assets.AssetsDB,
	configVersion string,
	githubToken string,
	requestedPath string,
) (*FileDiffResult, error) {
	if strings.TrimSpace(configVersion) == "" {
		return nil, fmt.Errorf("missing config version in local state")
	}

	candidates, err := normalizeDiffPathCandidates(requestedPath)
	if err != nil {
		return nil, err
	}

	tmpDir, err := os.MkdirTemp("", "gtnh-config-diff-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	packDir, err := downloadAndExtractConfig(ctx, db, configVersion, githubToken, filepath.Join(tmpDir, "pack"))
	if err != nil {
		return nil, fmt.Errorf("downloading config baseline: %w", err)
	}

	packPath, packContent, packExists, err := readFirstExistingPath(packDir, candidates)
	if err != nil {
		return nil, err
	}

	if !packExists {
		localPath, localContent, localExists, err := readFirstExistingPath(gameDir, candidates)
		if err != nil {
			return nil, err
		}
		if !localExists {
			return nil, fmt.Errorf("file %q not found in tracked config pack or local instance", requestedPath)
		}

		return &FileDiffResult{
			RequestedPath: requestedPath,
			ResolvedPath:  localPath,
			Status:        DiffAdded,
			Diff:          renderUnifiedLineDiff(nil, localContent, "pack/"+localPath, "local/"+localPath),
		}, nil
	}

	localPath := filepath.Join(gameDir, filepath.FromSlash(packPath))
	localContent, err := os.ReadFile(localPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &FileDiffResult{
				RequestedPath: requestedPath,
				ResolvedPath:  packPath,
				Status:        DiffRemoved,
				Diff:          renderUnifiedLineDiff(packContent, nil, "pack/"+packPath, "local/"+packPath),
			}, nil
		}
		return nil, fmt.Errorf("reading local file %s: %w", packPath, err)
	}

	status := DiffModified
	if hashBytes(packContent) == hashBytes(localContent) {
		status = DiffUnchanged
	}

	return &FileDiffResult{
		RequestedPath: requestedPath,
		ResolvedPath:  packPath,
		Status:        status,
		Diff:          renderUnifiedLineDiff(packContent, localContent, "pack/"+packPath, "local/"+packPath),
	}, nil
}

func normalizeDiffPathCandidates(requestedPath string) ([]string, error) {
	raw := strings.TrimSpace(requestedPath)
	if raw == "" {
		return nil, fmt.Errorf("path must not be empty")
	}

	raw = strings.ReplaceAll(raw, "\\", "/")
	clean := path.Clean(raw)
	switch {
	case clean == ".":
		return nil, fmt.Errorf("path must reference a file")
	case path.IsAbs(clean):
		return nil, fmt.Errorf("path must be relative, got %q", requestedPath)
	case clean == "..", strings.HasPrefix(clean, "../"):
		return nil, fmt.Errorf("path traversal is not allowed: %q", requestedPath)
	case strings.Contains(clean, ":"):
		return nil, fmt.Errorf("path must not contain a drive letter: %q", requestedPath)
	}

	candidates := []string{clean}
	if strings.HasPrefix(clean, "config/") {
		candidates = append(candidates, strings.TrimPrefix(clean, "config/"))
	} else {
		candidates = append(candidates, path.Join("config", clean))
	}

	seen := make(map[string]struct{}, len(candidates))
	deduped := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == "" || candidate == "." {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		deduped = append(deduped, candidate)
	}

	return deduped, nil
}

func readFirstExistingPath(root string, candidates []string) (string, []byte, bool, error) {
	for _, relPath := range candidates {
		fullPath := filepath.Join(root, filepath.FromSlash(relPath))
		content, err := os.ReadFile(fullPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", nil, false, fmt.Errorf("reading %s: %w", relPath, err)
		}
		return relPath, content, true, nil
	}
	return "", nil, false, nil
}

type lineOpKind int

const (
	opEqual lineOpKind = iota
	opDelete
	opInsert
)

type lineOp struct {
	kind lineOpKind
	line string
}

func renderUnifiedLineDiff(oldContent, newContent []byte, oldLabel, newLabel string) string {
	if bytes.Equal(oldContent, newContent) {
		return ""
	}

	if !isLikelyText(oldContent) || !isLikelyText(newContent) {
		return fmt.Sprintf("Binary files differ: %s -> %s", oldLabel, newLabel)
	}

	oldLines := splitLines(string(oldContent))
	newLines := splitLines(string(newContent))
	ops := diffLineOps(oldLines, newLines)

	hasChanges := false
	for _, op := range ops {
		if op.kind != opEqual {
			hasChanges = true
			break
		}
	}
	if !hasChanges {
		return ""
	}

	var b strings.Builder
	b.WriteString("--- ")
	b.WriteString(oldLabel)
	b.WriteString("\n")
	b.WriteString("+++ ")
	b.WriteString(newLabel)
	b.WriteString("\n")
	b.WriteString("@@\n")
	for _, op := range ops {
		switch op.kind {
		case opEqual:
			b.WriteString(" ")
		case opDelete:
			b.WriteString("-")
		case opInsert:
			b.WriteString("+")
		}
		b.WriteString(op.line)
		b.WriteString("\n")
	}
	return b.String()
}

func isLikelyText(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	if bytes.Contains(data, []byte{0}) {
		return false
	}
	return utf8.Valid(data)
}

func diffLineOps(oldLines, newLines []string) []lineOp {
	m := len(oldLines)
	n := len(newLines)

	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}

	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}

	ops := make([]lineOp, 0, m+n)
	i, j := 0, 0
	for i < m && j < n {
		switch {
		case oldLines[i] == newLines[j]:
			ops = append(ops, lineOp{kind: opEqual, line: oldLines[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			ops = append(ops, lineOp{kind: opDelete, line: oldLines[i]})
			i++
		default:
			ops = append(ops, lineOp{kind: opInsert, line: newLines[j]})
			j++
		}
	}

	for i < m {
		ops = append(ops, lineOp{kind: opDelete, line: oldLines[i]})
		i++
	}
	for j < n {
		ops = append(ops, lineOp{kind: opInsert, line: newLines[j]})
		j++
	}

	return ops
}

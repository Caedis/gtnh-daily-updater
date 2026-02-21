package configmerge

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/caedis/gtnh-daily-updater/internal/assets"
	"github.com/caedis/gtnh-daily-updater/internal/downloader"
	"github.com/caedis/gtnh-daily-updater/internal/logging"
)

type MergeResult struct {
	FilesUpdated  int
	FilesMerged   int
	FilesConflict int
	ConflictFiles []string
	NewHashes     map[string]string
}

// MergeConfigs performs a 3-way merge for files tracked by the GT-New-Horizons-Modpack release.
// gameDir is the pack root in the local instance (e.g. instanceDir/.minecraft/ for clients).
func MergeConfigs(ctx context.Context, gameDir string, oldHashes map[string]string, oldConfigVersion string, db *assets.AssetsDB, newConfigVersion, githubToken string) (*MergeResult, error) {
	logging.Debugf(
		"Verbose: pack merge start game-dir=%q old-version=%q new-version=%q old-hashes=%d github-token=%t\n",
		gameDir,
		oldConfigVersion,
		newConfigVersion,
		len(oldHashes),
		githubToken != "",
	)

	tmpDir, err := os.MkdirTemp("", "gtnh-pack-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Download and extract new modpack zip.
	newPackDir, err := downloadAndExtractConfig(ctx, db, newConfigVersion, githubToken, filepath.Join(tmpDir, "new"))
	if err != nil {
		return nil, fmt.Errorf("new config: %w", err)
	}

	// Download and extract old modpack zip for 3-way merge base content.
	var oldPackDir string
	if oldConfigVersion != "" {
		old, err := downloadAndExtractConfig(ctx, db, oldConfigVersion, githubToken, filepath.Join(tmpDir, "old"))
		if err != nil {
			logging.Infof("  Warning: could not download old config %s for 3-way merge: %v\n", oldConfigVersion, err)
			logging.Infoln("  Files where both user and pack changed will be flagged as conflicts.")
		} else {
			oldPackDir = old
		}
	}

	result := &MergeResult{
		NewHashes: make(map[string]string),
	}

	// Walk all files in the new modpack.
	err = filepath.WalkDir(newPackDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(newPackDir, path)
		if err != nil {
			return err
		}

		newContent, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading new config %s: %w", relPath, err)
		}

		newHash := hashBytes(newContent)
		result.NewHashes[relPath] = newHash

		userPath := filepath.Join(gameDir, relPath)
		baseHash, hasBase := lookupBaseHash(oldHashes, relPath)

		userContent, userErr := os.ReadFile(userPath)
		userExists := userErr == nil

		if !hasBase {
			// New file from pack - copy it in
			if err := writeFile(userPath, newContent); err != nil {
				return fmt.Errorf("writing new config %s: %w", relPath, err)
			}
			result.FilesUpdated++
			logging.Infof("  + %s (new)\n", relPath)
			return nil
		}

		if !userExists {
			// User deleted this file, pack still has it - copy new version
			if err := writeFile(userPath, newContent); err != nil {
				return fmt.Errorf("writing config %s: %w", relPath, err)
			}
			result.FilesUpdated++
			logging.Infof("  + %s (restored)\n", relPath)
			return nil
		}

		userHash := hashBytes(userContent)

		// Decision matrix
		switch {
		case userHash == baseHash:
			// User didn't change - take pack's new version
			if err := writeFile(userPath, newContent); err != nil {
				return fmt.Errorf("writing config %s: %w", relPath, err)
			}
			result.FilesUpdated++
			logging.Infof("  ~ %s (updated)\n", relPath)

		case newHash == baseHash:
			// Pack didn't change - keep user's version
			logging.Debugf("  = %s (user-changed, pack-unchanged)\n", relPath)

		case userHash == newHash:
			// Both made the same change - no action needed
			logging.Debugf("  = %s (already in sync)\n", relPath)

		default:
			// Both changed - attempt 3-way merge with base content
			var baseContent []byte
			if oldPackDir != "" {
				basePath := filepath.Join(oldPackDir, relPath)
				if data, err := os.ReadFile(basePath); err == nil {
					baseContent = data
				}
			}

			merged, conflicts := mergeFile(relPath, baseContent, newContent, userContent)
			if len(conflicts) > 0 {
				// Conflict - keep user's file, write .packnew
				packnewPath := userPath + ".packnew"
				if err := writeFile(packnewPath, newContent); err != nil {
					return fmt.Errorf("writing packnew %s: %w", relPath, err)
				}
				result.FilesConflict++
				result.ConflictFiles = append(result.ConflictFiles, relPath)
				logging.Infof("  ! %s (conflict → .packnew)\n", relPath)
			} else {
				if err := writeFile(userPath, merged); err != nil {
					return fmt.Errorf("writing merged config %s: %w", relPath, err)
				}
				result.FilesMerged++
				logging.Infof("  ~ %s (merged)\n", relPath)
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("merging configs: %w", err)
	}
	logging.Debugf(
		"Verbose: pack merge complete updated=%d merged=%d conflicts=%d\n",
		result.FilesUpdated,
		result.FilesMerged,
		result.FilesConflict,
	)

	return result, nil
}

// downloadAndExtractConfig downloads a modpack zip and extracts it, returning the pack root path.
func downloadAndExtractConfig(ctx context.Context, db *assets.AssetsDB, configVersion, githubToken, workDir string) (string, error) {
	url, filename, isAPI, err := db.ResolveConfigDownload(configVersion)
	if err != nil {
		if githubToken != "" {
			var apiURL string
			apiURL, filename, err = db.ResolveConfigDownloadWithAuth(configVersion)
			if err != nil {
				return "", fmt.Errorf("resolving config download: %w", err)
			}
			url = apiURL
			isAPI = true
		} else {
			return "", fmt.Errorf("resolving config download: %w", err)
		}
	}
	logging.Debugf("Verbose: config download version=%s filename=%s url=%s github-api=%t\n", configVersion, filename, url, isAPI)

	if err := os.MkdirAll(workDir, 0755); err != nil {
		return "", fmt.Errorf("creating work dir: %w", err)
	}

	zipPath := filepath.Join(workDir, filename)
	if err := downloader.DownloadToFile(ctx, url, zipPath, githubToken, isAPI); err != nil {
		// If browser URL failed, try API URL with token
		if isAPI && githubToken != "" {
			logging.Debugf("Verbose: config download failed via browser URL, retrying API URL for %s\n", configVersion)
			apiURL, _, err2 := db.ResolveConfigDownloadWithAuth(configVersion)
			if err2 == nil {
				err = downloader.DownloadToFile(ctx, apiURL, zipPath, githubToken, true)
			}
		}
		if err != nil {
			return "", fmt.Errorf("downloading config zip: %w", err)
		}
	}

	extractDir := filepath.Join(workDir, "extracted")
	if err := extractZip(zipPath, extractDir); err != nil {
		return "", fmt.Errorf("extracting config zip: %w", err)
	}

	packDir := findPackRoot(extractDir)
	if packDir == "" {
		return "", fmt.Errorf("no pack root containing a config directory found in zip")
	}

	return packDir, nil
}

// ComputeTrackedFileHashes hashes local files tracked by the specified modpack version.
func ComputeTrackedFileHashes(ctx context.Context, gameDir string, db *assets.AssetsDB, configVersion, githubToken string) (map[string]string, error) {
	tmpDir, err := os.MkdirTemp("", "gtnh-pack-hashes-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	packDir, err := downloadAndExtractConfig(ctx, db, configVersion, githubToken, filepath.Join(tmpDir, "pack"))
	if err != nil {
		return nil, err
	}

	hashes := make(map[string]string)
	err = filepath.WalkDir(packDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(packDir, path)
		if err != nil {
			return err
		}

		userPath := filepath.Join(gameDir, relPath)
		content, err := os.ReadFile(userPath)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}

		hashes[relPath] = hashBytes(content)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("hashing tracked files: %w", err)
	}

	return hashes, nil
}

// ComputeConfigHashes hashes all files in the config directory.
// gameDir is the directory containing the config/ folder.
func ComputeConfigHashes(gameDir string) (map[string]string, error) {
	configDir := filepath.Join(gameDir, "config")
	hashes := make(map[string]string)

	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		return hashes, nil
	}

	err := filepath.WalkDir(configDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(configDir, path)
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

	return hashes, err
}

func mergeFile(relPath string, base, theirs, ours []byte) ([]byte, []string) {
	if base == nil {
		// No base content available — we can't do a proper 3-way merge.
		// Report as conflict so user can review manually.
		return nil, []string{"no base content available for 3-way merge"}
	}

	ext := strings.ToLower(filepath.Ext(relPath))
	switch ext {
	case ".cfg":
		return MergeCfg(base, theirs, ours)
	case ".json":
		return MergeJSON(base, theirs, ours)
	default:
		return MergeText(base, theirs, ours)
	}
}

func hashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

func writeFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0644)
}

func extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		path := filepath.Join(destDir, f.Name)

		// Security check: prevent path traversal
		cleanDest := filepath.Clean(destDir)
		cleanPath := filepath.Clean(path)
		if cleanPath != cleanDest && !strings.HasPrefix(cleanPath, cleanDest+string(os.PathSeparator)) {
			continue
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(path, 0755)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		outFile, err := os.Create(path)
		if err != nil {
			rc.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()
		if err != nil {
			return err
		}
	}

	return nil
}

func findPackRoot(extractDir string) string {
	// Check if config/ exists at root of extracted dir.
	direct := filepath.Join(extractDir, "config")
	if info, err := os.Stat(direct); err == nil && info.IsDir() {
		return extractDir
	}

	// Check one level deep (zip might have a top-level directory).
	var found string
	_ = filepath.WalkDir(extractDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == extractDir {
			return nil
		}
		if !d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(extractDir, path)
		if err != nil {
			return err
		}

		// Only inspect first-level directories.
		if strings.ContainsRune(rel, os.PathSeparator) {
			return filepath.SkipDir
		}

		nested := filepath.Join(path, "config")
		if info, err := os.Stat(nested); err == nil && info.IsDir() {
			found = path
			return filepath.SkipAll
		}

		return filepath.SkipDir
	})
	if found != "" {
		return found
	}

	return ""
}

func lookupBaseHash(oldHashes map[string]string, relPath string) (string, bool) {
	if baseHash, ok := oldHashes[relPath]; ok {
		return baseHash, true
	}

	// Backward compatibility: previous versions tracked config files relative to
	// config/ instead of the pack root.
	normalized := filepath.ToSlash(relPath)
	if strings.HasPrefix(normalized, "config/") {
		legacy := filepath.FromSlash(strings.TrimPrefix(normalized, "config/"))
		if baseHash, ok := oldHashes[legacy]; ok {
			return baseHash, true
		}
	}

	return "", false
}

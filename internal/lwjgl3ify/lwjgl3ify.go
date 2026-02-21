package lwjgl3ify

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/caedis/gtnh-daily-updater/internal/downloader"
)

// multimcZipURL returns the download URL for the lwjgl3ify MultiMC zip.
func multimcZipURL(version string) string {
	return fmt.Sprintf(
		"https://github.com/GTNewHorizons/lwjgl3ify/releases/download/%s/lwjgl3ify-%s-multimc.zip",
		version, version,
	)
}

// forgePatchesJarURL returns the direct download URL for the forgePatches jar.
func forgePatchesJarURL(version string) string {
	return fmt.Sprintf(
		"https://github.com/GTNewHorizons/lwjgl3ify/releases/download/%s/lwjgl3ify-%s-forgePatches.jar",
		version, version,
	)
}

// NeedsUpdate returns true if this mod name is lwjgl3ify.
func NeedsUpdate(name string) bool {
	return strings.EqualFold(name, "lwjgl3ify")
}

// UpdateClient downloads the lwjgl3ify MultiMC zip and applies it to a client instance.
// This replaces:
//   - libraries/lwjgl3ify-*-forgePatches.jar
//   - patches/*.json
//   - mmc-pack.json
//
// The regular mod jar in mods/ is handled separately by the normal mod update flow.
func UpdateClient(ctx context.Context, instanceDir, newVersion, githubToken string) error {
	url := multimcZipURL(newVersion)

	tmpDir, err := os.MkdirTemp("", "gtnh-lwjgl3ify-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	zipPath := filepath.Join(tmpDir, fmt.Sprintf("lwjgl3ify-%s-multimc.zip", newVersion))
	if err := downloader.DownloadToFile(ctx, url, zipPath, githubToken, false); err != nil {
		return fmt.Errorf("downloading lwjgl3ify multimc zip: %w", err)
	}

	// Remove old forgePatches jar from libraries/
	librariesDir := filepath.Join(instanceDir, "libraries")
	if err := removeOldForgePatches(librariesDir); err != nil {
		return fmt.Errorf("removing old forgePatches: %w", err)
	}

	// Extract the zip into the instance directory
	if err := extractMultimcZip(zipPath, instanceDir); err != nil {
		return fmt.Errorf("extracting lwjgl3ify multimc zip: %w", err)
	}

	return nil
}

// UpdateServer downloads the forgePatches jar and places it at the server root.
// On servers, the forgePatches jar lives at the instance root (not in libraries/).
func UpdateServer(ctx context.Context, instanceDir, newVersion, githubToken string) error {
	// Remove old forgePatches jar from server root (fixed name on servers)
	oldPath := filepath.Join(instanceDir, "lwjgl3ify-forgePatches.jar")
	os.Remove(oldPath) // ignore error if it doesn't exist

	url := forgePatchesJarURL(newVersion)
	destPath := filepath.Join(instanceDir, "lwjgl3ify-forgePatches.jar")

	if err := downloader.DownloadToFile(ctx, url, destPath, githubToken, false); err != nil {
		return fmt.Errorf("downloading lwjgl3ify forgePatches jar: %w", err)
	}

	return nil
}

// removeOldForgePatches deletes any existing lwjgl3ify-*-forgePatches.jar from the given directory.
func removeOldForgePatches(dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if path == dir {
			return nil
		}
		if d.IsDir() {
			return filepath.SkipDir
		}
		name := d.Name()
		if strings.HasPrefix(name, "lwjgl3ify-") && strings.HasSuffix(name, "-forgePatches.jar") {
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("removing %s: %w", name, err)
			}
		}
		return nil
	})
}

// extractMultimcZip extracts the multimc zip into the instance directory.
// It only extracts: libraries/, patches/, and mmc-pack.json.
func extractMultimcZip(zipPath, instanceDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	allowedPrefixes := []string{"libraries/", "patches/", "mmc-pack.json"}

	for _, f := range r.File {
		name := f.Name

		// Only extract known safe paths
		allowed := false
		for _, prefix := range allowedPrefixes {
			if name == prefix || strings.HasPrefix(name, prefix) {
				allowed = true
				break
			}
		}
		if !allowed {
			continue
		}

		destPath := filepath.Join(instanceDir, name)

		// Security check: prevent path traversal
		cleanDest := filepath.Clean(instanceDir)
		cleanPath := filepath.Clean(destPath)
		if cleanPath != cleanDest && !strings.HasPrefix(cleanPath, cleanDest+string(os.PathSeparator)) {
			continue
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(destPath, 0755)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		outFile, err := os.Create(destPath)
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

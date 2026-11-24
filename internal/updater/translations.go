package updater

import (
	"archive/zip"
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/caedis/gtnh-daily-updater/internal/assets"
	"github.com/caedis/gtnh-daily-updater/internal/downloader"
	"github.com/caedis/gtnh-daily-updater/internal/logging"
	"github.com/caedis/gtnh-daily-updater/internal/manifest"
)

func refreshDailyTranslation(ctx context.Context, opts Options, gameDir, mode, side string, db *assets.AssetsDB) {
	if mode != manifest.ModeDaily || !strings.EqualFold(side, "client") {
		return
	}

	locale, release, err := updateTranslation(ctx, gameDir, db, opts.GithubToken)
	if locale == "" {
		locale = "unknown"
	}
	if release == "" {
		release = "none"
	}
	switch {
	case err != nil:
		logging.Infof("  Translation (%s, %s): update failed: %v\n", locale, release, err)
	case release == "none":
		logging.Infof("  Translation (%s, %s): skipped, no pack available\n", locale, release)
	default:
		logging.Infof("  Translation (%s, %s): updated\n", locale, release)
	}
}

func updateTranslation(ctx context.Context, gameDir string, db *assets.AssetsDB, token string) (locale, release string, err error) {
	locale, err = clientLocale(gameDir)
	if err != nil {
		return locale, "", err
	}
	for _, asset := range db.Translations.Versions {
		packLocale, ok := strings.CutSuffix(asset.VersionTag, "-latest")
		if !ok || !strings.EqualFold(packLocale, locale) ||
			!strings.HasPrefix(asset.Filename, "GTNH-"+packLocale+"-Translation-Daily-") ||
			!strings.HasSuffix(asset.Filename, ".zip") {
			continue
		}
		return packLocale, asset.Filename, installTranslation(ctx, gameDir, packLocale, asset, token)
	}
	return locale, "", nil
}

func clientLocale(gameDir string) (string, error) {
	options, err := os.Open(filepath.Join(gameDir, "options.txt"))
	if os.IsNotExist(err) {
		return "en_US", nil
	}
	if err != nil {
		return "", err
	}
	defer options.Close()

	locale := "en_US"
	scanner := bufio.NewScanner(options)
	for scanner.Scan() {
		if lang, ok := strings.CutPrefix(scanner.Text(), "lang:"); ok && strings.TrimSpace(lang) != "" {
			locale = strings.TrimSpace(lang)
		}
	}
	return locale, scanner.Err()
}

func installTranslation(ctx context.Context, gameDir, locale string, asset assets.VersionAsset, token string) error {
	stage, err := os.MkdirTemp(gameDir, ".translation-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)

	downloadURL := asset.BrowserDownloadURL
	useAPI := token != "" && asset.DownloadURL != ""
	if useAPI {
		downloadURL = asset.DownloadURL
	}
	archivePath := filepath.Join(stage, "translation.zip")
	if err := downloader.DownloadToFile(ctx, downloadURL, archivePath, token, useAPI); err != nil {
		return err
	}
	files, err := stageTranslation(archivePath, stage, locale)
	if err != nil {
		return fmt.Errorf("validating translation archive: %w", err)
	}

	root, err := os.OpenRoot(gameDir)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, name := range files {
		if err := checkTranslationDestination(root, name); err != nil {
			return err
		}
	}
	for _, name := range files {
		if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			return err
		}
		if err := root.Rename(filepath.Join(filepath.Base(stage), name), name); err != nil {
			return err
		}
	}
	return nil
}

func stageTranslation(archivePath, stage, locale string) ([]string, error) {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, err
	}
	defer archive.Close()

	var files []string
	seen := make(map[string]bool)
	for _, entry := range archive.File {
		name := strings.TrimSuffix(entry.Name, "/")
		localName, err := filepath.Localize(name)
		inConfig := strings.HasPrefix(name, "config/") || name == "config" && entry.Mode().IsDir()
		if err != nil || name != path.Clean(name) || strings.Contains(name, "\\") ||
			(!inConfig && name != "GregTech_"+locale+".lang") {
			return nil, fmt.Errorf("unexpected path %q", entry.Name)
		}
		if entry.Mode().IsDir() {
			continue
		}
		key := strings.ToLower(name)
		if !entry.Mode().IsRegular() || seen[key] {
			return nil, fmt.Errorf("invalid or duplicate file %q", entry.Name)
		}
		seen[key] = true
		if err := extractTranslationFile(entry, filepath.Join(stage, localName)); err != nil {
			return nil, err
		}
		files = append(files, localName)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("archive contains no translation files")
	}
	return files, nil
}

func extractTranslationFile(entry *zip.File, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	src, err := entry.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, src)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func checkTranslationDestination(root *os.Root, name string) error {
	for current := name; current != "."; current = filepath.Dir(current) {
		info, err := root.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if current == name && !info.Mode().IsRegular() || current != name && !info.IsDir() {
			return fmt.Errorf("cannot replace translation file at %q", current)
		}
	}
	return nil
}

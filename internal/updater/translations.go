package updater

import (
	"archive/zip"
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/caedis/gtnh-daily-updater/internal/assets"
	"github.com/caedis/gtnh-daily-updater/internal/config"
	"github.com/caedis/gtnh-daily-updater/internal/downloader"
	"github.com/caedis/gtnh-daily-updater/internal/logging"
	"github.com/caedis/gtnh-daily-updater/internal/manifest"
)

const maxTranslationBytes = 256 << 20

var errTranslationRollback = errors.New("translation rollback incomplete")
var errTranslationSymlink = errors.New("translation path contains a symbolic link")

func refreshDailyTranslation(ctx context.Context, opts Options, gameDir, mode string, state *config.LocalState, db *assets.AssetsDB) {
	if mode != manifest.ModeDaily || !strings.EqualFold(state.Side, "client") {
		return
	}

	locale, asset, err := findTranslation(gameDir, db)
	if locale == "" {
		locale = "unknown"
	}
	if err != nil {
		logging.Infof("  Translation (%s, none): update failed: %v\n", locale, err)
		return
	}
	if asset == nil {
		logging.Infof("  Translation (%s, none): skipped, no pack available\n", locale)
		return
	}
	release := asset.Filename
	if !opts.Force && state.TranslationRelease == release {
		logging.Infof("  Translation (%s, %s): already installed\n", locale, release)
		return
	}
	if err := installTranslation(ctx, gameDir, locale, *asset, opts.GithubToken); err != nil {
		if errors.Is(err, errTranslationSymlink) {
			logging.Infof("  Translation (%s, %s): skipped: %v\n", locale, release, err)
		} else {
			logging.Infof("  Translation (%s, %s): update failed: %v\n", locale, release, err)
		}
		return
	}
	state.TranslationRelease = release
	if err := state.Save(opts.InstanceDir); err != nil {
		logging.Infof("  Translation (%s, %s): updated, but could not save the installed release: %v\n", locale, release, err)
		return
	}
	logging.Infof("  Translation (%s, %s): updated\n", locale, release)
}

func findTranslation(gameDir string, db *assets.AssetsDB) (string, *assets.VersionAsset, error) {
	locale, err := clientLocale(gameDir)
	if err != nil {
		return locale, nil, err
	}
	for _, asset := range db.Translations.Versions {
		packLocale, ok := strings.CutSuffix(asset.VersionTag, "-latest")
		if !ok || !strings.EqualFold(packLocale, locale) ||
			!strings.HasPrefix(asset.Filename, "GTNH-"+packLocale+"-Translation-Daily-") ||
			!strings.HasSuffix(asset.Filename, ".zip") {
			continue
		}
		return packLocale, &asset, nil
	}
	return locale, nil, nil
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

func installTranslation(ctx context.Context, gameDir, locale string, asset assets.VersionAsset, token string) (installErr error) {
	stage, err := os.MkdirTemp(gameDir, ".translation-")
	if err != nil {
		return err
	}
	defer func() {
		if !errors.Is(installErr, errTranslationRollback) {
			_ = os.RemoveAll(stage)
		}
	}()

	downloadURL := asset.BrowserDownloadURL
	useAPI := token != "" && asset.DownloadURL != ""
	if useAPI {
		downloadURL = asset.DownloadURL
	}
	archivePath := filepath.Join(stage, "translation.zip")
	if err := downloader.DownloadToFile(ctx, downloadURL, archivePath, token, useAPI); err != nil {
		return err
	}
	files, err := stageTranslation(archivePath, stage, locale, maxTranslationBytes)
	if err != nil {
		return fmt.Errorf("validating translation archive: %w", err)
	}

	root, err := os.OpenRoot(gameDir)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := applyStagedTranslation(root, filepath.Base(stage), files); err != nil {
		if errors.Is(err, errTranslationRollback) {
			return fmt.Errorf("%w (backups retained in %s)", err, stage)
		}
		return err
	}
	return nil
}

func applyStagedTranslation(root *os.Root, stage string, files []string) (applyErr error) {
	for _, name := range files {
		if err := checkTranslationPath(root, name, false); err != nil {
			return err
		}
	}
	type replacement struct {
		name   string
		backup string
	}
	var replaced []replacement
	defer func() {
		if applyErr == nil {
			return
		}
		for i := len(replaced) - 1; i >= 0; i-- {
			file := replaced[i]
			var err error
			if file.backup != "" {
				err = root.Rename(file.backup, file.name)
			} else {
				err = root.Remove(file.name)
			}
			if err != nil {
				applyErr = errors.Join(applyErr, fmt.Errorf("%w: %s: %v", errTranslationRollback, file.name, err))
			}
		}
	}()

	for _, name := range files {
		if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			return err
		}
		file := replacement{name: name}
		if _, err := root.Lstat(name); err == nil {
			file.backup = filepath.Join(stage, "backup", name)
			if err := root.MkdirAll(filepath.Dir(file.backup), 0o755); err != nil {
				return err
			}
			if err := root.Rename(name, file.backup); err != nil {
				return err
			}
			replaced = append(replaced, file)
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := root.Rename(filepath.Join(stage, name), name); err != nil {
			return err
		}
		if file.backup == "" {
			replaced = append(replaced, file)
		}
	}
	return nil
}

func stageTranslation(archivePath, stage, locale string, remaining int64) ([]string, error) {
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
		if entry.UncompressedSize64 > uint64(remaining) {
			return nil, fmt.Errorf("translation archive exceeds extraction limit")
		}
		written, err := extractTranslationFile(entry, filepath.Join(stage, localName), remaining)
		if err != nil {
			return nil, err
		}
		remaining -= written
		files = append(files, localName)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("archive contains no translation files")
	}
	return files, nil
}

func extractTranslationFile(entry *zip.File, dest string, limit int64) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return 0, err
	}
	src, err := entry.Open()
	if err != nil {
		return 0, err
	}
	defer src.Close()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return 0, err
	}
	written, copyErr := io.Copy(out, io.LimitReader(src, limit+1))
	closeErr := out.Close()
	if copyErr != nil {
		return 0, copyErr
	}
	if written > limit {
		return 0, fmt.Errorf("translation archive exceeds extraction limit")
	}
	return written, closeErr
}

func checkTranslationPath(root *os.Root, current string, directory bool) error {
	if parent := filepath.Dir(current); parent != "." {
		if err := checkTranslationPath(root, parent, true); err != nil {
			return err
		}
	}
	info, err := root.Lstat(current)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s", errTranslationSymlink, current)
	}
	if directory && !info.IsDir() || !directory && !info.Mode().IsRegular() {
		return fmt.Errorf("cannot replace translation file at %q", current)
	}
	return nil
}

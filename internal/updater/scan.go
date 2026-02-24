package updater

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/caedis/gtnh-daily-updater/internal/assets"
	"github.com/caedis/gtnh-daily-updater/internal/config"
	"github.com/caedis/gtnh-daily-updater/internal/logging"
	"github.com/caedis/gtnh-daily-updater/internal/manifest"
	"github.com/caedis/gtnh-daily-updater/internal/side"
)

// scanInstalledMods reads the mods directory and matches jar filenames against the
// assets DB to determine what's actually installed. Returns a map of mod name to
// InstalledMod. Unmatched jars are silently skipped.
func scanInstalledMods(modsDir string, filenameIdx map[string][]assets.FilenameMatch, manifestMods map[string]manifest.ModInfo, excludeSet map[string]bool, sideMode string) (map[string]config.InstalledMod, error) {
	mods := make(map[string]config.InstalledMod)
	err := filepath.WalkDir(modsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}

		if filepath.Ext(d.Name()) != ".jar" {
			return nil
		}

		fn := d.Name()

		matches := filenameIdx[fn]
		if len(matches) == 0 {
			logging.Debugf("Verbose: unmatched jar skipped during scan: %s\n", fn)
			return nil
		}

		// Pick the best match
		match := pickBestMatch(matches, manifestMods)

		// Skip excluded mods
		if excludeSet[match.ModName] {
			logging.Debugf("Verbose: excluded mod skipped during scan: %s\n", match.ModName)
			return nil
		}

		// Use manifest side when available (more accurate than assets DB),
		// and filter using that effective side.
		modSide := match.Side
		if manifestInfo, ok := manifestMods[match.ModName]; ok {
			modSide = manifestInfo.Side
		}
		s := side.Parse(modSide)
		if !s.IncludedIn(sideMode) {
			logging.Debugf("Verbose: side-filtered mod skipped during scan: %s side=%s side-mode=%s\n", match.ModName, modSide, sideMode)
			return nil
		}

		mods[match.ModName] = config.InstalledMod{
			Version:  match.Version,
			Filename: fn,
			Side:     modSide,
		}
		logging.Debugf("Verbose: scanned mod %s version=%s filename=%s side=%s\n", match.ModName, match.Version, fn, modSide)

		return nil
	})
	if err != nil {
		return nil, err
	}

	return mods, nil
}

// pickBestMatch selects the best filename match when there are multiple candidates.
// It prefers mods that are in the current manifest.
func pickBestMatch(matches []assets.FilenameMatch, manifestMods map[string]manifest.ModInfo) assets.FilenameMatch {
	if len(matches) == 1 {
		return matches[0]
	}

	// Prefer matches that are in the manifest
	for _, m := range matches {
		if _, ok := manifestMods[m.ModName]; ok {
			return m
		}
	}

	// Fall back to first match
	return matches[0]
}

func listTopLevelJarFiles(modsDir string) (map[string]bool, error) {
	files := make(map[string]bool)
	err := filepath.WalkDir(modsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if path == modsDir {
			return nil
		}
		if d.IsDir() {
			return filepath.SkipDir
		}
		if filepath.Ext(d.Name()) == ".jar" {
			files[d.Name()] = true
		}
		return nil
	})
	return files, err
}

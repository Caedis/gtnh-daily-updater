package updater

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

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

// buildVersionPattern constructs a case-insensitive regexp that matches any jar
// filename that looks like the same mod but at a different version. It replaces
// the first occurrence of the version string in the escaped filename with `.*`.
// Returns false when the version string does not appear in the filename.
func buildVersionPattern(filename, version string) (*regexp.Regexp, bool) {
	if version == "" || !strings.Contains(filename, version) {
		return nil, false
	}
	escaped := regexp.QuoteMeta(filename)
	escapedVer := regexp.QuoteMeta(version)
	patStr := strings.Replace(escaped, escapedVer, `.*`, 1)
	return regexp.MustCompile(`(?i)^` + patStr + `$`), true
}

// detectStaleJars finds disk jars that belong to manifest mods not yet present
// in scannedMods. It compares each unresolved mod's expected filename pattern
// against unclaimed jars. Mods are processed in reverse alphabetical order so
// that more-specific names (e.g. "BuildCraftCompat") claim their jars before
// shorter prefix names (e.g. "buildcraft").
func detectStaleJars(
	manifestMods map[string]manifest.ModInfo,
	scannedMods map[string]config.InstalledMod,
	diskJars map[string]bool,
	db *assets.AssetsDB,
) map[string]config.InstalledMod {
	result := make(map[string]config.InstalledMod)

	// Collect manifest mods not yet accounted for in scannedMods.
	var unresolved []string
	for modName := range manifestMods {
		if _, found := scannedMods[modName]; !found {
			unresolved = append(unresolved, modName)
		}
	}
	if len(unresolved) == 0 {
		return result
	}

	// Reverse alpha order: longer/more-specific names claim jars first.
	slices.Sort(unresolved)
	slices.Reverse(unresolved)

	// Build the set of already-claimed jar filenames.
	claimedJars := make(map[string]bool, len(scannedMods))
	for _, installed := range scannedMods {
		if installed.Filename != "" {
			claimedJars[installed.Filename] = true
		}
	}

	// Sorted jar list for deterministic scanning.
	jarList := slices.Sorted(maps.Keys(diskJars))

	for _, modName := range unresolved {
		info := manifestMods[modName]
		filename, ok := db.FilenameForVersion(modName, info.Version)
		if !ok {
			continue
		}
		pat, ok := buildVersionPattern(filename, info.Version)
		if !ok {
			continue
		}
		for _, jar := range jarList {
			if claimedJars[jar] {
				continue
			}
			if pat.MatchString(jar) {
				result[modName] = config.InstalledMod{
					Version:  "",
					Filename: jar,
					Side:     info.Side,
				}
				claimedJars[jar] = true
				logging.Debugf("Verbose: stale jar detected mod=%s pattern=%s matched=%s\n", modName, pat.String(), jar)
				break
			}
		}
	}

	return result
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

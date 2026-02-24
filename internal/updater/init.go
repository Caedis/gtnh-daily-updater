package updater

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/caedis/gtnh-daily-updater/internal/assets"
	"github.com/caedis/gtnh-daily-updater/internal/config"
	"github.com/caedis/gtnh-daily-updater/internal/configmerge"
	"github.com/caedis/gtnh-daily-updater/internal/logging"
	"github.com/caedis/gtnh-daily-updater/internal/manifest"
)

// Init initializes tracking for an existing GTNH installation.
// It scans the mods/ directory and matches jar filenames against the assets DB
// to determine what's actually installed, rather than assuming the latest manifest.
func Init(ctx context.Context, instanceDir, side, configVersion, mode, githubToken string) error {
	if side != "client" && side != "server" {
		return fmt.Errorf("side must be 'client' or 'server'")
	}
	resolvedMode, err := resolveInitMode(configVersion, mode)
	if err != nil {
		return err
	}
	logging.Debugf("Verbose: init start instance=%q side=%s mode=%s config-version=%q github-token=%t\n", instanceDir, side, resolvedMode, configVersion, githubToken != "")

	logging.Infoln("Fetching assets database...")
	db, err := assets.Fetch(ctx)
	if err != nil {
		return fmt.Errorf("fetching assets DB: %w", err)
	}
	logging.Debugf("Verbose: assets DB loaded mods=%d config-versions=%d\n", len(db.Mods), len(db.Config.Versions))

	// Resolve game directory (mods/ and config/ location)
	gameDir := config.GameDir(instanceDir)

	// Build reverse index: filename -> mod matches
	logging.Infoln("Scanning mods directory...")
	filenameIdx := db.BuildFilenameIndex()

	modsDir := filepath.Join(gameDir, "mods")
	// Fetch latest manifest to help disambiguate and for the manifest date
	logging.Infof("Fetching latest %s manifest...\n", resolvedMode)
	m, err := manifest.Fetch(ctx, resolvedMode)
	if err != nil {
		return fmt.Errorf("fetching manifest: %w", err)
	}
	logging.Debugf("Verbose: manifest fetched updated=%s config=%s\n", m.LastUpdated, m.Config)
	allManifestMods := m.AllMods()

	// Load existing state to preserve exclude/extra settings if re-initializing
	existingState, _ := config.Load(instanceDir)

	// Build exclude set to skip excluded mods during init
	excludeSet := make(map[string]bool)
	if existingState != nil {
		for _, name := range existingState.ExcludeMods {
			excludeSet[name] = true
		}
	}

	mods, err := scanInstalledMods(modsDir, filenameIdx, allManifestMods, excludeSet, side)
	if err != nil {
		return fmt.Errorf("scanning mods directory: %w", err)
	}
	logging.Debugf("Verbose: identified %d tracked mods from mods directory\n", len(mods))

	// Collect unmatched jars for reporting
	var unmatched []string
	err = filepath.WalkDir(modsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() || filepath.Ext(d.Name()) != ".jar" {
			return nil
		}
		fn := d.Name()
		if len(filenameIdx[fn]) == 0 {
			unmatched = append(unmatched, fn)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("scanning mods directory: %w", err)
	}

	// Determine config version
	if configVersion == "" {
		// Default to latest manifest's config version, but warn the user
		configVersion = m.Config
		logging.Infof("  No --config-version specified, assuming latest: %s\n", configVersion)
		logging.Infoln("  If your instance is out of date, specify the correct version with --config-version")
	}

	// Hash files tracked by this modpack version (config + other managed files).
	logging.Infoln("Hashing tracked modpack files...")
	hashes, err := configmerge.ComputeTrackedFileHashes(ctx, gameDir, db, configVersion, githubToken)
	if err != nil {
		logging.Infof("  Warning: could not hash full modpack file set for %s: %v\n", configVersion, err)
		logging.Infoln("  Falling back to config-only hashing for this init run.")
		hashes, err = configmerge.ComputeConfigHashes(gameDir)
		if err != nil {
			return fmt.Errorf("hashing configs: %w", err)
		}
	}
	logging.Debugf("Verbose: computed %d tracked file hashes\n", len(hashes))

	// We don't set ManifestDate so the next update will always detect changes
	state := &config.LocalState{
		Side:          side,
		Mode:          resolvedMode,
		ManifestDate:  "", // empty = force update on next run
		ConfigVersion: configVersion,
		ConfigHashes:  hashes,
		Mods:          mods,
	}

	// Preserve exclude/extra settings from existing state
	if existingState != nil {
		state.ExcludeMods = existingState.ExcludeMods
		state.ExtraMods = existingState.ExtraMods
	}

	if err := state.Save(instanceDir); err != nil {
		return fmt.Errorf("saving state: %w", err)
	}
	logging.Debugf("Verbose: init state saved with excluded=%d extras=%d\n", len(state.ExcludeMods), len(state.ExtraMods))

	logging.Infof("\nInitialized: detected %d mods (%s side)\n", len(mods), side)
	logging.Infof("  Config version: %s\n", configVersion)
	if len(unmatched) > 0 {
		logging.Infof("  %d jars not recognized (user-added or unknown):\n", len(unmatched))
		for _, fn := range unmatched {
			logging.Infof("    - %s\n", fn)
		}
	}
	if len(state.ExcludeMods) > 0 {
		logging.Infof("  %d excluded mod(s) preserved\n", len(state.ExcludeMods))
	}
	if len(state.ExtraMods) > 0 {
		logging.Infof("  %d extra mod(s) preserved\n", len(state.ExtraMods))
	}
	logging.Infoln("\nRun 'update' to bring the instance up to date.")
	return nil
}

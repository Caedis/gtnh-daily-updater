package updater

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/caedis/gtnh-daily-updater/internal/assets"
	"github.com/caedis/gtnh-daily-updater/internal/config"
	"github.com/caedis/gtnh-daily-updater/internal/fileutil"
	"github.com/caedis/gtnh-daily-updater/internal/gitconfigs"
	"github.com/caedis/gtnh-daily-updater/internal/logging"
	"github.com/caedis/gtnh-daily-updater/internal/manifest"
)

// Init initializes tracking for an existing GTNH installation.
// It scans the mods/ directory and matches jar filenames against the assets DB
// to determine what's actually installed, rather than assuming the latest manifest.
func Init(ctx context.Context, instanceDir, side, configVersion, mode string) error {
	if side != "client" && side != "server" {
		return fmt.Errorf("side must be 'client' or 'server'")
	}
	resolvedMode, err := resolveInitMode(configVersion, mode)
	if err != nil {
		return err
	}
	logging.Debugf("Verbose: init start instance=%q side=%s mode=%s config-version=%q\n", instanceDir, side, resolvedMode, configVersion)

	logging.Infoln("Fetching assets database...")
	db, err := assets.Fetch(ctx)
	if err != nil {
		return fmt.Errorf("fetching assets DB: %w", err)
	}
	logging.Debugf("Verbose: assets DB loaded mods=%d config-versions=%d\n", len(db.Mods), len(db.Config.Versions))

	// Resolve game directory (mods/ and config/ location)
	gameDir := config.GameDir(instanceDir)

	logging.Infoln("Backing up mods directory...")
	if err := backupModsDir(gameDir, instanceDir); err != nil {
		return fmt.Errorf("backing up mods: %w", err)
	}

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

	// Remove jar files for excluded mods
	if len(excludeSet) > 0 {
		allMods, err := scanInstalledMods(modsDir, filenameIdx, allManifestMods, nil, side)
		if err != nil {
			return fmt.Errorf("scanning mods for excluded jars: %w", err)
		}
		for name, installed := range allMods {
			if !excludeSet[name] || installed.Filename == "" {
				continue
			}
			jarPath := filepath.Join(modsDir, installed.Filename)
			if err := os.Remove(jarPath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("removing excluded mod %s: %w", name, err)
			}
			logging.Infof("  - Removed excluded mod %s (%s)\n", name, installed.Filename)
		}
	}

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

	// Initialize git-backed config tracking
	if !gitconfigs.IsGitAvailable() {
		logging.Infoln("  Warning: git not found — skipping config tracking. Install git to enable this feature.")
	} else {
		logging.Infoln("Initializing config git repo...")
		if err := gitconfigs.Init(ctx, gameDir, side, configVersion); err != nil {
			return fmt.Errorf("initializing config repo: %w", err)
		}
	}

	// We don't set ManifestDate so the next update will always detect changes
	state := &config.LocalState{
		Side:          side,
		Mode:          resolvedMode,
		ManifestDate:  "", // empty = force update on next run
		ConfigVersion: configVersion,
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

func backupModsDir(gameDir, instanceDir string) error {
	modsDir := filepath.Join(gameDir, "mods")
	if _, err := os.Stat(modsDir); os.IsNotExist(err) {
		return nil
	}
	backupDir := filepath.Join(instanceDir, ".gtnh-mods-backup-"+time.Now().Format("2006-01-02"))
	if err := os.RemoveAll(backupDir); err != nil {
		return fmt.Errorf("clearing old mods backup: %w", err)
	}
	logging.Infof("  Backed up mods to %s\n", backupDir)
	return fileutil.CopyDir(modsDir, backupDir)
}

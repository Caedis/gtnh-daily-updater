package updater

import (
	"context"
	"path/filepath"

	"github.com/caedis/gtnh-daily-updater/internal/config"
	"github.com/caedis/gtnh-daily-updater/internal/diff"
	"github.com/caedis/gtnh-daily-updater/internal/logging"
)

// Run performs the full update flow.
func Run(ctx context.Context, opts Options) (*UpdateResult, error) {
	opts = normalizeRunOptions(opts)
	logRunStart(opts)

	state, err := loadAndLogState(opts.InstanceDir)
	if err != nil {
		return nil, err
	}

	m, err := fetchAndLogManifest(ctx)
	if err != nil {
		return nil, err
	}

	db, err := fetchAndLogAssetsDB(ctx)
	if err != nil {
		return nil, err
	}

	gameDir := config.GameDir(opts.InstanceDir)
	modsDir := filepath.Join(gameDir, "mods")

	rollback := func(cause error) error { return cause }

	if err := refreshTrackedMods(state, db, m, modsDir); err != nil {
		return nil, err
	}

	resolvedExtras, extraDownloads, err := resolveConfiguredExtras(ctx, state, db, opts)
	if err != nil {
		return nil, err
	}

	computeOpts := &diff.ComputeOptions{ExcludeMods: state.ExcludeMods, ExtraMods: resolvedExtras}
	changes := diff.Compute(state, m, computeOpts)

	latestDownloads := make(map[string]resolvedExtra)
	if opts.Latest {
		resolveLatestVersions(ctx, db, changes, extraDownloads, latestDownloads, opts)
	}

	added, removed, updated, unchanged := diff.Summary(changes)
	logging.Debugf("Verbose: diff summary added=%d removed=%d updated=%d unchanged=%d\n", added, removed, updated, unchanged)
	result := &UpdateResult{
		OldVersion: state.ConfigVersion,
		NewVersion: m.Config,
		Added:      added,
		Removed:    removed,
		Updated:    updated,
		Unchanged:  unchanged,
	}

	if !opts.Force && !opts.DryRun && result.Added == 0 && result.Removed == 0 && result.Updated == 0 && state.ConfigVersion == m.Config {
		logging.Infoln("Already up to date.")
		return result, nil
	}

	if opts.DryRun {
		printDryRun(changes)
		return result, nil
	}

	needsDownload := selectDownloadChanges(changes)
	downloads, err := resolveDownloadsForChanges(needsDownload, db, opts, extraDownloads, latestDownloads)
	if err != nil {
		return nil, err
	}

	if err := ensureModsDir(modsDir, rollback); err != nil {
		return nil, err
	}
	if err := removeOutdatedJars(changes, state.Mods, modsDir, rollback); err != nil {
		return nil, err
	}

	cacheDir := resolveCacheDirectory(opts)
	if err := downloadMods(ctx, downloads, needsDownload, modsDir, opts, cacheDir, rollback); err != nil {
		return nil, err
	}
	if err := updateLwjgl3ifyIfNeeded(ctx, changes, state.Mode, opts, rollback); err != nil {
		return nil, err
	}
	if err := mergeConfigsIfNeeded(ctx, state, m, gameDir, db, opts, result, rollback); err != nil {
		return nil, err
	}
	if err := persistUpdatedState(state, changes, m, opts, db, extraDownloads, latestDownloads, rollback); err != nil {
		return nil, err
	}

	return result, nil
}

func printDryRun(changes []diff.ModChange) {
	added, removed, updated, unchanged := diff.Summary(changes)
	logging.Infof("\nDry run - no changes made:\n")
	logging.Infof("  %d would be added, %d removed, %d updated, %d unchanged\n", added, removed, updated, unchanged)

	for _, c := range changes {
		switch c.Type {
		case diff.Added:
			logging.Infof("  + %s %s\n", c.Name, c.NewVersion)
		case diff.Removed:
			logging.Infof("  - %s %s\n", c.Name, c.OldVersion)
		case diff.Updated:
			logging.Infof("  ~ %s %s -> %s\n", c.Name, c.OldVersion, c.NewVersion)
		}
	}
}

package updater

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/caedis/gtnh-daily-updater/internal/assets"
	"github.com/caedis/gtnh-daily-updater/internal/config"
	"github.com/caedis/gtnh-daily-updater/internal/configmerge"
	"github.com/caedis/gtnh-daily-updater/internal/diff"
	"github.com/caedis/gtnh-daily-updater/internal/downloader"
	"github.com/caedis/gtnh-daily-updater/internal/logging"
	"github.com/caedis/gtnh-daily-updater/internal/lwjgl3ify"
	"github.com/caedis/gtnh-daily-updater/internal/manifest"
)

func normalizeRunOptions(opts Options) Options {
	if opts.Concurrency < 1 {
		opts.Concurrency = 6
	}
	return opts
}

func logRunStart(opts Options) {
	logging.Debugf(
		"Verbose: update start instance=%q dry-run=%t force=%t latest=%t concurrency=%d no-cache=%t cache-dir=%q github-token=%t\n",
		opts.InstanceDir,
		opts.DryRun,
		opts.Force,
		opts.Latest,
		opts.Concurrency,
		opts.NoCache,
		opts.CacheDir,
		opts.GithubToken != "",
	)
}

func loadAndLogState(instanceDir string) (*config.LocalState, error) {
	state, err := config.Load(instanceDir)
	if err != nil {
		return nil, err
	}
	logging.Debugf(
		"Verbose: loaded state mode=%s manifest-date=%q config=%s mods=%d excluded=%d extras=%d\n",
		state.Mode,
		state.ManifestDate,
		state.ConfigVersion,
		len(state.Mods),
		len(state.ExcludeMods),
		len(state.ExtraMods),
	)
	return state, nil
}

func fetchAndLogManifest(ctx context.Context) (*manifest.DailyManifest, error) {
	logging.Infoln("Fetching latest daily manifest...")
	m, err := manifest.Fetch(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching manifest: %w", err)
	}
	logging.Debugf(
		"Verbose: fetched manifest version=%s updated=%s config=%s github-mods=%d external-mods=%d\n",
		m.Version,
		m.LastUpdated,
		m.Config,
		len(m.GithubMods),
		len(m.ExternalMods),
	)
	return m, nil
}

func fetchAndLogAssetsDB(ctx context.Context) (*assets.AssetsDB, error) {
	logging.Infoln("Fetching assets database...")
	db, err := assets.Fetch(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching assets DB: %w", err)
	}
	logging.Debugf("Verbose: assets DB loaded mods=%d config-versions=%d\n", len(db.Mods), len(db.Config.Versions))
	return db, nil
}

// FetchSharedData fetches the manifest and assets DB that can be reused
// across multiple sequential profile updates to save bandwidth.
func FetchSharedData(ctx context.Context) (*SharedData, error) {
	m, err := fetchAndLogManifest(ctx)
	if err != nil {
		return nil, err
	}
	db, err := fetchAndLogAssetsDB(ctx)
	if err != nil {
		return nil, err
	}
	return &SharedData{Manifest: m, AssetsDB: db}, nil
}

func resolveSharedData(ctx context.Context, shared *SharedData) (*manifest.DailyManifest, *assets.AssetsDB, error) {
	if shared != nil {
		return shared.Manifest, shared.AssetsDB, nil
	}
	m, err := fetchAndLogManifest(ctx)
	if err != nil {
		return nil, nil, err
	}
	db, err := fetchAndLogAssetsDB(ctx)
	if err != nil {
		return nil, nil, err
	}
	return m, db, nil
}

func refreshTrackedMods(state *config.LocalState, db *assets.AssetsDB, m *manifest.DailyManifest, modsDir string) error {
	logging.Infoln("Scanning mods directory...")
	allManifestMods := m.AllMods()

	filenameIdx := db.BuildFilenameIndex()
	// Do not apply excludes during refresh. Excluded manifest mods still need to
	// be detected on disk so diff can mark them as Removed and delete their jars.
	scannedMods, err := scanInstalledMods(modsDir, filenameIdx, allManifestMods, nil, state.Mode)
	if err != nil {
		return fmt.Errorf("scanning mods directory: %w", err)
	}

	// Preserve previously tracked mods when their recorded jar still exists on
	// disk but scan couldn't identify them. This covers versions fetched via
	// --latest that are newer than the assets DB index.
	diskFiles, err := listTopLevelJarFiles(modsDir)
	if err != nil {
		return fmt.Errorf("scanning mods directory: %w", err)
	}
	for _, modName := range slices.Sorted(maps.Keys(state.Mods)) {
		installed := state.Mods[modName]
		if _, already := scannedMods[modName]; already {
			continue
		}
		if installed.Filename != "" && diskFiles[installed.Filename] {
			scannedMods[modName] = installed
		}
	}

	state.Mods = scannedMods
	logging.Debugf("Verbose: scanned installed mods=%d\n", len(scannedMods))
	return nil
}

func resolveConfiguredExtras(ctx context.Context, state *config.LocalState, db *assets.AssetsDB, opts Options) (map[string]diff.ResolvedExtraMod, map[string]resolvedExtra, error) {
	resolvedExtras := make(map[string]diff.ResolvedExtraMod)
	extraDownloads := make(map[string]resolvedExtra)

	if len(state.ExtraMods) == 0 {
		return resolvedExtras, extraDownloads, nil
	}

	logging.Infof("Resolving %d extra mod(s)...\n", len(state.ExtraMods))
	var unresolvedExtras []string
	for _, name := range slices.Sorted(maps.Keys(state.ExtraMods)) {
		spec := state.ExtraMods[name]
		logging.Debugf("Verbose: resolving extra mod %s source=%q version=%q side=%q\n", name, spec.Source, spec.Version, spec.Side)
		resolved, dlInfo, err := resolveExtraMod(ctx, name, spec, db, opts.GithubToken, opts.Latest)
		if err != nil {
			unresolvedExtras = append(unresolvedExtras, fmt.Sprintf("%s (%v)", name, err))
			logging.Debugf("Verbose: failed resolving extra mod %s: %v\n", name, err)
			continue
		}
		resolvedExtras[name] = resolved
		extraDownloads[name] = dlInfo
		logging.Debugf("Verbose: resolved extra mod %s version=%s filename=%s github-api=%t\n", name, resolved.Version, dlInfo.Filename, dlInfo.IsGitHubAPI)
	}
	if len(unresolvedExtras) > 0 {
		return nil, nil, fmt.Errorf("failed to resolve extra mods: %s", strings.Join(unresolvedExtras, "; "))
	}

	return resolvedExtras, extraDownloads, nil
}

func selectDownloadChanges(changes []diff.ModChange) []diff.ModChange {
	var needsDownload []diff.ModChange
	for _, c := range changes {
		if c.Type == diff.Added || c.Type == diff.Updated {
			needsDownload = append(needsDownload, c)
		}
	}
	return needsDownload
}

func resolveDownloadsForChanges(needsDownload []diff.ModChange, db *assets.AssetsDB, opts Options, extraDownloads, latestDownloads map[string]resolvedExtra) ([]downloader.Download, error) {
	var downloads []downloader.Download
	var unresolved []string

	for _, c := range needsDownload {
		dl, ok := resolveModDownload(db, c.Name, c.NewVersion, opts.GithubToken, extraDownloads, latestDownloads)
		if !ok {
			unresolved = append(unresolved, c.Name)
			continue
		}
		downloads = append(downloads, dl)
		logging.Debugf(
			"Verbose: resolved download mod=%s version=%s filename=%s url=%s github-api=%t\n",
			c.Name,
			c.NewVersion,
			dl.Filename,
			dl.URL,
			dl.IsGitHubAPI,
		)
	}
	if len(unresolved) > 0 {
		return nil, fmt.Errorf("failed to resolve download URLs for: %s", strings.Join(unresolved, ", "))
	}

	return downloads, nil
}

func ensureModsDir(modsDir string, rollback func(error) error) error {
	if err := os.MkdirAll(modsDir, 0o755); err != nil {
		return rollback(fmt.Errorf("creating mods directory: %w", err))
	}
	return nil
}

func removeOutdatedJars(changes []diff.ModChange, installedMods map[string]config.InstalledMod, modsDir string, rollback func(error) error) error {
	for _, c := range changes {
		switch c.Type {
		case diff.Removed:
			if installed, ok := installedMods[c.Name]; ok && installed.Filename != "" {
				jarPath := filepath.Join(modsDir, installed.Filename)
				if err := os.Remove(jarPath); err != nil && !os.IsNotExist(err) {
					return rollback(fmt.Errorf("removing %s: %w", installed.Filename, err))
				}
				logging.Infof("  - Removed %s %s\n", c.Name, c.OldVersion)
			}
		case diff.Updated:
			if installed, ok := installedMods[c.Name]; ok && installed.Filename != "" {
				jarPath := filepath.Join(modsDir, installed.Filename)
				if err := os.Remove(jarPath); err != nil && !os.IsNotExist(err) {
					return rollback(fmt.Errorf("removing %s: %w", installed.Filename, err))
				}
			}
		}
	}
	return nil
}

func resolveCacheDirectory(opts Options) string {
	cacheDir := ""
	if !opts.NoCache {
		cacheDir = opts.CacheDir
		if cacheDir == "" {
			base := os.Getenv("XDG_CACHE_HOME")
			if base == "" {
				if home, err := os.UserHomeDir(); err == nil {
					base = filepath.Join(home, ".cache")
				}
			}
			if base != "" {
				cacheDir = filepath.Join(base, "gtnh-daily-updater", "mods")
			}
		}
		if cacheDir != "" {
			if err := os.MkdirAll(cacheDir, 0o755); err != nil {
				logging.Infof("  Warning: could not create cache dir %s: %v (continuing without cache)\n", cacheDir, err)
				cacheDir = ""
			}
		}
	}
	logging.Debugf("Verbose: cache directory=%q\n", cacheDir)
	return cacheDir
}

func downloadMods(ctx context.Context, downloads []downloader.Download, needsDownload []diff.ModChange, modsDir string, opts Options, cacheDir string, rollback func(error) error) error {
	if len(downloads) == 0 {
		return nil
	}

	for _, c := range needsDownload {
		switch c.Type {
		case diff.Added:
			logging.Infof("  + Adding %s %s\n", c.Name, c.NewVersion)
		case diff.Updated:
			logging.Infof("  ~ Updating %s %s → %s\n", c.Name, c.OldVersion, c.NewVersion)
		}
	}

	logging.Infof("Downloading %d mods...\n", len(downloads))
	results := downloader.Run(ctx, downloads, modsDir, opts.Concurrency, opts.GithubToken, cacheDir, func(p downloader.Progress) {
		logging.Infof("\r  [%d/%d] mods downloaded", p.Completed, p.Total)
	})
	logging.Infoln()

	var failed []string
	for _, r := range results {
		if r.Err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", r.Download.Filename, r.Err))
		}
	}
	if len(failed) > 0 {
		return rollback(fmt.Errorf("download failures: %s", strings.Join(failed, "; ")))
	}

	return nil
}

func updateLwjgl3ifyIfNeeded(ctx context.Context, changes []diff.ModChange, mode string, opts Options, rollback func(error) error) error {
	for _, c := range changes {
		if (c.Type == diff.Added || c.Type == diff.Updated) && lwjgl3ify.NeedsUpdate(c.Name) {
			logging.Infof("Updating lwjgl3ify launcher library to %s...\n", c.NewVersion)
			var err error
			if mode == "client" {
				err = lwjgl3ify.UpdateClient(ctx, opts.InstanceDir, c.NewVersion, opts.GithubToken)
			} else {
				err = lwjgl3ify.UpdateServer(ctx, opts.InstanceDir, c.NewVersion, opts.GithubToken)
			}
			if err != nil {
				return rollback(fmt.Errorf("updating lwjgl3ify launcher library: %w", err))
			}
			break
		}
	}
	return nil
}

func mergeConfigsIfNeeded(ctx context.Context, state *config.LocalState, m *manifest.DailyManifest, gameDir string, db *assets.AssetsDB, opts Options, result *UpdateResult, rollback func(error) error) error {
	if state.ConfigVersion == m.Config {
		return nil
	}

	logging.Infoln("Merging configs...")
	mergeResult, err := configmerge.MergeConfigs(ctx, gameDir, state.ConfigHashes, state.ConfigVersion, db, m.Config, opts.GithubToken)
	if err != nil {
		return rollback(fmt.Errorf("config merge failed: %w", err))
	}

	result.ConfigMerged = mergeResult.FilesMerged + mergeResult.FilesUpdated
	result.ConfigConflict = mergeResult.FilesConflict
	result.ConflictFiles = mergeResult.ConflictFiles
	state.ConfigHashes = mergeResult.NewHashes
	return nil
}

func persistUpdatedState(state *config.LocalState, changes []diff.ModChange, m *manifest.DailyManifest, opts Options, db *assets.AssetsDB, extraDownloads, latestDownloads map[string]resolvedExtra, rollback func(error) error) error {
	for _, c := range changes {
		switch c.Type {
		case diff.Added, diff.Updated:
			filename := ""
			if dl, ok := resolveModDownload(db, c.Name, c.NewVersion, opts.GithubToken, extraDownloads, latestDownloads); ok {
				filename = dl.Filename
			}
			state.Mods[c.Name] = config.InstalledMod{
				Version:  c.NewVersion,
				Filename: filename,
				Side:     c.Side,
			}
		case diff.Removed:
			delete(state.Mods, c.Name)
		}
	}
	logging.Debugf("Verbose: updated in-memory state mods=%d\n", len(state.Mods))

	state.ConfigVersion = m.Config
	state.ManifestDate = m.LastUpdated

	if err := state.Save(opts.InstanceDir); err != nil {
		return rollback(fmt.Errorf("saving state: %w", err))
	}
	logging.Debugf("Verbose: saved state with manifest-date=%s config=%s\n", state.ManifestDate, state.ConfigVersion)

	return nil
}

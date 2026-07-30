package updater

import (
	"context"
	"fmt"
	"strings"

	"github.com/caedis/gtnh-daily-updater/internal/assets"
	"github.com/caedis/gtnh-daily-updater/internal/config"
	"github.com/caedis/gtnh-daily-updater/internal/diff"
	"github.com/caedis/gtnh-daily-updater/internal/logging"
	"github.com/caedis/gtnh-daily-updater/internal/manifest"
)

// Status shows the current state vs latest available.
func Status(ctx context.Context, instanceDir, githubToken, curseforgeKey string) error {
	state, err := config.Load(instanceDir)
	if err != nil {
		return err
	}
	logging.Debugf(
		"Verbose: status state side=%s mode=%s manifest-date=%q config=%s display=%q mods=%d excluded=%d extras=%d\n",
		state.Side,
		resolveMode(state),
		state.ManifestDate,
		state.ConfigVersion,
		state.DisplayVersion,
		len(state.Mods),
		len(state.ExcludeMods),
		len(state.ExtraMods),
	)

	mode := resolveMode(state)
	logging.Infof("Fetching latest %s manifest...\n", mode)
	m, err := manifest.Fetch(ctx, mode)
	if err != nil {
		return fmt.Errorf("fetching manifest: %w", err)
	}
	logging.Debugf("Verbose: status manifest updated=%s config=%s\n", m.LastUpdated, m.Config)

	upToDate := m.LastUpdated == state.ManifestDate

	// Print what needs no lookup first, so a failed fetch below still leaves
	// the user with something.
	logging.Infof("Side:      %s\n", state.Side)
	logging.Infof("Mode:      %s\n", mode)

	// The counter for the latest build lives in the assets DB. An up-to-date
	// instance needs no counter, so it pays for no fetch.
	var db *assets.AssetsDB
	if !upToDate {
		logging.Infoln("Fetching assets database...")
		db, err = assets.Fetch(ctx)
		if err != nil {
			return fmt.Errorf("fetching assets DB: %w", err)
		}
	}

	current, latest := statusVersions(state, m, db, mode)
	logging.Infof("Current:   %s\n", current)
	logging.Infof("Latest:    %s\n", latest)

	upToDate = finalizeUpToDate(upToDate, current, latest)

	if upToDate {
		logging.Infoln("\nAlready up to date.")
		return nil
	}

	resolvedExtras := make(map[string]diff.ResolvedExtraMod)
	if len(state.ExtraMods) > 0 {
		var resolvedErr error
		resolvedExtras, _, resolvedErr = resolveConfiguredExtras(ctx, state, db, Options{GithubToken: githubToken, CurseForgeKey: curseforgeKey})
		if resolvedErr != nil {
			return fmt.Errorf("resolving extra mods: %w", resolvedErr)
		}
	}

	computeOpts := &diff.ComputeOptions{
		ExcludeMods: state.ExcludeMods,
		ExtraMods:   resolvedExtras,
	}

	changes := diff.Compute(state, m, computeOpts)
	added, removed, updated, unchanged := diff.Summary(changes)

	logging.Infof("\nChanges available:\n")
	logging.Infof("  %d added, %d removed, %d updated, %d unchanged\n", added, removed, updated, unchanged)

	if state.ConfigVersion != m.Config {
		logging.Infof("  Config: %s → %s\n", state.ConfigVersion, m.Config)
	}

	if len(state.ExcludeMods) > 0 {
		logging.Infof("  Excluding: %s\n", strings.Join(state.ExcludeMods, ", "))
	}
	if len(state.ExtraMods) > 0 {
		var names []string
		for name := range state.ExtraMods {
			names = append(names, name)
		}
		logging.Infof("  Extra mods: %s\n", strings.Join(names, ", "))
	}

	return nil
}

// finalizeUpToDate re-checks up-to-date status once the display strings are
// known: a manifest regenerated with no real change advances LastUpdated
// without a real difference, so identical Current/Latest strings also count.
// cheapUpToDate is the pre-computed m.LastUpdated == state.ManifestDate check,
// which avoids the assets DB fetch when it alone is already true.
func finalizeUpToDate(cheapUpToDate bool, current, latest string) bool {
	return cheapUpToDate || current == latest
}

// statusVersions returns the display strings for the Current and Latest lines.
// db is nil when the instance is up to date, in which case latest == current.
func statusVersions(state *config.LocalState, m *manifest.DailyManifest, db *assets.AssetsDB, mode string) (current, latest string) {
	current = state.DisplayVersion
	if current == "" {
		current = state.ConfigVersion
	}
	if db == nil {
		return current, current
	}
	return current, buildDisplayVersion(m, db, mode, m.Config, Options{}).Long
}

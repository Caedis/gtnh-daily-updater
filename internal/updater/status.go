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
func Status(ctx context.Context, instanceDir, githubToken string) error {
	state, err := config.Load(instanceDir)
	if err != nil {
		return err
	}
	logging.Debugf(
		"Verbose: status state mode=%s manifest-date=%q config=%s mods=%d excluded=%d extras=%d\n",
		state.Mode,
		state.ManifestDate,
		state.ConfigVersion,
		len(state.Mods),
		len(state.ExcludeMods),
		len(state.ExtraMods),
	)

	logging.Infoln("Fetching latest daily manifest...")
	m, err := manifest.Fetch(ctx)
	if err != nil {
		return fmt.Errorf("fetching manifest: %w", err)
	}
	logging.Debugf("Verbose: status manifest updated=%s config=%s\n", m.LastUpdated, m.Config)

	logging.Infof("Mode:      %s\n", state.Mode)
	logging.Infof("Current:   %s\n", state.ConfigVersion)
	logging.Infof("Latest:    %s\n", m.Config)
	logging.Infof("Updated:   %s\n", m.LastUpdated)

	if m.LastUpdated == state.ManifestDate {
		logging.Infoln("\nAlready up to date.")
		return nil
	}

	resolvedExtras := make(map[string]diff.ResolvedExtraMod)
	if len(state.ExtraMods) > 0 {
		logging.Infoln("Fetching assets database...")
		db, err := assets.Fetch(ctx)
		if err != nil {
			return fmt.Errorf("fetching assets DB: %w", err)
		}
		var resolvedErr error
		resolvedExtras, _, resolvedErr = resolveConfiguredExtras(ctx, state, db, Options{GithubToken: githubToken})
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

package cmd

import (
	"fmt"

	"github.com/caedis/gtnh-daily-updater/internal/assets"
	stateconfig "github.com/caedis/gtnh-daily-updater/internal/config"
	"github.com/caedis/gtnh-daily-updater/internal/configmerge"
	"github.com/caedis/gtnh-daily-updater/internal/logging"
	"github.com/spf13/cobra"
)

var configDiffAll bool

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Inspect tracked pack file state",
}

var configDiffCmd = &cobra.Command{
	Use:   "diff [path]",
	Short: "Show tracked file differences from the baseline",
	Long: `Compare tracked pack files against the baseline hashes saved in
.gtnh-daily-updater.json during init/update.

With a [path] argument, show a line diff for one file against the tracked
config pack version (supports both config-relative and config/ prefixed paths).`,
	Args: usageArgs(cobra.MaximumNArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		state, err := stateconfig.Load(instanceDir)
		if err != nil {
			return err
		}

		if len(args) == 1 {
			return runConfigSingleFileDiff(cmd, state, args[0])
		}

		return runConfigSummaryDiff(state)
	},
}

func runConfigSummaryDiff(state *stateconfig.LocalState) error {
	gameDir := stateconfig.GameDir(instanceDir)
	diffs, err := configmerge.DiffConfigFiles(gameDir, state.ConfigHashes, configDiffAll)
	if err != nil {
		return fmt.Errorf("diffing configs: %w", err)
	}

	if len(diffs) == 0 {
		logging.Infoln("No tracked file differences from baseline.")
		return nil
	}

	added := 0
	removed := 0
	modified := 0
	unchanged := 0

	logging.Infoln("Tracked file differences:")
	for _, d := range diffs {
		switch d.Status {
		case configmerge.DiffAdded:
			added++
			logging.Infof("  + %s\n", d.Path)
		case configmerge.DiffRemoved:
			removed++
			logging.Infof("  - %s\n", d.Path)
		case configmerge.DiffModified:
			modified++
			logging.Infof("  ~ %s\n", d.Path)
		case configmerge.DiffUnchanged:
			unchanged++
			logging.Infof("  = %s\n", d.Path)
		}
	}

	logging.Infof(
		"\nSummary: %d added, %d removed, %d modified",
		added, removed, modified,
	)
	if configDiffAll {
		logging.Infof(", %d unchanged", unchanged)
	}
	logging.Infoln()

	return nil
}

func runConfigSingleFileDiff(cmd *cobra.Command, state *stateconfig.LocalState, requestedPath string) error {
	logging.Infoln("Fetching assets database...")
	db, err := assets.Fetch(cmd.Context())
	if err != nil {
		return fmt.Errorf("fetching assets DB: %w", err)
	}

	gameDir := stateconfig.GameDir(instanceDir)
	result, err := configmerge.DiffFileAgainstConfigVersion(
		cmd.Context(),
		gameDir,
		db,
		state.ConfigVersion,
		getGithubToken(),
		requestedPath,
	)
	if err != nil {
		return fmt.Errorf("diffing file %q: %w", requestedPath, err)
	}

	switch result.Status {
	case configmerge.DiffUnchanged:
		logging.Infof("No differences for %s (config %s).\n", result.ResolvedPath, state.ConfigVersion)
	case configmerge.DiffAdded:
		logging.Infof("File added locally: %s\n", result.ResolvedPath)
	case configmerge.DiffRemoved:
		logging.Infof("File removed locally: %s\n", result.ResolvedPath)
	case configmerge.DiffModified:
		logging.Infof("File modified locally: %s\n", result.ResolvedPath)
	}

	if result.Diff != "" {
		logging.Infoln(result.Diff)
	}

	return nil
}

func init() {
	configDiffCmd.Flags().BoolVar(&configDiffAll, "all", false, "Include unchanged files")

	configCmd.AddCommand(configDiffCmd)
	rootCmd.AddCommand(configCmd)
}

package cmd

import (
	"fmt"

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
	Use:   "diff",
	Short: "Show tracked file differences from the baseline",
	Long: `Compare tracked pack files against the baseline hashes saved in
.gtnh-daily-updater.json during init/update.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		state, err := stateconfig.Load(instanceDir)
		if err != nil {
			return err
		}

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
	},
}

func init() {
	configDiffCmd.Flags().BoolVar(&configDiffAll, "all", false, "Include unchanged files")

	configCmd.AddCommand(configDiffCmd)
	rootCmd.AddCommand(configCmd)
}

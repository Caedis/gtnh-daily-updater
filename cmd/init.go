package cmd

import (
	"context"
	"fmt"

	"github.com/caedis/gtnh-daily-updater/internal/updater"
	"github.com/spf13/cobra"
)

var configVersion string

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize tracking for an existing GTNH installation",
	Long: `Scans the mods/ directory and matches jar filenames against the GTNH assets
database to determine what's currently installed.

--config is required. Specify the config version your instance is at
(e.g. "2.9.0-nightly-2026-02-10"). Config versions can be found at
<https://github.com/GTNewHorizons/GT-New-Horizons-Modpack/releases>
and they do not always match the date of the daily.

Use --mode experimental for experimental pack instances.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if installSide == "" {
			return wrapUsageError(fmt.Errorf("--side is required (client or server)"))
		}
		if configVersion == "" {
			return wrapUsageError(fmt.Errorf("--config is required (e.g. 2.9.0-nightly-2026-02-10); see <https://github.com/GTNewHorizons/GT-New-Horizons-Modpack/releases>; will not always be the day the daily was mode"))
		}
		return updater.Init(context.Background(), instanceDir, installSide, configVersion, mode)
	},
}

func init() {
	initCmd.Flags().StringVar(&configVersion, "config", "", "Current config version (e.g. 2.9.0-nightly-2026-02-10). Required.")
	initCmd.Flags().StringVar(&installSide, "side", "", "Install side: client or server")
	initCmd.Flags().StringVar(&mode, "mode", "", "Pack mode to use: daily or experimental (default: infer from --config, else daily)")
	rootCmd.AddCommand(initCmd)
}

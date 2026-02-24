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

If your instance is out of date, optionally specify --config to indicate
which nightly config version is installed (e.g. "2.9.0-nightly-2026-02-10").
If omitted, the latest config version is assumed.

Use --mode experimental for experimental pack instances.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if installSide == "" {
			return wrapUsageError(fmt.Errorf("--side is required (client or server)"))
		}
		return updater.Init(context.Background(), instanceDir, installSide, configVersion, mode, getGithubToken())
	},
}

func init() {
	initCmd.Flags().StringVar(&configVersion, "config", "", "Current config version (e.g. 2.9.0-nightly-2026-02-10). Defaults to latest if omitted.")
	initCmd.Flags().StringVar(&installSide, "side", "", "Install side: client or server")
	initCmd.Flags().StringVar(&mode, "mode", "", "Pack mode to use: daily or experimental (default: infer from --config, else daily)")
	rootCmd.AddCommand(initCmd)
}

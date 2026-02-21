package cmd

import (
	"context"

	"github.com/caedis/gtnh-daily-updater/internal/profile"
	"github.com/caedis/gtnh-daily-updater/internal/updater"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current vs latest version and change summary",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Apply profile defaults for flags not explicitly set by the user.
		if profileName != "" {
			p, err := profile.Load(profileName)
			if err != nil {
				return err
			}
			if p.InstanceDir != nil && !cmd.Flags().Changed("instance-dir") {
				instanceDir = *p.InstanceDir
			}
			if p.Mode != nil && !cmd.Flags().Changed("mode") {
				mode = *p.Mode
			}
		}
		return updater.Status(context.Background(), instanceDir, getGithubToken())
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

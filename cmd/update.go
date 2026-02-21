package cmd

import (
	"context"

	"github.com/caedis/gtnh-daily-updater/internal/logging"
	"github.com/caedis/gtnh-daily-updater/internal/updater"
	"github.com/spf13/cobra"
)

var (
	dryRun      bool
	force       bool
	latest      bool
	concurrency int
	cacheDir    string
	noCache     bool
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update mods and tracked pack files to the latest daily build",
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := updater.Options{
			InstanceDir: instanceDir,
			DryRun:      dryRun,
			Force:       force,
			Latest:      latest,
			Concurrency: concurrency,
			GithubToken: getGithubToken(),
			CacheDir:    cacheDir,
			NoCache:     noCache,
		}

		result, err := updater.Run(context.Background(), opts)
		if err != nil {
			return err
		}

		if result.OldVersion == result.NewVersion && !force {
			return nil
		}

		if dryRun {
			return nil
		}

		logging.Infof("\nUpdate complete: %s → %s\n", result.OldVersion, result.NewVersion)
		logging.Infof("  Mods: %d added, %d removed, %d updated, %d unchanged\n",
			result.Added, result.Removed, result.Updated, result.Unchanged)

		if result.ConfigMerged > 0 || result.ConfigConflict > 0 {
			logging.Infof("  Pack files: %d files merged", result.ConfigMerged)
			if result.ConfigConflict > 0 {
				logging.Infof(", %d conflicts → see .packnew files", result.ConfigConflict)
			}
			logging.Infoln()
		}

		if len(result.Skipped) > 0 {
			logging.Infof("  Skipped: %s\n", joinSkipped(result.Skipped))
		}

		return nil
	},
}

func init() {
	updateCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would change without modifying anything")
	updateCmd.Flags().BoolVar(&force, "force", false, "Force update even if already up to date")
	updateCmd.Flags().BoolVar(&latest, "latest", false, "Use latest non-pre versions for all mods instead of manifest-pinned versions")
	updateCmd.Flags().IntVar(&concurrency, "concurrency", 6, "Number of concurrent downloads")
	updateCmd.Flags().StringVar(&cacheDir, "cache-dir", "", "Directory for caching downloaded mods (default: ~/.cache/gtnh-daily-updater/mods/)")
	updateCmd.Flags().BoolVar(&noCache, "no-cache", false, "Disable download caching")
	rootCmd.AddCommand(updateCmd)
}

func joinSkipped(s []string) string {
	if len(s) == 0 {
		return ""
	}
	result := s[0]
	for _, v := range s[1:] {
		result += ", " + v
	}
	return result
}

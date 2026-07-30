package cmd

import (
	"context"

	"github.com/caedis/gtnh-daily-updater/internal/logging"
	"github.com/caedis/gtnh-daily-updater/internal/updater"
	"github.com/spf13/cobra"
)

var (
	dryRun         bool
	force          bool
	latest         bool
	concurrency    int
	cacheDir       string
	noCache        bool
	noVersionStamp bool
)

var updateCmdName = "update"

var updateCmd = &cobra.Command{
	Use:   updateCmdName,
	Short: "Update mods and tracked pack files to the latest manifest build",
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := updater.Options{
			InstanceDir:    instanceDir,
			DryRun:         dryRun,
			Force:          force,
			Latest:         latest,
			Concurrency:    concurrency,
			GithubToken:    getGithubToken(),
			CurseForgeKey:  getCurseForgeKey(),
			CacheDir:       cacheDir,
			NoCache:        noCache,
			NoVersionStamp: noVersionStamp,
		}

		result, err := updater.Run(context.Background(), opts)
		if err != nil {
			return err
		}

		if result.UpToDate || dryRun {
			// The up-to-date path still repairs a stale version stamp.
			if len(result.StampedFiles) > 0 {
				logging.Infof("  Version stamped into %d file(s)\n", len(result.StampedFiles))
			}
			return nil
		}

		logging.Infof("\nUpdate complete: %s\n", versionTransition(result.OldVersion, result.NewVersion))
		logging.Infof("  Mods: %d added, %d removed, %d updated, %d unchanged\n",
			result.Added, result.Removed, result.Updated, result.Unchanged)

		if result.ConfigUpdated {
			logging.Infof("  Pack configs: %s → %s\n", result.OldConfigVersion, result.NewConfigVersion)
		}

		if len(result.StampedFiles) > 0 {
			logging.Infof("  Version stamped into %d file(s)\n", len(result.StampedFiles))
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
	updateCmd.Flags().StringVar(&cacheDir, "cache-dir", "", "Directory for caching downloaded mods (default: OS user cache dir + /gtnh-daily-updater/mods/)")
	updateCmd.Flags().BoolVar(&noCache, "no-cache", false, "Disable download caching")
	updateCmd.Flags().BoolVar(&noVersionStamp, "no-version-stamp", false, "Do not write the pack version into config files, server.properties or instance.cfg")
	rootCmd.AddCommand(updateCmd)
}

// versionTransition renders the headline form: an arrow when the pack version
// moved, otherwise the current version with a note.
func versionTransition(old, new string) string {
	if old == new {
		return new + " (pack version unchanged)"
	}
	return old + " → " + new
}

// versionCell renders the table form: an arrow when the pack version moved,
// otherwise just the version. No prose — it sits in a column.
func versionCell(old, new string) string {
	if old == new {
		return new
	}
	return old + " → " + new
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

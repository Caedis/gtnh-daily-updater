package cmd

import (
	"context"
	"fmt"

	"github.com/caedis/gtnh-daily-updater/internal/logging"
	"github.com/caedis/gtnh-daily-updater/internal/profile"
	"github.com/caedis/gtnh-daily-updater/internal/updater"
	"github.com/spf13/cobra"
)

var (
	dryRunAll      bool
	forceAll       bool
	latestAll      bool
	concurrencyAll int
	cacheDirAll    string
	noCacheAll     bool
)

var updateAllCmd = &cobra.Command{
	Use:   "update-all <profile> [profile...]",
	Short: "Update multiple profiles sequentially, fetching the manifest and assets DB only once",
	Args:  usageArgs(cobra.MinimumNArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		logging.Infoln("Fetching manifest and assets database (shared)...")
		shared, err := updater.FetchSharedData(ctx)
		if err != nil {
			return err
		}

		type profileResult struct {
			name   string
			result *updater.UpdateResult
			err    error
		}
		results := make([]profileResult, 0, len(args))

		var firstErr error
		for _, name := range args {
			p, err := profile.Load(name)
			if err != nil {
				results = append(results, profileResult{name: name, err: err})
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			if p.InstanceDir == nil {
				err := fmt.Errorf("profile %q has no instance-dir set", name)
				results = append(results, profileResult{name: name, err: err})
				if firstErr == nil {
					firstErr = err
				}
				continue
			}

			// Build options: CLI-flag-wins over profile values.
			opts := updater.Options{
				InstanceDir: *p.InstanceDir,
				GithubToken: getGithubToken(),
				Shared:      shared,
			}

			if cmd.Flags().Changed("dry-run") {
				opts.DryRun = dryRunAll
			}
			if cmd.Flags().Changed("force") {
				opts.Force = forceAll
			}
			if cmd.Flags().Changed("latest") {
				opts.Latest = latestAll
			} else if p.Latest != nil {
				opts.Latest = *p.Latest
			}
			if cmd.Flags().Changed("concurrency") {
				opts.Concurrency = concurrencyAll
			} else if p.Concurrency != nil {
				opts.Concurrency = *p.Concurrency
			}
			if cmd.Flags().Changed("cache-dir") {
				opts.CacheDir = cacheDirAll
			} else if p.CacheDir != nil {
				opts.CacheDir = *p.CacheDir
			}
			if cmd.Flags().Changed("no-cache") {
				opts.NoCache = noCacheAll
			} else if p.NoCache != nil {
				opts.NoCache = *p.NoCache
			}

			// Per-profile verbose setting; CLI wins.
			profileVerbose := verbose
			if p.Verbose != nil && !cmd.Flags().Changed("verbose") {
				profileVerbose = *p.Verbose
			}
			logging.SetVerbose(profileVerbose)

			logging.Infof("\n=== Profile %q (%s) ===\n", name, *p.InstanceDir)

			res, runErr := updater.Run(ctx, opts)
			results = append(results, profileResult{name: name, result: res, err: runErr})
			if runErr != nil {
				logging.Infof("  Error: %v\n", runErr)
				if firstErr == nil {
					firstErr = runErr
				}
				continue
			}

			if dryRunAll || (res.Added == 0 && res.Removed == 0 && res.Updated == 0 && res.OldVersion == res.NewVersion && !forceAll) {
				continue
			}

			logging.Infof("\nUpdate complete: %s → %s\n", res.OldVersion, res.NewVersion)
			logging.Infof("  Mods: %d added, %d removed, %d updated, %d unchanged\n",
				res.Added, res.Removed, res.Updated, res.Unchanged)
			if res.ConfigMerged > 0 || res.ConfigConflict > 0 {
				logging.Infof("  Pack files: %d files merged", res.ConfigMerged)
				if res.ConfigConflict > 0 {
					logging.Infof(", %d conflicts → see .packnew files", res.ConfigConflict)
				}
				logging.Infoln()
			}
			if len(res.Skipped) > 0 {
				logging.Infof("  Skipped: %s\n", joinSkipped(res.Skipped))
			}
		}

		// Overall summary table.
		logging.Infoln("\n=== Summary ===")
		for _, r := range results {
			if r.err != nil {
				logging.Infof("  %-20s  ERROR  %v\n", r.name, r.err)
				continue
			}
			modsChanged := r.result.Added + r.result.Removed + r.result.Updated
			logging.Infof("  %-20s  OK     %s → %s   %d updated\n",
				r.name, r.result.OldVersion, r.result.NewVersion, modsChanged)
		}

		return firstErr
	},
}

func init() {
	updateAllCmd.Flags().BoolVar(&dryRunAll, "dry-run", false, "Show what would change without modifying anything")
	updateAllCmd.Flags().BoolVar(&forceAll, "force", false, "Force update even if already up to date")
	updateAllCmd.Flags().BoolVar(&latestAll, "latest", false, "Use latest non-pre versions for all mods instead of manifest-pinned versions")
	updateAllCmd.Flags().IntVar(&concurrencyAll, "concurrency", 6, "Number of concurrent downloads")
	updateAllCmd.Flags().StringVar(&cacheDirAll, "cache-dir", "", "Directory for caching downloaded mods (default: ~/.cache/gtnh-daily-updater/mods/)")
	updateAllCmd.Flags().BoolVar(&noCacheAll, "no-cache", false, "Disable download caching")
	rootCmd.AddCommand(updateAllCmd)
}

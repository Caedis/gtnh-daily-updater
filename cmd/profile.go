package cmd

import (
	"bytes"

	"github.com/BurntSushi/toml"
	"github.com/caedis/gtnh-daily-updater/internal/logging"
	"github.com/caedis/gtnh-daily-updater/internal/profile"
	"github.com/spf13/cobra"
)

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage saved option profiles",
}

// Flags for profile create
var (
	profInstanceDir *string
	profMode        *string
	profConcurrency *int
	profLatest      *bool
	profCacheDir    *string
	profNoCache     *bool
	profVerbose     *bool
)

var profileCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new profile",
	Args:  usageArgs(cobra.ExactArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		p := &profile.Profile{}

		if cmd.Flags().Changed("instance-dir") {
			p.InstanceDir = profInstanceDir
		}
		if cmd.Flags().Changed("mode") {
			p.Mode = profMode
		}
		if cmd.Flags().Changed("concurrency") {
			p.Concurrency = profConcurrency
		}
		if cmd.Flags().Changed("latest") {
			p.Latest = profLatest
		}
		if cmd.Flags().Changed("cache-dir") {
			p.CacheDir = profCacheDir
		}
		if cmd.Flags().Changed("no-cache") {
			p.NoCache = profNoCache
		}
		if cmd.Flags().Changed("verbose") {
			p.Verbose = profVerbose
		}
		if cmd.Flags().Changed("log-file") {
			p.LogFile = &logFile
		}

		if err := profile.Save(args[0], p); err != nil {
			return err
		}
		logging.Infof("Profile %q saved to %s\n", args[0], profile.Dir())
		return nil
	},
}

var profileListCmd = &cobra.Command{
	Use:   "list",
	Short: "List saved profiles",
	Args:  usageArgs(cobra.NoArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		names, err := profile.List()
		if err != nil {
			return err
		}
		if len(names) == 0 {
			logging.Infoln("No profiles saved.")
			return nil
		}
		for _, n := range names {
			logging.Infoln(n)
		}
		return nil
	},
}

var profileShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show a profile's contents",
	Args:  usageArgs(cobra.ExactArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := profile.Load(args[0])
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		if err := toml.NewEncoder(&buf).Encode(p); err != nil {
			return err
		}
		logging.Infof("%s", buf.String())
		return nil
	},
}

var profileDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a saved profile",
	Args:  usageArgs(cobra.ExactArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := profile.Delete(args[0]); err != nil {
			return err
		}
		logging.Infof("Profile %q deleted.\n", args[0])
		return nil
	},
}

func init() {
	// Wire up flags for create. We use local variables so they only apply to
	// this subcommand and don't collide with the root/update flags.
	profInstanceDir = profileCreateCmd.Flags().String("instance-dir", "", "Minecraft instance root directory")
	profMode = profileCreateCmd.Flags().String("mode", "", "Install mode: client or server")
	profConcurrency = profileCreateCmd.Flags().Int("concurrency", 6, "Number of concurrent downloads")
	profLatest = profileCreateCmd.Flags().Bool("latest", false, "Use latest non-pre versions")
	profCacheDir = profileCreateCmd.Flags().String("cache-dir", "", "Cache directory for downloaded mods")
	profNoCache = profileCreateCmd.Flags().Bool("no-cache", false, "Disable download caching")
	profVerbose = profileCreateCmd.Flags().Bool("verbose", false, "Enable verbose logging")

	profileCmd.AddCommand(profileCreateCmd, profileListCmd, profileShowCmd, profileDeleteCmd)
	rootCmd.AddCommand(profileCmd)
}

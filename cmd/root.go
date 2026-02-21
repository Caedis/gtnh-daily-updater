package cmd

import (
	"fmt"
	"os"

	"github.com/caedis/gtnh-daily-updater/internal/logging"
	"github.com/caedis/gtnh-daily-updater/internal/profile"
	"github.com/spf13/cobra"
)

var (
	instanceDir string
	mode        string
	githubToken string
	profileName string
	verbose     bool
	logFile     string
)

var rootCmd = &cobra.Command{
	Use:   "gtnh-daily-updater",
	Short: "Update tool for GTNH daily builds",
	Long:  "Automatically update GregTech: New Horizons daily modpack builds, including mod downloads and config merging.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
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
			if p.Concurrency != nil && !cmd.Flags().Changed("concurrency") {
				concurrency = *p.Concurrency
			}
			if p.Latest != nil && !cmd.Flags().Changed("latest") {
				latest = *p.Latest
			}
			if p.CacheDir != nil && !cmd.Flags().Changed("cache-dir") {
				cacheDir = *p.CacheDir
			}
			if p.NoCache != nil && !cmd.Flags().Changed("no-cache") {
				noCache = *p.NoCache
			}
			if p.Verbose != nil && !cmd.Flags().Changed("verbose") {
				verbose = *p.Verbose
			}
			if p.LogFile != nil && !cmd.Flags().Changed("log-file") {
				logFile = *p.LogFile
			}
		}

		logging.SetVerbose(verbose)
		if err := logging.SetOutputFile(logFile); err != nil {
			return fmt.Errorf("opening log file %q: %w", logFile, err)
		}
		return nil
	},
}

func Execute() {
	err := rootCmd.Execute()
	closeErr := logging.Close()
	if closeErr != nil {
		fmt.Fprintf(os.Stderr, "Error closing log file: %v\n", closeErr)
		if err == nil {
			os.Exit(1)
		}
	}
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&instanceDir, "instance-dir", "d", ".", "Minecraft instance root directory")
	rootCmd.PersistentFlags().StringVarP(&mode, "mode", "m", "", "Install mode: client or server")
	rootCmd.PersistentFlags().StringVar(&githubToken, "github-token", "", "GitHub token for private mod downloads (also reads GITHUB_TOKEN env)")
	rootCmd.PersistentFlags().StringVar(&profileName, "profile", "", "Load a saved option profile by name")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")
	rootCmd.PersistentFlags().StringVar(&logFile, "log-file", "", "Write command output to a log file")
}

func getGithubToken() string {
	if githubToken != "" {
		return githubToken
	}
	return os.Getenv("GITHUB_TOKEN")
}

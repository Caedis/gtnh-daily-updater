package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

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
	Use:           "gtnh-daily-updater",
	Short:         "Update tool for GTNH daily builds",
	Long:          "Automatically update GregTech: New Horizons daily modpack builds, including mod downloads and config merging.",
	SilenceUsage:  true,
	SilenceErrors: true,
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
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		if isUsageError(err) {
			if cmd, _, findErr := rootCmd.Find(os.Args[1:]); findErr == nil && cmd != nil {
				_ = cmd.Usage()
			} else {
				_ = rootCmd.Usage()
			}
		}
		os.Exit(1)
	}
}

func init() {
	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return wrapUsageError(err)
	})

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

type usageError struct {
	err error
}

func (e *usageError) Error() string {
	return e.err.Error()
}

func (e *usageError) Unwrap() error {
	return e.err
}

func wrapUsageError(err error) error {
	if err == nil {
		return nil
	}
	return &usageError{err: err}
}

func usageArgs(validate cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if validate == nil {
			return nil
		}
		if err := validate(cmd, args); err != nil {
			return wrapUsageError(err)
		}
		return nil
	}
}

func isUsageError(err error) bool {
	var ue *usageError
	if errors.As(err, &ue) {
		return true
	}

	msg := err.Error()
	return strings.HasPrefix(msg, "unknown command ")
}

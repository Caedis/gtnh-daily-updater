package cmd

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"

	stateconfig "github.com/caedis/gtnh-daily-updater/internal/config"
	"github.com/caedis/gtnh-daily-updater/internal/gitconfigs"
	"github.com/caedis/gtnh-daily-updater/internal/logging"
	"github.com/spf13/cobra"
)

var configDiffAll bool

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Inspect tracked pack file state",
}

var configDiffCmd = &cobra.Command{
	Use:   "diff [path]",
	Short: "Show tracked config file differences from the pack baseline",
	Long: `Compare tracked config files against the pack baseline using the git history
in the .gtnh-configs/ repository.

With a [path] argument, show the diff for that specific file.
Without arguments, shows git diff output of local changes vs. the pack baseline.`,
	Args: usageArgs(cobra.MaximumNArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		state, err := stateconfig.Load(instanceDir)
		if err != nil {
			return err
		}

		gameDir := stateconfig.GameDir(instanceDir)
		repoDir := gitconfigs.ConfigRepoDir(gameDir)

		// Build git diff command: compare local branch to the pack tag
		gitArgs := []string{"diff", state.ConfigVersion + ".." + gitconfigs.LocalBranch}
		if len(args) == 1 {
			gitArgs = append(gitArgs, "--", resolveConfigPath(args[0]))
		} else if !configDiffAll {
			gitArgs = append(gitArgs, "--stat")
		}

		out, err := runGitDiff(repoDir, gitArgs)
		if err != nil {
			return fmt.Errorf("git diff: %w", err)
		}

		if out == "" {
			logging.Infoln("No tracked file differences from pack baseline.")
			return nil
		}

		logging.Infoln(out)
		return nil
	},
}

func resolveConfigPath(p string) string {
	// Allow both "config/foo.cfg" and "foo.cfg" (relative to config/)
	if filepath.IsAbs(p) {
		return p
	}
	// Check if it already starts with a tracked dir name
	for _, prefix := range []string{"config/", "journeymap/", "resourcepacks/", "serverutilities/"} {
		if len(p) >= len(prefix) && p[:len(prefix)] == prefix {
			return p
		}
	}
	// Default to config/ prefix
	return filepath.Join("config", p)
}

func runGitDiff(repoDir string, args []string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoDir
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("%w\n%s", err, stderr.String())
		}
		return "", err
	}
	return out.String(), nil
}

func init() {
	configDiffCmd.Flags().BoolVar(&configDiffAll, "all", false, "Show full diff instead of --stat summary")

	configCmd.AddCommand(configDiffCmd)
	rootCmd.AddCommand(configCmd)
}

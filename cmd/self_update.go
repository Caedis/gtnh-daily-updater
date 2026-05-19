package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/caedis/gtnh-daily-updater/internal/globalconfig"
	"github.com/caedis/gtnh-daily-updater/internal/logging"
	"github.com/caedis/gtnh-daily-updater/internal/paths"
	"github.com/caedis/gtnh-daily-updater/internal/selfupdate"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const selfUpdateCmdName = "self-update"

var (
	selfUpdateYes        bool
	selfUpdatePrerelease bool
)

var selfUpdateCmd = &cobra.Command{
	Use:   selfUpdateCmdName,
	Short: "Download and install the latest release of this tool",
	Long: `Check GitHub for the latest release, verify its SHA256, and replace
the running binary. Prompts for confirmation unless --yes is given.`,
	Args: usageArgs(cobra.NoArgs),
	RunE: runSelfUpdate,
}

func init() {
	selfUpdateCmd.Flags().BoolVarP(&selfUpdateYes, "yes", "y", false, "Skip the confirmation prompt")
	selfUpdateCmd.Flags().BoolVar(&selfUpdatePrerelease, "prerelease", false, "Include pre-release versions")
	rootCmd.AddCommand(selfUpdateCmd)
}

func runSelfUpdate(cmd *cobra.Command, _ []string) error {
	includePre := selfUpdatePrerelease
	if !cmd.Flags().Changed("prerelease") {
		if cfg, err := globalconfig.Load(); err == nil {
			includePre = cfg.IncludePrereleases
		}
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	info, newer, err := selfupdate.CheckLatest(ctx, version, includePre)
	if err != nil {
		return fmt.Errorf("checking latest release: %w", err)
	}
	if !newer {
		logging.Infof("Already up to date (%s).\n", version)
		return nil
	}

	if !selfUpdateYes {
		ok, err := confirm(fmt.Sprintf("Update %s → %s. Proceed? [y/N]: ", version, info.Tag))
		if err != nil {
			return err
		}
		if !ok {
			logging.Infof("Aborted.\n")
			return nil
		}
	}

	cacheDir, err := paths.CacheDir()
	if err != nil {
		return fmt.Errorf("resolving cache dir: %w", err)
	}
	workDir := filepath.Join(cacheDir, "selfupdate")
	zipPath := filepath.Join(workDir, info.AssetName)
	binPath := filepath.Join(workDir, selfupdate.BinaryName()+".new")

	logging.Infof("Downloading %s...\n", info.AssetURL)
	if err := selfupdate.Download(ctx, info, zipPath); err != nil {
		return fmt.Errorf("downloading: %w", err)
	}

	if err := selfupdate.VerifySHA256(zipPath, info.SHA256); err != nil {
		_ = os.Remove(zipPath)
		return fmt.Errorf("verifying download: %w", err)
	}

	if err := selfupdate.ExtractBinary(zipPath, binPath); err != nil {
		return fmt.Errorf("extracting: %w", err)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving current executable: %w", err)
	}
	if err := selfupdate.Replace(exe, binPath); err != nil {
		return fmt.Errorf("replacing binary: %w", err)
	}
	_ = os.Remove(zipPath)

	logging.Infof("Updated to %s.\n", info.Tag)
	return nil
}

func confirm(prompt string) (bool, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false, fmt.Errorf("non-interactive stdin; pass --yes to skip prompt")
	}
	fmt.Fprint(os.Stderr, prompt)
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil {
		return false, err
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes", nil
}

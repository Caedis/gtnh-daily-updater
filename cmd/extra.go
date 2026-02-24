package cmd

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/caedis/gtnh-daily-updater/internal/assets"
	"github.com/caedis/gtnh-daily-updater/internal/config"
	"github.com/caedis/gtnh-daily-updater/internal/logging"
	"github.com/caedis/gtnh-daily-updater/internal/side"
	"github.com/spf13/cobra"
)

var (
	extraSource  string
	extraVersion string
	extraSide    string
)

var extraCmd = &cobra.Command{
	Use:   "extra",
	Short: "Manage extra mods",
	Long:  "Add, remove, or list extra mods installed alongside the daily manifest.",
}

var extraAddCmd = &cobra.Command{
	Use:   "add [mod name]",
	Short: "Add an extra mod",
	Long: `Add a mod not in the daily manifest. Sources:
  - Default: looks up mod name in the GTNH assets database
  - --source github:Owner/Repo: downloads from GitHub releases
  - --source https://example.com/mod.jar: downloads from direct URL`,
	Args: usageArgs(cobra.ExactArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		state, err := config.Load(instanceDir)
		if err != nil {
			return err
		}

		spec := config.ExtraModSpec{
			Version: extraVersion,
			Source:  extraSource,
		}
		normalizedSide, err := normalizeExtraSide(extraSide)
		if err != nil {
			return err
		}
		spec.Side = normalizedSide

		// Validate source
		ctx := context.Background()
		if spec.Source == "" {
			// Assets DB source — validate mod exists
			logging.Infoln("Fetching assets database...")
			db, err := assets.Fetch(ctx)
			if err != nil {
				return fmt.Errorf("fetching assets DB: %w", err)
			}
			entry := db.LookupMod(name)
			if entry == nil {
				return fmt.Errorf("mod %q not found in assets database", name)
			}
			logging.Infof("  Found %s in assets DB (latest: %s)\n", name, entry.LatestVersion)
		} else if repo, ok := strings.CutPrefix(spec.Source, "github:"); ok {
			// GitHub source — validate repo exists
			url := fmt.Sprintf("https://api.github.com/repos/%s", repo)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return fmt.Errorf("creating request: %w", err)
			}
			if token := getGithubToken(); token != "" {
				req.Header.Set("Authorization", "token "+token)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("checking GitHub repo: %w", err)
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusNotFound {
				return fmt.Errorf("GitHub repository %q not found", repo)
			}
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("GitHub API returned HTTP %d for %q", resp.StatusCode, repo)
			}
			logging.Infof("  Validated GitHub repo: %s\n", repo)
		} else if strings.HasPrefix(spec.Source, "http://") || strings.HasPrefix(spec.Source, "https://") {
			// Direct URL — just note it
			logging.Infof("  Direct URL source: %s\n", spec.Source)
		} else {
			return wrapUsageError(fmt.Errorf("invalid source %q: must be empty (assets DB), github:Owner/Repo, or a URL", spec.Source))
		}

		if state.ExtraMods == nil {
			state.ExtraMods = make(map[string]config.ExtraModSpec)
		}
		state.ExtraMods[name] = spec

		if err := state.Save(instanceDir); err != nil {
			return fmt.Errorf("saving state: %w", err)
		}

		logging.Infof("  Added extra mod: %s\n", name)
		return nil
	},
}

var extraRemoveCmd = &cobra.Command{
	Use:   "remove [mod names...]",
	Short: "Remove extra mods",
	Args:  usageArgs(cobra.MinimumNArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		state, err := config.Load(instanceDir)
		if err != nil {
			return err
		}

		if state.ExtraMods == nil {
			state.ExtraMods = make(map[string]config.ExtraModSpec)
		}

		for _, name := range args {
			if _, exists := state.ExtraMods[name]; !exists {
				logging.Infof("  %s is not in the extra mods list\n", name)
				continue
			}
			delete(state.ExtraMods, name)
			if _, installed := state.Mods[name]; installed {
				logging.Infof("  %s — will be removed on next update\n", name)
			} else {
				logging.Infof("  %s — removed from extra mods\n", name)
			}
		}

		if err := state.Save(instanceDir); err != nil {
			return fmt.Errorf("saving state: %w", err)
		}
		return nil
	},
}

var extraListCmd = &cobra.Command{
	Use:   "list",
	Short: "List extra mods",
	RunE: func(cmd *cobra.Command, args []string) error {
		state, err := config.Load(instanceDir)
		if err != nil {
			return err
		}

		if len(state.ExtraMods) == 0 {
			logging.Infoln("No extra mods configured.")
			return nil
		}

		logging.Infoln("Extra mods:")
		for name, spec := range state.ExtraMods {
			source := "assets DB"
			if spec.Source != "" {
				source = spec.Source
			}
			version := spec.Version
			if version == "" {
				version = "latest"
			}
			logging.Infof("  - %s (source: %s, version: %s, side: %s)\n", name, source, version, spec.Side)
		}
		return nil
	},
}

func init() {
	extraAddCmd.Flags().StringVar(&extraSource, "source", "", "Mod source: github:Owner/Repo or direct URL (default: assets DB)")
	extraAddCmd.Flags().StringVar(&extraVersion, "version", "", "Pin to specific version (default: latest)")
	extraAddCmd.Flags().StringVar(&extraSide, "side", "", "Mod side: CLIENT, SERVER, or BOTH (default: BOTH)")
	extraCmd.AddCommand(extraAddCmd)
	extraCmd.AddCommand(extraRemoveCmd)
	extraCmd.AddCommand(extraListCmd)
	rootCmd.AddCommand(extraCmd)
}

func normalizeExtraSide(raw string) (string, error) {
	canonical := side.Parse(strings.TrimSpace(raw))
	if canonical == "" {
		return string(side.Both), nil
	}

	switch canonical {
	case side.Client, side.Server, side.Both, side.ClientJ9, side.ServerJ9, side.BothJ9:
		return string(canonical), nil
	default:
		return "", wrapUsageError(fmt.Errorf("invalid --side %q: must be CLIENT, SERVER, BOTH, CLIENT_JAVA9, SERVER_JAVA9, or BOTH_JAVA9", raw))
	}
}

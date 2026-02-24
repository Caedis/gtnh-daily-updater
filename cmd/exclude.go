package cmd

import (
	"fmt"

	"github.com/caedis/gtnh-daily-updater/internal/config"
	"github.com/caedis/gtnh-daily-updater/internal/logging"
	"github.com/spf13/cobra"
)

var excludeCmd = &cobra.Command{
	Use:   "exclude",
	Short: "Manage excluded mods",
	Long:  "Add, remove, or list mods excluded from updates. Excluded mods in the manifest will be skipped during updates.",
}

var excludeAddCmd = &cobra.Command{
	Use:   "add [mod names...]",
	Short: "Exclude mods from updates",
	Args:  usageArgs(cobra.MinimumNArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		state, err := config.Load(instanceDir)
		if err != nil {
			return err
		}

		existing := make(map[string]bool)
		for _, name := range state.ExcludeMods {
			existing[name] = true
		}

		var added []string
		for _, name := range args {
			if existing[name] {
				logging.Infof("  %s is already excluded\n", name)
				continue
			}
			state.ExcludeMods = append(state.ExcludeMods, name)
			existing[name] = true
			added = append(added, name)

			if _, installed := state.Mods[name]; installed {
				logging.Infof("  %s — will be removed on next update\n", name)
			} else {
				logging.Infof("  %s — added to exclude list\n", name)
			}
		}

		if len(added) == 0 {
			return nil
		}

		if err := state.Save(instanceDir); err != nil {
			return fmt.Errorf("saving state: %w", err)
		}
		return nil
	},
}

var excludeRemoveCmd = &cobra.Command{
	Use:   "remove [mod names...]",
	Short: "Stop excluding mods",
	Args:  usageArgs(cobra.MinimumNArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		state, err := config.Load(instanceDir)
		if err != nil {
			return err
		}

		removeSet := make(map[string]bool)
		for _, name := range args {
			removeSet[name] = true
		}

		var kept []string
		for _, name := range state.ExcludeMods {
			if removeSet[name] {
				logging.Infof("  %s — removed from exclude list\n", name)
				delete(removeSet, name)
			} else {
				kept = append(kept, name)
			}
		}

		for name := range removeSet {
			logging.Infof("  %s was not in the exclude list\n", name)
		}

		state.ExcludeMods = kept

		if err := state.Save(instanceDir); err != nil {
			return fmt.Errorf("saving state: %w", err)
		}
		return nil
	},
}

var excludeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List excluded mods",
	RunE: func(cmd *cobra.Command, args []string) error {
		state, err := config.Load(instanceDir)
		if err != nil {
			return err
		}

		if len(state.ExcludeMods) == 0 {
			logging.Infoln("No mods excluded.")
			return nil
		}

		logging.Infoln("Excluded mods:")
		for _, name := range state.ExcludeMods {
			logging.Infof("  - %s\n", name)
		}
		return nil
	},
}

func init() {
	excludeCmd.AddCommand(excludeAddCmd)
	excludeCmd.AddCommand(excludeRemoveCmd)
	excludeCmd.AddCommand(excludeListCmd)
	rootCmd.AddCommand(excludeCmd)
}

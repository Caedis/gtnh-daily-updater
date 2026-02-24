package diff

import (
	"maps"
	"slices"

	"github.com/caedis/gtnh-daily-updater/internal/config"
	"github.com/caedis/gtnh-daily-updater/internal/manifest"
	"github.com/caedis/gtnh-daily-updater/internal/side"
)

type ChangeType int

const (
	Added ChangeType = iota
	Removed
	Updated
	Unchanged
)

type ModChange struct {
	Name       string
	Type       ChangeType
	OldVersion string
	NewVersion string
	Side       string
}

// ResolvedExtraMod carries the resolved version and side for an extra mod.
type ResolvedExtraMod struct {
	Version string
	Side    string
}

// ComputeOptions configures how Compute processes the diff.
type ComputeOptions struct {
	ExcludeMods []string
	ExtraMods   map[string]ResolvedExtraMod
}

// Compute compares the current local state against a new manifest and returns the list of changes.
func Compute(state *config.LocalState, newManifest *manifest.DailyManifest, opts *ComputeOptions) []ModChange {
	newMods := newManifest.AllMods()
	sideMode := state.Side

	// Build exclude set for O(1) lookups
	excludeSet := make(map[string]bool)
	if opts != nil {
		for _, name := range opts.ExcludeMods {
			excludeSet[name] = true
		}
	}

	var changes []ModChange

	// Check for added and updated mods
	for _, name := range slices.Sorted(maps.Keys(newMods)) {
		info := newMods[name]
		s := side.Parse(info.Side)
		if !s.IncludedIn(sideMode) {
			continue
		}

		// Skip excluded mods — if currently installed, mark as Removed
		if excludeSet[name] {
			if installed, exists := state.Mods[name]; exists {
				changes = append(changes, ModChange{
					Name:       name,
					Type:       Removed,
					OldVersion: installed.Version,
					Side:       installed.Side,
				})
			}
			continue
		}

		installed, exists := state.Mods[name]
		if !exists {
			changes = append(changes, ModChange{
				Name:       name,
				Type:       Added,
				NewVersion: info.Version,
				Side:       info.Side,
			})
		} else if installed.Version != info.Version {
			changes = append(changes, ModChange{
				Name:       name,
				Type:       Updated,
				OldVersion: installed.Version,
				NewVersion: info.Version,
				Side:       info.Side,
			})
		} else {
			changes = append(changes, ModChange{
				Name:       name,
				Type:       Unchanged,
				OldVersion: installed.Version,
				NewVersion: info.Version,
				Side:       info.Side,
			})
		}
	}

	// Check for removed mods (in state but not in manifest)
	for _, name := range slices.Sorted(maps.Keys(state.Mods)) {
		installed := state.Mods[name]
		if _, exists := newMods[name]; exists {
			continue
		}
		// Check if this is an extra mod — don't mark as removed
		if opts != nil {
			if _, isExtra := opts.ExtraMods[name]; isExtra {
				continue
			}
		}
		changes = append(changes, ModChange{
			Name:       name,
			Type:       Removed,
			OldVersion: installed.Version,
			Side:       installed.Side,
		})
	}

	// Handle extra mods
	if opts != nil {
		for _, name := range slices.Sorted(maps.Keys(opts.ExtraMods)) {
			extra := opts.ExtraMods[name]
			// Skip if already in manifest (manifest takes precedence unless excluded)
			if _, inManifest := newMods[name]; inManifest && !excludeSet[name] {
				continue
			}

			s := side.Parse(extra.Side)
			if !s.IncludedIn(sideMode) {
				continue
			}

			installed, exists := state.Mods[name]
			if !exists {
				changes = append(changes, ModChange{
					Name:       name,
					Type:       Added,
					NewVersion: extra.Version,
					Side:       extra.Side,
				})
			} else if installed.Version != extra.Version {
				changes = append(changes, ModChange{
					Name:       name,
					Type:       Updated,
					OldVersion: installed.Version,
					NewVersion: extra.Version,
					Side:       extra.Side,
				})
			} else {
				changes = append(changes, ModChange{
					Name:       name,
					Type:       Unchanged,
					OldVersion: installed.Version,
					NewVersion: extra.Version,
					Side:       extra.Side,
				})
			}
		}
	}

	return changes
}

// Summary returns counts by change type.
func Summary(changes []ModChange) (added, removed, updated, unchanged int) {
	for _, c := range changes {
		switch c.Type {
		case Added:
			added++
		case Removed:
			removed++
		case Updated:
			updated++
		case Unchanged:
			unchanged++
		}
	}
	return
}

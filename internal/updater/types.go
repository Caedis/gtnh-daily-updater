package updater

import (
	"github.com/caedis/gtnh-daily-updater/internal/assets"
	"github.com/caedis/gtnh-daily-updater/internal/manifest"
)

// SharedData holds pre-fetched remote data that can be reused across
// sequential profile updates to avoid redundant network fetches.
type SharedData struct {
	Manifest *manifest.DailyManifest
	AssetsDB *assets.AssetsDB
	Mode     string
}

type Options struct {
	InstanceDir string
	DryRun      bool
	Force       bool
	Latest      bool
	Concurrency int
	GithubToken string
	CacheDir    string
	NoCache     bool
	// Shared optionally supplies pre-fetched manifest and assets DB.
	// When non-nil, Run skips those network fetches.
	Shared *SharedData
}

type UpdateResult struct {
	OldVersion     string
	NewVersion     string
	Added          int
	Removed        int
	Updated        int
	Unchanged      int
	ConfigMerged   int
	ConfigConflict int
	ConflictFiles  []string
	Skipped        []string
}

// resolvedExtra holds download info for an extra mod resolved before download.
type resolvedExtra struct {
	URL         string
	Filename    string
	IsGitHubAPI bool
}

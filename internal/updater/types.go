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
	InstanceDir     string
	DryRun          bool
	Force           bool
	Latest          bool
	AllowPreRelease bool
	Concurrency     int
	GithubToken     string
	CurseForgeKey   string
	CacheDir        string
	NoCache         bool
	// NoVersionStamp disables writing the pack version into config files,
	// server.properties and instance.cfg.
	NoVersionStamp bool
	// Shared optionally supplies pre-fetched manifest and assets DB.
	// When non-nil, Run skips those network fetches.
	Shared *SharedData
}

type UpdateResult struct {
	// OldVersion and NewVersion hold the pack display version shown in
	// summaries, e.g. "2.9.x (Daily 648) - 2026-07-28". OldVersion falls back
	// to the config repo tag when no display version was recorded yet.
	OldVersion string
	NewVersion string
	// OldConfigVersion and NewConfigVersion hold the config repo tags.
	OldConfigVersion string
	NewConfigVersion string
	Added            int
	Removed          int
	Updated          int
	Unchanged        int
	ConfigUpdated    bool
	ConfigSkipped    bool
	// StampedFiles lists pack files whose version stamp was rewritten.
	StampedFiles []string
	Skipped      []string
	// UpToDate is set when Run exited early because nothing needed doing.
	// Callers use it to decide whether to print a summary, instead of
	// re-deriving the condition from the version fields.
	UpToDate bool
}

// resolvedExtra holds download info for an extra mod resolved before download.
type resolvedExtra struct {
	URL               string
	Filename          string
	IsGitHubAPI       bool
	ExpectedHash      string
	HashAlgo          string
	MavenFallbackHash string
}

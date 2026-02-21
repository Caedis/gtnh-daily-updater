package updater

type Options struct {
	InstanceDir string
	DryRun      bool
	Force       bool
	Latest      bool
	Concurrency int
	GithubToken string
	CacheDir    string
	NoCache     bool
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

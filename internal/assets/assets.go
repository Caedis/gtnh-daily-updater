package assets

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/caedis/gtnh-daily-updater/internal/maven"
	"github.com/caedis/gtnh-daily-updater/internal/semver"
)

const AssetsURL = "https://raw.githubusercontent.com/GTNewHorizons/DreamAssemblerXXL/master/gtnh-assets.json"

type AssetsDB struct {
	Config AssetEntry   `json:"config"`
	Mods   []AssetEntry `json:"mods"`

	// Index built after parsing
	modIndex map[string]*AssetEntry
}

type AssetEntry struct {
	Name          string         `json:"name"`
	LatestVersion string         `json:"latest_version"`
	Versions      []VersionAsset `json:"versions"`
	License       string         `json:"license"`
	Side          string         `json:"side"`
	Source        string         `json:"source"`
	Type          string         `json:"type"`
	Disabled      bool           `json:"disabled"`
}

type VersionAsset struct {
	VersionTag         string `json:"version_tag"`
	Filename           string `json:"filename"`
	DownloadURL        string `json:"download_url"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Prerelease         bool   `json:"prerelease"`
}

// Fetch downloads and parses the assets DB from GitHub.
func Fetch(ctx context.Context) (*AssetsDB, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, AssetsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching assets: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching assets: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading assets: %w", err)
	}

	var db AssetsDB
	if err := json.Unmarshal(data, &db); err != nil {
		return nil, fmt.Errorf("parsing assets: %w", err)
	}

	db.buildIndex()
	return &db, nil
}

func (db *AssetsDB) buildIndex() {
	db.modIndex = make(map[string]*AssetEntry, len(db.Mods))
	for i := range db.Mods {
		// Sort versions by semver descending so newest is first
		slices.SortFunc(db.Mods[i].Versions, func(a, b VersionAsset) int {
			return semver.Compare(b.VersionTag, a.VersionTag) // reversed for descending
		})
		db.modIndex[db.Mods[i].Name] = &db.Mods[i]
	}
}

// LookupMod finds a mod by name in the index.
func (db *AssetsDB) LookupMod(name string) *AssetEntry {
	return db.modIndex[name]
}

// IsGTNH returns true if the mod is hosted by GTNewHorizons (Source == "").
func (db *AssetsDB) IsGTNH(modName string) bool {
	entry := db.LookupMod(modName)
	return entry != nil && entry.Source == ""
}

// GitHubRepo returns the "owner/repo" string for a mod's GitHub repository.
// For GTNH-hosted mods (Source == ""), the repo is GTNewHorizons/<modName>.
// For external mods, it attempts to parse the repo from the download URLs.
// Returns empty string if the repo cannot be determined.
func (db *AssetsDB) GitHubRepo(modName string) string {
	entry := db.LookupMod(modName)
	if entry == nil {
		return ""
	}
	if entry.Source == "" {
		return "GTNewHorizons/" + modName
	}
	// Try to extract owner/repo from a GitHub download URL
	for _, v := range entry.Versions {
		for _, u := range []string{v.DownloadURL, v.BrowserDownloadURL} {
			if repo := parseGitHubRepo(u); repo != "" {
				return repo
			}
		}
	}
	return ""
}

// parseGitHubRepo extracts "owner/repo" from a GitHub URL.
// Handles api.github.com/repos/owner/repo/... and github.com/owner/repo/...
func parseGitHubRepo(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	// https://api.github.com/repos/owner/repo/...
	if after, ok := strings.CutPrefix(rawURL, "https://api.github.com/repos/"); ok {
		parts := strings.SplitN(after, "/", 3)
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1]
		}
	}
	// https://github.com/owner/repo/...
	if after, ok := strings.CutPrefix(rawURL, "https://github.com/"); ok {
		parts := strings.SplitN(after, "/", 3)
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1]
		}
	}
	return ""
}

// FilenameMatch represents a mod identified by its jar filename.
type FilenameMatch struct {
	ModName string
	Version string
	Side    string
}

// BuildFilenameIndex creates a reverse index from jar filename to mod name + version.
// For duplicate filenames, all matches are returned. For GTNH-hosted mods (Source == ""),
// Maven-style filenames (ModName-Version.jar) are also indexed.
func (db *AssetsDB) BuildFilenameIndex() map[string][]FilenameMatch {
	idx := make(map[string][]FilenameMatch)
	for _, mod := range db.Mods {
		isGTNH := mod.Source == ""
		for _, v := range mod.Versions {
			match := FilenameMatch{
				ModName: mod.Name,
				Version: v.VersionTag,
				Side:    mod.Side,
			}
			if v.Filename != "" {
				idx[v.Filename] = append(idx[v.Filename], match)
			}
			// Also index the Maven-style filename for GTNH-hosted mods
			if isGTNH {
				mavenFn := maven.MavenFilename(mod.Name, v.VersionTag)
				if mavenFn != v.Filename {
					idx[mavenFn] = append(idx[mavenFn], match)
				}
			}
		}
	}
	return idx
}

// LatestVersion returns the latest non-prerelease version tag for a mod.
func (db *AssetsDB) LatestVersion(modName string) (string, error) {
	entry := db.LookupMod(modName)
	if entry == nil {
		return "", fmt.Errorf("mod %q not found in assets DB", modName)
	}

	if entry.LatestVersion != "" {
		return entry.LatestVersion, nil
	}

	// Fallback: find the first non-prerelease version
	for _, v := range entry.Versions {
		if !v.Prerelease {
			return v.VersionTag, nil
		}
	}

	return "", fmt.Errorf("no stable version found for mod %q", modName)
}

// ResolveDownload finds the download URL and filename for a mod at a specific version.
// Returns the download URL, filename, and whether it's a GitHub API URL (needs auth for private repos).
func (db *AssetsDB) ResolveDownload(modName, version string) (url, filename string, isGitHubAPI bool, err error) {
	entry := db.LookupMod(modName)
	if entry == nil {
		return "", "", false, fmt.Errorf("mod %q not found in assets DB", modName)
	}

	for _, v := range entry.Versions {
		if v.VersionTag == version {
			dl := v.DownloadURL
			isAPI := strings.HasPrefix(dl, "https://api.github.com/")

			if isAPI {
				// Prefer browser_download_url for public repos (no auth needed)
				return v.BrowserDownloadURL, v.Filename, true, nil
			}
			return dl, v.Filename, false, nil
		}
	}

	return "", "", false, fmt.Errorf("version %q not found for mod %q", version, modName)
}

// ResolveDownloadWithAuth returns the GitHub API URL for authenticated download.
func (db *AssetsDB) ResolveDownloadWithAuth(modName, version string) (apiURL, filename string, err error) {
	entry := db.LookupMod(modName)
	if entry == nil {
		return "", "", fmt.Errorf("mod %q not found in assets DB", modName)
	}

	for _, v := range entry.Versions {
		if v.VersionTag == version {
			return v.DownloadURL, v.Filename, nil
		}
	}

	return "", "", fmt.Errorf("version %q not found for mod %q", version, modName)
}

// LatestNonPreVersion returns the latest non-prerelease version tag for a mod,
// filtering out both the Prerelease flag and "-pre" suffixed version tags.
func (db *AssetsDB) LatestNonPreVersion(modName string) (string, error) {
	entry := db.LookupMod(modName)
	if entry == nil {
		return "", fmt.Errorf("mod %q not found in assets DB", modName)
	}

	for _, v := range entry.Versions {
		if !v.Prerelease && !strings.HasSuffix(strings.ToLower(strings.TrimSpace(v.VersionTag)), "-pre") {
			return v.VersionTag, nil
		}
	}

	return "", fmt.Errorf("no stable non-pre version found for mod %q", modName)
}

// ResolveConfigDownload finds the download URL for a config version.
func (db *AssetsDB) ResolveConfigDownload(configVersion string) (url, filename string, isGitHubAPI bool, err error) {
	for _, v := range db.Config.Versions {
		if v.VersionTag == configVersion {
			dl := v.DownloadURL
			isAPI := strings.HasPrefix(dl, "https://api.github.com/")
			if isAPI {
				return v.BrowserDownloadURL, v.Filename, true, nil
			}
			return dl, v.Filename, false, nil
		}
	}
	return "", "", false, fmt.Errorf("config version %q not found in assets DB", configVersion)
}

// ResolveConfigDownloadWithAuth returns the GitHub API URL for authenticated config download.
func (db *AssetsDB) ResolveConfigDownloadWithAuth(configVersion string) (apiURL, filename string, err error) {
	for _, v := range db.Config.Versions {
		if v.VersionTag == configVersion {
			return v.DownloadURL, v.Filename, nil
		}
	}
	return "", "", fmt.Errorf("config version %q not found in assets DB", configVersion)
}

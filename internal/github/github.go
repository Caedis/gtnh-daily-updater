package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/caedis/gtnh-daily-updater/internal/semver"
)

// Release is the subset of GitHub's release API response we need.
type Release struct {
	TagName    string         `json:"tag_name"`
	Prerelease bool           `json:"prerelease"`
	Assets     []ReleaseAsset `json:"assets"`
}

// ReleaseAsset represents a downloadable file attached to a GitHub release.
type ReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	URL                string `json:"url"` // API URL for authenticated downloads
}

// LatestResult holds the result of a GitHub latest-release lookup.
type LatestResult struct {
	Version  string
	Filename string
	URL      string
	IsAPI    bool
}

const releasesPerPage = 25

var githubHTTPClient = http.DefaultClient

// PickPrimaryJar selects the primary mod jar from a list of GitHub release assets.
// It looks for a .jar file whose name ends with "-{version}.jar", which excludes
// secondary artifacts like -dev, -api, -sources jars that append a classifier
// after the version (e.g. "ModName-1.4.7-dev.jar").
// Also handles tags with a "v" prefix (e.g. tag "v1.4.7" → filename uses "1.4.7").
// Falls back to the first .jar only when the release has a single jar asset.
func PickPrimaryJar(releaseAssets []ReleaseAsset, version string) *ReleaseAsset {
	version = strings.TrimSpace(version)
	// Try with the version as-is, and stripped of "v" prefix
	suffixes := []string{"-" + version + ".jar"}
	if stripped := strings.TrimPrefix(version, "v"); stripped != version {
		suffixes = append(suffixes, "-"+stripped+".jar")
	}
	for i, suffix := range suffixes {
		suffixes[i] = strings.ToLower(suffix)
	}

	var jars []*ReleaseAsset
	for i, asset := range releaseAssets {
		name := strings.TrimSpace(asset.Name)
		if !strings.EqualFold(filepath.Ext(name), ".jar") {
			continue
		}
		nameLower := strings.ToLower(name)
		for _, suffix := range suffixes {
			if strings.HasSuffix(nameLower, suffix) {
				return &releaseAssets[i]
			}
		}
		jars = append(jars, &releaseAssets[i])
	}

	// Only fall back if there's exactly one jar (no ambiguity)
	if len(jars) == 1 {
		return jars[0]
	}
	return nil
}

// FetchLatestRelease fetches recent releases from a GitHub repo and returns
// the highest semver non-prerelease that has a .jar asset.
func FetchLatestRelease(ctx context.Context, repo, token string) (*LatestResult, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=%d", repo, releasesPerPage)
	releases, err := fetchReleases(ctx, apiURL, token)
	if err != nil {
		return nil, err
	}

	best, err := selectLatestResult(releases, token)
	if err != nil {
		return nil, fmt.Errorf("repo %s: %w", repo, err)
	}
	return best, nil
}

func fetchReleases(ctx context.Context, apiURL, token string) ([]Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	resp, err := githubHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}
	return releases, nil
}

func selectLatestResult(releases []Release, token string) (*LatestResult, error) {
	var best *LatestResult
	for _, rel := range releases {
		tag := strings.TrimSpace(rel.TagName)
		if tag == "" || rel.Prerelease || isPreReleaseTag(tag) {
			continue
		}
		// Compare against current best by semver before checking assets
		if best != nil && semver.Compare(tag, best.Version) <= 0 {
			continue
		}
		asset := PickPrimaryJar(rel.Assets, tag)
		if asset == nil {
			continue
		}

		downloadURL := strings.TrimSpace(asset.BrowserDownloadURL)
		isAPI := false
		if token != "" && strings.TrimSpace(asset.URL) != "" {
			downloadURL = strings.TrimSpace(asset.URL)
			isAPI = true
		}
		if downloadURL == "" {
			continue
		}

		best = &LatestResult{
			Version:  tag,
			Filename: strings.TrimSpace(asset.Name),
			URL:      downloadURL,
			IsAPI:    isAPI,
		}
	}

	if best == nil {
		return nil, fmt.Errorf("no non-prerelease with .jar asset found")
	}
	return best, nil
}

func isPreReleaseTag(tag string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(tag)), "-pre")
}

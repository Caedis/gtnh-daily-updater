package selfupdate

import (
	"context"
	"fmt"
	"strings"

	"github.com/caedis/gtnh-daily-updater/internal/github"
	"github.com/caedis/gtnh-daily-updater/internal/semver"
)

// LatestInfo describes the newest release asset for this platform.
type LatestInfo struct {
	Tag       string
	AssetName string
	AssetURL  string
	SHA256    string // hex-encoded, lowercase
}

// CheckLatest queries GitHub for the latest release of this tool and returns
// info about the matching platform asset. The bool is true when the latest tag
// is newer than currentVersion. If currentVersion is "dev" (unversioned build),
// returns (info, false, nil) — info populated so callers can still self-update,
// but startup notifier suppresses notice.
func CheckLatest(ctx context.Context, currentVersion string, includePre bool) (*LatestInfo, bool, error) {
	releases, err := github.FetchReleasesRaw(ctx, Repo, "")
	if err != nil {
		return nil, false, err
	}

	var best *github.Release
	for i, rel := range releases {
		tag := strings.TrimSpace(rel.TagName)
		if tag == "" {
			continue
		}
		if !includePre && (rel.Prerelease || semver.IsPreRelease(tag)) {
			continue
		}
		if best == nil || semver.Compare(tag, best.TagName) > 0 {
			best = &releases[i]
		}
	}
	if best == nil {
		return nil, false, fmt.Errorf("no release found for repo %s", Repo)
	}

	wantName := AssetName(strings.TrimPrefix(best.TagName, "v"))
	asset := findAsset(best.Assets, wantName)
	if asset == nil {
		// fall back to matching with the tag as-is (in case tag has no v-prefix)
		asset = findAsset(best.Assets, AssetName(best.TagName))
	}
	if asset == nil {
		return nil, false, fmt.Errorf("release %s has no asset %q", best.TagName, wantName)
	}

	sha, err := extractSHA256(asset.Digest)
	if err != nil {
		return nil, false, fmt.Errorf("release %s asset %s: %w", best.TagName, asset.Name, err)
	}

	url := strings.TrimSpace(asset.BrowserDownloadURL)
	if url == "" {
		return nil, false, fmt.Errorf("release %s asset %s has no download URL", best.TagName, asset.Name)
	}

	info := &LatestInfo{
		Tag:       best.TagName,
		AssetName: asset.Name,
		AssetURL:  url,
		SHA256:    sha,
	}

	newer := isNewer(currentVersion, best.TagName)
	return info, newer, nil
}

func findAsset(assets []github.ReleaseAsset, name string) *github.ReleaseAsset {
	for i := range assets {
		if strings.EqualFold(strings.TrimSpace(assets[i].Name), name) {
			return &assets[i]
		}
	}
	return nil
}

func extractSHA256(digest string) (string, error) {
	d := strings.TrimSpace(digest)
	const prefix = "sha256:"
	if !strings.HasPrefix(strings.ToLower(d), prefix) {
		return "", fmt.Errorf("missing or unsupported digest %q", digest)
	}
	return strings.ToLower(d[len(prefix):]), nil
}

func isNewer(current, latest string) bool {
	c := strings.TrimSpace(current)
	if c == "" || c == "dev" {
		return false
	}
	return semver.Compare(latest, c) > 0
}

package maven

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"

	"github.com/caedis/gtnh-daily-updater/internal/fileutil"
	"github.com/caedis/gtnh-daily-updater/internal/semver"
)

const repoBase = "https://nexus.gtnewhorizons.com/repository/releases/"

const gtnhGroup = "com.github.GTNewHorizons"

// searchBase is the Nexus REST search endpoint; a var so tests can override it.
var searchBase = "https://nexus.gtnewhorizons.com/service/rest/v1/search"

var HTTPClient = http.DefaultClient

var (
	groupCacheMu sync.Mutex
	groupCache   = map[string]string{}
)

type searchResponse struct {
	Items []searchItem `json:"items"`
}

type searchItem struct {
	Group   string `json:"group"`
	Version string `json:"version"`
}

type metadata struct {
	Versioning struct {
		Release  string `xml:"release"`
		Versions struct {
			Version []string `xml:"version"`
		} `xml:"versions"`
	} `xml:"versioning"`
}

func groupBaseURL(group string) string {
	return repoBase + strings.ReplaceAll(group, ".", "/") + "/"
}

func metadataURL(group, modName string) string {
	return groupBaseURL(group) + path.Join(url.PathEscape(modName), "maven-metadata.xml")
}

func downloadURL(group, modName, version string) (dlURL, filename string) {
	filename = MavenFilename(modName, version)
	dlURL = groupBaseURL(group) + url.PathEscape(modName) + "/" + url.PathEscape(version) + "/" + url.PathEscape(filename)
	return dlURL, filename
}

// DownloadURL resolves the mod's group and builds its jar download URL.
func DownloadURL(ctx context.Context, modName, version string) (dlURL, filename string, err error) {
	group, err := ResolveGroup(ctx, modName)
	if err != nil {
		return "", "", err
	}
	dlURL, filename = downloadURL(group, modName, version)
	return dlURL, filename, nil
}

// ResolveGroup finds the Maven group for a mod via the Nexus search API,
// preferring the GTNewHorizons group. Results are cached per mod name.
func ResolveGroup(ctx context.Context, modName string) (string, error) {
	groupCacheMu.Lock()
	if g, ok := groupCache[modName]; ok {
		groupCacheMu.Unlock()
		return g, nil
	}
	groupCacheMu.Unlock()

	g, err := searchGroup(ctx, modName)
	if err != nil {
		return "", err
	}

	groupCacheMu.Lock()
	groupCache[modName] = g
	groupCacheMu.Unlock()
	return g, nil
}

// ResetGroupCache clears the resolved-group cache. For test isolation.
func ResetGroupCache() {
	groupCacheMu.Lock()
	groupCache = map[string]string{}
	groupCacheMu.Unlock()
}

// searchGroup finds a mod's Maven group via the Nexus search API. The Maven
// fallback now depends on search availability. Only the first result page is
// read; that is fine since all pages of one artifact share the same group.
func searchGroup(ctx context.Context, modName string) (string, error) {
	u := searchBase + "?repository=releases&maven.artifactId=" + url.QueryEscape(modName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("creating Nexus search request: %w", err)
	}
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("searching Nexus: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("searching Nexus for %s: HTTP %d", modName, resp.StatusCode)
	}
	var sr searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return "", fmt.Errorf("parsing Nexus search: %w", err)
	}
	return selectGroup(sr.Items, modName)
}

// selectGroup prefers the GTNewHorizons group; otherwise returns the group of
// the newest-version item.
func selectGroup(items []searchItem, modName string) (string, error) {
	bestGroup, bestVer := "", ""
	for _, it := range items {
		g := strings.TrimSpace(it.Group)
		if g == "" {
			continue
		}
		if g == gtnhGroup {
			return gtnhGroup, nil
		}
		if bestGroup == "" || semver.Compare(it.Version, bestVer) > 0 {
			bestGroup, bestVer = g, it.Version
		}
	}
	if bestGroup == "" {
		return "", fmt.Errorf("no Nexus artifact found for %s", modName)
	}
	return bestGroup, nil
}

// LatestAnyVersion fetches Maven metadata for a mod and returns the latest
// version including pre-releases.
func LatestAnyVersion(ctx context.Context, modName string) (string, error) {
	group, err := ResolveGroup(ctx, modName)
	if err != nil {
		return "", err
	}
	md, err := fetchMetadata(ctx, metadataURL(group, modName))
	if err != nil {
		return "", err
	}

	best := ""
	for _, v := range md.Versioning.Versions.Version {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if best == "" || semver.Compare(v, best) > 0 {
			best = v
		}
	}
	release := strings.TrimSpace(md.Versioning.Release)
	if release != "" && (best == "" || semver.Compare(release, best) > 0) {
		best = release
	}
	if best == "" {
		return "", fmt.Errorf("no versions found in Maven metadata for %s", modName)
	}
	return best, nil
}

// LatestNonPreVersion fetches Maven metadata for a mod and returns the latest
// stable (non "-pre") version.
func LatestNonPreVersion(ctx context.Context, modName string) (string, error) {
	group, err := ResolveGroup(ctx, modName)
	if err != nil {
		return "", err
	}
	md, err := fetchMetadata(ctx, metadataURL(group, modName))
	if err != nil {
		return "", err
	}

	latest := latestStableVersion(md.Versioning.Versions.Version, md.Versioning.Release)
	if latest == "" {
		return "", fmt.Errorf("no stable non-pre version found in Maven metadata")
	}
	return latest, nil
}

func fetchMetadata(ctx context.Context, metadataURL string) (*metadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching Maven metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching Maven metadata from %s: HTTP %d", metadataURL, resp.StatusCode)
	}

	var md metadata
	if err := xml.NewDecoder(resp.Body).Decode(&md); err != nil {
		return nil, fmt.Errorf("parsing Maven metadata: %w", err)
	}
	return &md, nil
}

func latestStableVersion(versions []string, release string) string {
	best := ""
	for _, v := range versions {
		v = strings.TrimSpace(v)
		if v == "" || semver.IsPreRelease(v) {
			continue
		}
		if best == "" || semver.Compare(v, best) > 0 {
			best = v
		}
	}

	release = strings.TrimSpace(release)
	if release != "" && !semver.IsPreRelease(release) {
		if best == "" || semver.Compare(release, best) > 0 {
			best = release
		}
	}

	return best
}

// SanitizeComponent removes or replaces characters invalid in Maven artifact
// paths or filenames.
func SanitizeComponent(s string) string {
	return fileutil.SanitizeFilename(s)
}

func MavenFilename(modName, version string) string {
	return SanitizeComponent(modName) + "-" + SanitizeComponent(version) + ".jar"
}

// FetchSHA256 fetches the `<jarURL>.sha256` sidecar and returns the lowercase
// hex digest. A 404 returns ("", nil) so callers can degrade gracefully.
func FetchSHA256(ctx context.Context, jarURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jarURL+".sha256", nil)
	if err != nil {
		return "", fmt.Errorf("creating sha256 request: %w", err)
	}
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching sha256 sidecar: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("sha256 sidecar HTTP %d", resp.StatusCode)
	}
	buf := make([]byte, 256)
	n, _ := resp.Body.Read(buf)
	s := strings.TrimSpace(string(buf[:n]))
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(s), nil
}

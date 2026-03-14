package maven

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/caedis/gtnh-daily-updater/internal/fileutil"
	"github.com/caedis/gtnh-daily-updater/internal/semver"
)

const BaseURL = "https://nexus.gtnewhorizons.com/repository/releases/com/github/GTNewHorizons/"

var HTTPClient = http.DefaultClient

type metadata struct {
	Versioning struct {
		Release  string `xml:"release"`
		Versions struct {
			Version []string `xml:"version"`
		} `xml:"versions"`
	} `xml:"versioning"`
}

func MetadataURL(modName string) string {
	return BaseURL + path.Join(url.PathEscape(modName), "maven-metadata.xml")
}

func DownloadURL(modName, version string) (dlURL, filename string) {
	filename = MavenFilename(modName, version)
	dlURL = BaseURL + url.PathEscape(modName) + "/" + url.PathEscape(version) + "/" + url.PathEscape(filename)
	return dlURL, filename
}

// LatestAnyVersion fetches Maven metadata for a mod and returns the latest
// version including pre-releases.
func LatestAnyVersion(ctx context.Context, modName string) (string, error) {
	md, err := fetchMetadata(ctx, MetadataURL(modName))
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
	md, err := fetchMetadata(ctx, MetadataURL(modName))
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

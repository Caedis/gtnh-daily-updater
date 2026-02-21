package maven

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

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
		if v == "" || isPreReleaseTag(v) {
			continue
		}
		if best == "" || semver.Compare(v, best) > 0 {
			best = v
		}
	}

	release = strings.TrimSpace(release)
	if release != "" && !isPreReleaseTag(release) {
		if best == "" || semver.Compare(release, best) > 0 {
			best = release
		}
	}

	return best
}

func isPreReleaseTag(v string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(v)), "-pre")
}

// SanitizeComponent removes or replaces characters invalid in Maven artifact
// paths or filenames.
func SanitizeComponent(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		case r == '-' || r == '.' || r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		default:
			// skip invalid characters
		}
	}
	return b.String()
}

func MavenFilename(modName, version string) string {
	return SanitizeComponent(modName) + "-" + SanitizeComponent(version) + ".jar"
}

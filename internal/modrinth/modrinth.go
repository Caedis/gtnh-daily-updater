package modrinth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// GTNHGameVersion is the Minecraft version GTNH targets.
const GTNHGameVersion = "1.7.10"

// GTNHLoader is the mod loader used by GTNH.
const GTNHLoader = "forge"

// UserAgent identifies this tool to the Modrinth API (recommended by Modrinth).
// cmd/root.go overrides it with the build version on init.
var UserAgent = "github.com/caedis/gtnh-daily-updater/dev"

// baseURL and httpClient are vars so tests can override them.
var (
	baseURL    = "https://api.modrinth.com"
	httpClient = http.DefaultClient
)

// SetVersion updates the User-Agent string with the current build version.
func SetVersion(v string) {
	if v == "" {
		return
	}
	UserAgent = "github.com/caedis/gtnh-daily-updater/" + v
}

// File represents a file attached to a Modrinth version.
type File struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Primary  bool   `json:"primary"`
}

// Version represents a Modrinth project version.
type Version struct {
	ID            string   `json:"id"`
	ProjectID     string   `json:"project_id"`
	VersionNumber string   `json:"version_number"`
	VersionType   string   `json:"version_type"` // release | beta | alpha
	Loaders       []string `json:"loaders"`
	GameVersions  []string `json:"game_versions"`
	Files         []File   `json:"files"`
}

// ParseSource parses the part of a modrinth source after the "modrinth:" prefix.
// Accepted formats:
//   - "slug-or-id"                 — use latest matching version
//   - "slug-or-id/versionID"       — use that specific version
func ParseSource(s string) (project, versionID string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", fmt.Errorf("empty Modrinth source")
	}
	if proj, ver, hasVer := strings.Cut(s, "/"); hasVer {
		if proj == "" || ver == "" {
			return "", "", fmt.Errorf("invalid Modrinth source %q: expected slug[/versionID]", s)
		}
		return proj, ver, nil
	}
	return s, "", nil
}

// FetchLatestVersion returns the newest release version of a Modrinth project
// that matches the given game version and loader.
func FetchLatestVersion(ctx context.Context, project, gameVersion, loader string) (Version, error) {
	q := url.Values{}
	if gameVersion != "" {
		q.Set("game_versions", fmt.Sprintf("[%q]", gameVersion))
	}
	if loader != "" {
		q.Set("loaders", fmt.Sprintf("[%q]", loader))
	}
	endpoint := fmt.Sprintf("%s/v2/project/%s/version", baseURL, url.PathEscape(project))
	if encoded := q.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}

	req, err := newRequest(ctx, endpoint)
	if err != nil {
		return Version{}, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return Version{}, fmt.Errorf("fetching Modrinth versions for %s: %w", project, err)
	}
	defer resp.Body.Close()

	if err := checkStatus(resp.StatusCode, fmt.Sprintf("project %s versions", project)); err != nil {
		return Version{}, err
	}

	var versions []Version
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		return Version{}, fmt.Errorf("parsing Modrinth versions response: %w", err)
	}

	// Prefer release versions; fall back to first match if none.
	for _, v := range versions {
		if v.VersionType == "release" {
			return v, nil
		}
	}
	if len(versions) > 0 {
		return versions[0], nil
	}
	return Version{}, fmt.Errorf("no versions found for Modrinth project %s (gameVersion=%q loader=%q)", project, gameVersion, loader)
}

// FetchVersion returns a specific version by ID.
func FetchVersion(ctx context.Context, versionID string) (Version, error) {
	endpoint := fmt.Sprintf("%s/v2/version/%s", baseURL, url.PathEscape(versionID))

	req, err := newRequest(ctx, endpoint)
	if err != nil {
		return Version{}, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return Version{}, fmt.Errorf("fetching Modrinth version %s: %w", versionID, err)
	}
	defer resp.Body.Close()

	if err := checkStatus(resp.StatusCode, fmt.Sprintf("version %s", versionID)); err != nil {
		return Version{}, err
	}

	var v Version
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return Version{}, fmt.Errorf("parsing Modrinth version response: %w", err)
	}
	return v, nil
}

// PrimaryFile returns the primary jar file from a version, or the first file
// if none is flagged primary.
func PrimaryFile(v Version) (File, error) {
	if len(v.Files) == 0 {
		return File{}, fmt.Errorf("Modrinth version %s has no files", v.ID)
	}
	for _, f := range v.Files {
		if f.Primary {
			return f, nil
		}
	}
	return v.Files[0], nil
}

// ProjectExists checks whether a project slug/ID resolves on Modrinth.
func ProjectExists(ctx context.Context, project string) (bool, error) {
	endpoint := fmt.Sprintf("%s/v2/project/%s", baseURL, url.PathEscape(project))
	req, err := newRequest(ctx, endpoint)
	if err != nil {
		return false, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("checking Modrinth project %s: %w", project, err)
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("Modrinth API returned HTTP %d for project %s", resp.StatusCode, project)
	}
}

func newRequest(ctx context.Context, endpoint string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func checkStatus(code int, target string) error {
	switch code {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return fmt.Errorf("Modrinth resource not found: %s", target)
	case http.StatusTooManyRequests:
		return fmt.Errorf("Modrinth rate limit hit (HTTP 429) for %s", target)
	default:
		return fmt.Errorf("Modrinth API returned HTTP %d for %s", code, target)
	}
}

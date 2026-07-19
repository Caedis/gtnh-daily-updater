package modrinth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
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

// FileHashes holds the cryptographic hashes for a Modrinth file.
type FileHashes struct {
	SHA1   string `json:"sha1"`
	SHA512 string `json:"sha512"`
}

// File represents a file attached to a Modrinth version.
type File struct {
	URL      string     `json:"url"`
	Filename string     `json:"filename"`
	Primary  bool       `json:"primary"`
	Hashes   FileHashes `json:"hashes"`
}

// Version represents a Modrinth project version.
type Version struct {
	ID            string   `json:"id"`
	ProjectID     string   `json:"project_id"`
	VersionNumber string   `json:"version_number"`
	VersionType   string   `json:"version_type"` // release | beta | alpha
	DatePublished string   `json:"date_published"`
	Loaders       []string `json:"loaders"`
	GameVersions  []string `json:"game_versions"`
	Files         []File   `json:"files"`
}

// ParseChannel maps a release-channel name to the maximum version-type rank it
// permits (release=1, beta=2, alpha=3). Empty defaults to release. Unknown names error.
func ParseChannel(s string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "release":
		return 1, nil
	case "beta":
		return 2, nil
	case "alpha":
		return 3, nil
	default:
		return 0, fmt.Errorf("invalid Modrinth channel %q: must be release, beta, or alpha", s)
	}
}

// versionTypeRank maps a Modrinth version_type to a stability rank. Unknown types
// return 0 so they are excluded from channel filtering.
func versionTypeRank(t string) int {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "release":
		return 1
	case "beta":
		return 2
	case "alpha":
		return 3
	default:
		return 0
	}
}

// ParseSource parses the part of a modrinth source after the "modrinth:" prefix.
// Accepted formats:
//   - "slug-or-id"            latest matching version, release channel
//   - "slug-or-id@beta"       latest matching version in the given channel
//   - "slug-or-id/versionID"  that specific version
//
// A "@channel" suffix cannot be combined with a pinned version ID.
func ParseSource(s string) (project, versionID, channel string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", "", fmt.Errorf("empty Modrinth source")
	}
	rest, chanPart, hasChannel := strings.Cut(s, "@")
	if hasChannel {
		if _, cerr := ParseChannel(chanPart); cerr != nil {
			return "", "", "", cerr
		}
	}
	if proj, ver, hasVer := strings.Cut(rest, "/"); hasVer {
		if proj == "" || ver == "" {
			return "", "", "", fmt.Errorf("invalid Modrinth source %q: expected slug[/versionID]", s)
		}
		if hasChannel {
			return "", "", "", fmt.Errorf("channel %q cannot be combined with a pinned version ID", chanPart)
		}
		return proj, ver, "", nil
	}
	if rest == "" {
		return "", "", "", fmt.Errorf("invalid Modrinth source %q: expected slug[/versionID]", s)
	}
	return rest, "", chanPart, nil
}

// FetchLatestVersion returns the newest version of a Modrinth project that
// matches the given game version, loader, and channel (maxRank).
func FetchLatestVersion(ctx context.Context, project, gameVersion, loader string, maxRank int) (Version, error) {
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

	// Keep versions within the requested channel: 1 <= rank <= maxRank.
	var allowed []Version
	for _, v := range versions {
		if r := versionTypeRank(v.VersionType); r >= 1 && r <= maxRank {
			allowed = append(allowed, v)
		}
	}
	if len(allowed) == 0 {
		return Version{}, fmt.Errorf("no versions found for Modrinth project %s within channel (gameVersion=%q loader=%q)", project, gameVersion, loader)
	}

	// Newest date_published wins. Stable sort keeps API order for equal/unparseable dates.
	sort.SliceStable(allowed, func(i, j int) bool {
		return publishedTime(allowed[i].DatePublished).After(publishedTime(allowed[j].DatePublished))
	})
	return allowed[0], nil
}

// publishedTime parses a Modrinth date_published stamp, returning the zero time
// when empty or unparseable.
func publishedTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
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

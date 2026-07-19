package curseforge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// GTNHGameVersion is the Minecraft version GTNH targets.
const GTNHGameVersion = "1.7.10"

// baseURL and httpClient are vars so tests can override them.
var (
	baseURL    = "https://api.curseforge.com"
	httpClient = http.DefaultClient
)

// FileHash is a CurseForge file hash entry. Algo: 1=sha1, 2=md5.
type FileHash struct {
	Value string `json:"value"`
	Algo  int    `json:"algo"`
}

// File represents a CurseForge file entry.
type File struct {
	ID           int        `json:"id"`
	ModID        int        `json:"modId"`
	DisplayName  string     `json:"displayName"`
	FileName     string     `json:"fileName"`
	DownloadURL  string     `json:"downloadUrl"` // may be empty for some files
	ReleaseType  int        `json:"releaseType"` // 1=Release, 2=Beta, 3=Alpha
	GameVersions []string   `json:"gameVersions"`
	Hashes       []FileHash `json:"hashes"`
}

// SHA1 returns the sha1 hex digest for this file, or "" if not present.
func (f File) SHA1() string {
	for _, h := range f.Hashes {
		if h.Algo == 1 {
			return h.Value
		}
	}
	return ""
}

type fileResponse struct {
	Data File `json:"data"`
}

type filesResponse struct {
	Data []File `json:"data"`
}

type downloadURLResponse struct {
	Data string `json:"data"`
}

// ParseChannel maps a release-channel name to the maximum CurseForge releaseType
// it permits (1=Release, 2=Beta, 3=Alpha). Empty defaults to release. Unknown
// names error.
func ParseChannel(s string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "release":
		return 1, nil
	case "beta":
		return 2, nil
	case "alpha":
		return 3, nil
	default:
		return 0, fmt.Errorf("invalid CurseForge channel %q: must be release, beta, or alpha", s)
	}
}

// ParseSource parses the part of a curseforge source after the "curseforge:" prefix.
// Accepted formats:
//   - "12345"            project ID, latest release-channel file
//   - "12345@beta"       project ID, latest file in the given channel
//   - "12345/67890"      project ID + file ID, that specific file
//
// A "@channel" suffix cannot be combined with a pinned file ID. Returns
// projectID, fileID (0 if not specified), channel ("" when none given), and an error.
func ParseSource(s string) (projectID, fileID int, channel string, err error) {
	rest, chanPart, hasChannel := strings.Cut(s, "@")
	if hasChannel {
		if _, cerr := ParseChannel(chanPart); cerr != nil {
			return 0, 0, "", cerr
		}
	}
	if projStr, fileStr, hasFile := strings.Cut(rest, "/"); hasFile {
		if hasChannel {
			return 0, 0, "", fmt.Errorf("channel %q cannot be combined with a pinned file ID", chanPart)
		}
		projectID, err = strconv.Atoi(projStr)
		if err != nil || projectID <= 0 {
			return 0, 0, "", fmt.Errorf("invalid CurseForge project ID %q: must be a positive integer", projStr)
		}
		fileID, err = strconv.Atoi(fileStr)
		if err != nil || fileID <= 0 {
			return 0, 0, "", fmt.Errorf("invalid CurseForge file ID %q: must be a positive integer", fileStr)
		}
		return projectID, fileID, "", nil
	}
	projectID, err = strconv.Atoi(rest)
	if err != nil || projectID <= 0 {
		return 0, 0, "", fmt.Errorf("invalid CurseForge project ID %q: must be a positive integer", rest)
	}
	return projectID, 0, chanPart, nil
}

// FetchLatestFile returns the latest release file for a CurseForge project.
// If gameVersion is non-empty, only files tagged for that game version are considered.
// The returned File's ID can be used as a stable version identifier.
func FetchLatestFile(ctx context.Context, projectID int, gameVersion, apiKey string, maxReleaseType int) (File, error) {
	endpoint := fmt.Sprintf("%s/v1/mods/%d/files", baseURL, projectID)
	if gameVersion != "" {
		endpoint += "?" + url.Values{"gameVersion": {gameVersion}}.Encode()
	}

	req, err := newRequest(ctx, endpoint, apiKey)
	if err != nil {
		return File{}, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return File{}, fmt.Errorf("fetching CurseForge files for project %d: %w", projectID, err)
	}
	defer resp.Body.Close()

	if err := checkStatus(resp.StatusCode, fmt.Sprintf("project %d files", projectID)); err != nil {
		return File{}, err
	}

	var result filesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return File{}, fmt.Errorf("parsing CurseForge files response: %w", err)
	}

	// Keep files within the requested channel: 1 <= releaseType <= maxReleaseType.
	var allowed []File
	for _, f := range result.Data {
		if f.ReleaseType >= 1 && f.ReleaseType <= maxReleaseType {
			allowed = append(allowed, f)
		}
	}
	if len(allowed) == 0 {
		return File{}, fmt.Errorf("no files found for CurseForge project %d within channel (gameVersion=%q)", projectID, gameVersion)
	}

	// Highest file ID = most recently uploaded
	sort.Slice(allowed, func(i, j int) bool {
		return allowed[i].ID > allowed[j].ID
	})
	return allowed[0], nil
}

// FetchFile returns a specific file from a CurseForge project.
func FetchFile(ctx context.Context, projectID, fileID int, apiKey string) (File, error) {
	endpoint := fmt.Sprintf("%s/v1/mods/%d/files/%d", baseURL, projectID, fileID)

	req, err := newRequest(ctx, endpoint, apiKey)
	if err != nil {
		return File{}, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return File{}, fmt.Errorf("fetching CurseForge file %d for project %d: %w", fileID, projectID, err)
	}
	defer resp.Body.Close()

	if err := checkStatus(resp.StatusCode, fmt.Sprintf("project %d file %d", projectID, fileID)); err != nil {
		return File{}, err
	}

	var result fileResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return File{}, fmt.Errorf("parsing CurseForge file response: %w", err)
	}
	return result.Data, nil
}

// ResolveDownloadURL returns the download URL for a file.
// If the File's DownloadURL is empty (some mods require an extra API call), it
// fetches the URL from the /download-url endpoint.
func ResolveDownloadURL(ctx context.Context, projectID int, file File, apiKey string) (string, error) {
	if file.DownloadURL != "" {
		return file.DownloadURL, nil
	}

	endpoint := fmt.Sprintf("%s/v1/mods/%d/files/%d/download-url", baseURL, projectID, file.ID)
	req, err := newRequest(ctx, endpoint, apiKey)
	if err != nil {
		return "", err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching CurseForge download URL for file %d: %w", file.ID, err)
	}
	defer resp.Body.Close()

	if err := checkStatus(resp.StatusCode, fmt.Sprintf("project %d file %d download-url", projectID, file.ID)); err != nil {
		return "", err
	}

	var result downloadURLResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parsing CurseForge download URL response: %w", err)
	}
	if result.Data == "" {
		return "", fmt.Errorf("CurseForge returned empty download URL for file %d", file.ID)
	}
	return result.Data, nil
}

// FileVersion returns a stable version string for a CurseForge file.
// The file ID is used since it is unique and monotonically increasing.
func FileVersion(file File) string {
	return strconv.Itoa(file.ID)
}

func newRequest(ctx context.Context, endpoint, apiKey string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func checkStatus(code int, target string) error {
	switch code {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("CurseForge API key rejected (HTTP %d) — set CURSEFORGE_API_KEY", code)
	case http.StatusNotFound:
		return fmt.Errorf("CurseForge resource not found: %s", target)
	default:
		return fmt.Errorf("CurseForge API returned HTTP %d for %s", code, target)
	}
}

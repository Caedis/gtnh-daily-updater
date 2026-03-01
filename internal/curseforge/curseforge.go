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

// File represents a CurseForge file entry.
type File struct {
	ID           int      `json:"id"`
	ModID        int      `json:"modId"`
	DisplayName  string   `json:"displayName"`
	FileName     string   `json:"fileName"`
	DownloadURL  string   `json:"downloadUrl"` // may be empty for some files
	ReleaseType  int      `json:"releaseType"` // 1=Release, 2=Beta, 3=Alpha
	GameVersions []string `json:"gameVersions"`
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

// ParseSource parses the part of a curseforge source after the "curseforge:" prefix.
// Accepted formats:
//   - "12345"       — project ID, use latest release file
//   - "12345/67890" — project ID + file ID, use that specific file
//
// Returns projectID, fileID (0 if not specified), and an error.
func ParseSource(s string) (projectID, fileID int, err error) {
	if projStr, fileStr, hasFile := strings.Cut(s, "/"); hasFile {
		projectID, err = strconv.Atoi(projStr)
		if err != nil || projectID <= 0 {
			return 0, 0, fmt.Errorf("invalid CurseForge project ID %q: must be a positive integer", projStr)
		}
		fileID, err = strconv.Atoi(fileStr)
		if err != nil || fileID <= 0 {
			return 0, 0, fmt.Errorf("invalid CurseForge file ID %q: must be a positive integer", fileStr)
		}
		return projectID, fileID, nil
	}
	projectID, err = strconv.Atoi(s)
	if err != nil || projectID <= 0 {
		return 0, 0, fmt.Errorf("invalid CurseForge project ID %q: must be a positive integer", s)
	}
	return projectID, 0, nil
}

// FetchLatestFile returns the latest release file for a CurseForge project.
// If gameVersion is non-empty, only files tagged for that game version are considered.
// The returned File's ID can be used as a stable version identifier.
func FetchLatestFile(ctx context.Context, projectID int, gameVersion, apiKey string) (File, error) {
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

	// Keep release-type files only (ReleaseType 1 = Release)
	var releases []File
	for _, f := range result.Data {
		if f.ReleaseType == 1 {
			releases = append(releases, f)
		}
	}
	if len(releases) == 0 {
		return File{}, fmt.Errorf("no release files found for CurseForge project %d (gameVersion=%q)", projectID, gameVersion)
	}

	// Highest file ID = most recently uploaded
	sort.Slice(releases, func(i, j int) bool {
		return releases[i].ID > releases[j].ID
	})
	return releases[0], nil
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

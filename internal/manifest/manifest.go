package manifest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	DailyManifestURL        = "https://raw.githubusercontent.com/GTNewHorizons/DreamAssemblerXXL/master/releases/manifests/daily.json"
	ExperimentalManifestURL = "https://raw.githubusercontent.com/GTNewHorizons/DreamAssemblerXXL/master/releases/manifests/experimental.json"

	ModeDaily        = "daily"
	ModeExperimental = "experimental"

	// Deprecated aliases kept for internal compatibility.
	TrackDaily        = ModeDaily
	TrackExperimental = ModeExperimental
)

type DailyManifest struct {
	Version      string             `json:"version"`
	LastVersion  string             `json:"last_version"`
	LastUpdated  string             `json:"last_updated"`
	Config       string             `json:"config"`
	GithubMods   map[string]ModInfo `json:"github_mods"`
	ExternalMods map[string]ModInfo `json:"external_mods"`
}

type ModInfo struct {
	Version string `json:"version"`
	Side    string `json:"side"`
}

// AllMods returns a merged map of github_mods and external_mods.
func (m *DailyManifest) AllMods() map[string]ModInfo {
	all := make(map[string]ModInfo, len(m.GithubMods)+len(m.ExternalMods))
	for k, v := range m.GithubMods {
		all[k] = v
	}
	for k, v := range m.ExternalMods {
		all[k] = v
	}
	return all
}

// ParseMode validates and normalizes a manifest mode.
// Empty values default to "daily".
func ParseMode(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", ModeDaily:
		return ModeDaily, nil
	case ModeExperimental:
		return ModeExperimental, nil
	default:
		return "", fmt.Errorf("mode must be %q or %q", ModeDaily, ModeExperimental)
	}
}

// ParseTrack is a deprecated wrapper around ParseMode.
func ParseTrack(track string) (string, error) {
	return ParseMode(track)
}

// URLForMode returns the manifest URL for a normalized mode.
func URLForMode(mode string) (string, error) {
	normalized, err := ParseMode(mode)
	if err != nil {
		return "", err
	}

	switch normalized {
	case ModeExperimental:
		return ExperimentalManifestURL, nil
	default:
		return DailyManifestURL, nil
	}
}

// URLForTrack is a deprecated wrapper around URLForMode.
func URLForTrack(track string) (string, error) {
	return URLForMode(track)
}

// Fetch downloads and parses the selected manifest from GitHub.
func Fetch(ctx context.Context, mode string) (*DailyManifest, error) {
	manifestURL, err := URLForMode(mode)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching manifest: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}

	var m DailyManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}

	return &m, nil
}

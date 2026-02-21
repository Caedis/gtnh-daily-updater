package manifest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const ManifestURL = "https://raw.githubusercontent.com/GTNewHorizons/DreamAssemblerXXL/master/releases/manifests/daily.json"

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

// Fetch downloads and parses the daily manifest from GitHub.
func Fetch(ctx context.Context) (*DailyManifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ManifestURL, nil)
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

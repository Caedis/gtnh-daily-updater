package updater

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caedis/gtnh-daily-updater/internal/config"
	"github.com/caedis/gtnh-daily-updater/internal/fileutil"
	"github.com/caedis/gtnh-daily-updater/internal/gitconfigs"
	"github.com/caedis/gtnh-daily-updater/internal/github"
	"github.com/caedis/gtnh-daily-updater/internal/logging"
)

func refreshDefaultGuide(ctx context.Context, opts Options, gameDir string, state *config.LocalState) error {
	if !strings.EqualFold(state.Side, "client") {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	tag, asset, err := latestGuideAsset(ctx, opts.GithubToken)
	if err != nil {
		err = fmt.Errorf("DefaultGuide.zip (%s): update failed: %w", tag, err)
		logging.Infof("  Warning: %v\n", err)
		return err
	}
	expectedHash := strings.TrimPrefix(asset.Digest, "sha256:")

	dest := filepath.Join(gameDir, filepath.FromSlash(gitconfigs.DefaultGuidePath))
	if !opts.Force && state.DefaultGuideRelease == tag &&
		(expectedHash == "" || strings.EqualFold(expectedHash, state.DefaultGuideSHA256)) {
		if file, err := os.Open(dest); err == nil {
			hash := sha256.New()
			_, err := io.Copy(hash, file)
			file.Close()
			if err == nil && fmt.Sprintf("%x", hash.Sum(nil)) == state.DefaultGuideSHA256 {
				return nil
			}
		}
	}

	logging.Infof("Updating DefaultGuide.zip from GTNH-Guide-Pack %s...\n", tag)
	data, err := fetchDefaultGuide(ctx, asset.BrowserDownloadURL)
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	if err == nil && expectedHash != "" && !strings.EqualFold(hash, expectedHash) {
		err = fmt.Errorf("guide ZIP SHA-256 mismatch")
	}
	if err == nil {
		err = os.MkdirAll(filepath.Dir(dest), 0755)
	}
	if err == nil {
		err = fileutil.WriteFileAtomic(dest, data, 0644)
	}
	if err != nil {
		err = fmt.Errorf("DefaultGuide.zip (%s): update failed: %w", tag, err)
		logging.Infof("  Warning: %v\n", err)
		return err
	}

	state.DefaultGuideRelease = tag
	state.DefaultGuideSHA256 = hash
	if err := state.Save(opts.InstanceDir); err != nil {
		err = fmt.Errorf("DefaultGuide.zip (%s): installed, but could not save state: %w", tag, err)
		logging.Infof("  Warning: %v\n", err)
		return err
	}
	logging.Infof("  DefaultGuide.zip (%s): updated\n", tag)
	return nil
}

func latestGuideAsset(ctx context.Context, token string) (string, github.ReleaseAsset, error) {
	const endpoint = "https://api.github.com/repos/GTNewHorizons/GTNH-Guide-Pack/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "latest", github.ReleaseAsset{}, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "latest", github.ReleaseAsset{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "latest", github.ReleaseAsset{}, fmt.Errorf("looking up latest GTNH-Guide-Pack release: HTTP %d", resp.StatusCode)
	}
	var release github.Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "latest", github.ReleaseAsset{}, fmt.Errorf("reading GTNH-Guide-Pack release: %w", err)
	}
	if release.TagName == "" {
		return "latest", github.ReleaseAsset{}, fmt.Errorf("GTNH-Guide-Pack release has no tag")
	}
	var guide github.ReleaseAsset
	for _, asset := range release.Assets {
		if !strings.HasPrefix(asset.Name, "GTNH-Guide-Pack-") || !strings.HasSuffix(asset.Name, ".zip") || asset.BrowserDownloadURL == "" {
			continue
		}
		if guide.BrowserDownloadURL != "" {
			return release.TagName, github.ReleaseAsset{}, fmt.Errorf("GTNH-Guide-Pack release has multiple guide ZIPs")
		}
		guide = asset
	}
	if guide.BrowserDownloadURL == "" {
		return release.TagName, github.ReleaseAsset{}, fmt.Errorf("GTNH-Guide-Pack release has no guide ZIP")
	}
	return release.TagName, guide, nil
}

func fetchDefaultGuide(ctx context.Context, assetURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading guide ZIP: HTTP %d", resp.StatusCode)
	}

	const maxGuideBytes = 256 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxGuideBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading guide ZIP: %w", err)
	}
	if len(data) > maxGuideBytes {
		return nil, fmt.Errorf("guide ZIP exceeds %d MiB", maxGuideBytes>>20)
	}
	guide, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("invalid guide ZIP: %w", err)
	}
	if len(guide.File) == 0 {
		return nil, fmt.Errorf("guide ZIP is empty")
	}
	return data, nil
}

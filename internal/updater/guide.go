package updater

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caedis/gtnh-daily-updater/internal/config"
	"github.com/caedis/gtnh-daily-updater/internal/fileutil"
	"github.com/caedis/gtnh-daily-updater/internal/gitconfigs"
	"github.com/caedis/gtnh-daily-updater/internal/logging"
)

func refreshDefaultGuide(ctx context.Context, opts Options, gameDir string, state *config.LocalState) error {
	tag := state.ConfigVersion
	if tag == "" || !strings.EqualFold(state.Side, "client") {
		return nil
	}

	dest := filepath.Join(gameDir, filepath.FromSlash(gitconfigs.DefaultGuidePath))
	if !opts.Force && state.DefaultGuideRelease == tag {
		if file, err := os.Open(dest); err == nil {
			hash := sha256.New()
			_, err := io.Copy(hash, file)
			file.Close()
			if err == nil && fmt.Sprintf("%x", hash.Sum(nil)) == state.DefaultGuideSHA256 {
				return nil
			}
		}
	}

	logging.Infof("Updating DefaultGuide.zip from config release %s...\n", tag)
	data, err := fetchDefaultGuide(ctx, tag)
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
	state.DefaultGuideSHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
	if err := state.Save(opts.InstanceDir); err != nil {
		err = fmt.Errorf("DefaultGuide.zip (%s): installed, but could not save state: %w", tag, err)
		logging.Infof("  Warning: %v\n", err)
		return err
	}
	logging.Infof("  DefaultGuide.zip (%s): updated\n", tag)
	return nil
}

func fetchDefaultGuide(ctx context.Context, tag string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	escapedTag := url.PathEscape(tag)
	assetURL := gitconfigs.RemoteURL + "/releases/download/" + escapedTag + "/" + escapedTag + ".zip"
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
		return nil, fmt.Errorf("downloading config release: HTTP %d", resp.StatusCode)
	}

	release, err := os.CreateTemp("", "gtnh-config-release-*.zip")
	if err != nil {
		return nil, err
	}
	defer os.Remove(release.Name())
	defer release.Close()
	size, err := io.Copy(release, resp.Body)
	if err != nil {
		return nil, fmt.Errorf("downloading config release: %w", err)
	}
	archive, err := zip.NewReader(release, size)
	if err != nil {
		return nil, fmt.Errorf("opening config release ZIP: %w", err)
	}
	entry, err := archive.Open(gitconfigs.DefaultGuidePath)
	if err != nil {
		return nil, fmt.Errorf("reading guide from config release: %w", err)
	}
	defer entry.Close()
	const maxGuideBytes = 256 << 20
	data, err := io.ReadAll(io.LimitReader(entry, maxGuideBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading guide from config release: %w", err)
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

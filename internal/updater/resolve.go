package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/caedis/gtnh-daily-updater/internal/assets"
	"github.com/caedis/gtnh-daily-updater/internal/config"
	"github.com/caedis/gtnh-daily-updater/internal/diff"
	"github.com/caedis/gtnh-daily-updater/internal/downloader"
	"github.com/caedis/gtnh-daily-updater/internal/github"
	"github.com/caedis/gtnh-daily-updater/internal/logging"
	"github.com/caedis/gtnh-daily-updater/internal/maven"
	"github.com/caedis/gtnh-daily-updater/internal/semver"
)

// resolveModDownload resolves the download URL and filename for a mod, trying
// extra downloads, --latest GitHub downloads, GitHub auth/public, and Maven in order.
func resolveModDownload(db *assets.AssetsDB, modName, version, githubToken string, extraDownloads, latestDownloads map[string]resolvedExtra) (dl downloader.Download, ok bool) {
	// Extra mod with pre-resolved download info
	if dlInfo, isExtra := extraDownloads[modName]; isExtra {
		return downloader.Download{
			URL:         dlInfo.URL,
			Filename:    dlInfo.Filename,
			ModName:     modName,
			IsGitHubAPI: dlInfo.IsGitHubAPI,
		}, true
	}

	// --latest resolved from GitHub directly
	if dlInfo, found := latestDownloads[modName]; found {
		return downloader.Download{
			URL:         dlInfo.URL,
			Filename:    dlInfo.Filename,
			ModName:     modName,
			IsGitHubAPI: dlInfo.IsGitHubAPI,
		}, true
	}

	// GitHub with auth
	if githubToken != "" {
		if apiURL, fn, err := db.ResolveDownloadWithAuth(modName, version); err == nil {
			return downloader.Download{
				URL:         apiURL,
				Filename:    fn,
				ModName:     modName,
				IsGitHubAPI: true,
			}, true
		}
	}

	// GitHub public download
	if url, filename, _, err := db.ResolveDownload(modName, version); err == nil {
		return downloader.Download{
			URL:      url,
			Filename: filename,
			ModName:  modName,
		}, true
	}

	// Maven fallback for GTNH-hosted mods
	if db.IsGTNH(modName) {
		if mavenURL, mavenFn := maven.DownloadURL(modName, version); mavenURL != "" && mavenFn != "" {
			return downloader.Download{
				URL:      mavenURL,
				Filename: mavenFn,
				ModName:  modName,
			}, true
		}
	}

	return downloader.Download{}, false
}

// resolveLatestVersions overrides manifest-pinned versions with the latest available.
// Pass 1 checks the assets DB; Pass 2 checks Maven metadata for GTNH mods to
// catch versions not yet in the DB; Pass 3 checks GitHub releases when a token
// is available.
func resolveLatestVersions(ctx context.Context, db *assets.AssetsDB, changes []diff.ModChange, extraDownloads map[string]resolvedExtra, latestDownloads map[string]resolvedExtra, opts Options) {
	// Pass 1: use assets DB to find latest versions
	for i, c := range changes {
		if c.Type == diff.Removed {
			continue
		}
		if _, isExtra := extraDownloads[c.Name]; isExtra {
			continue
		}
		latestVer, err := db.LatestNonPreVersion(c.Name)
		if err != nil {
			continue
		}
		if latestVer != c.NewVersion {
			logging.Debugf("Verbose: assets latest override %s %s -> %s\n", c.Name, c.NewVersion, latestVer)
			changes[i].NewVersion = latestVer
			if c.Type == diff.Unchanged {
				changes[i].Type = diff.Updated
			}
		}
	}

	// Pass 2: check Maven metadata for GTNH-hosted mods to catch versions
	// not yet in the assets DB.
	logging.Infoln("Checking Maven for latest versions...")

	type mavenResult struct {
		idx     int
		version string
	}

	var (
		mavenMu      sync.Mutex
		mavenResults []mavenResult
		mavenWG      sync.WaitGroup
		mavenSem     = make(chan struct{}, opts.Concurrency)
	)

	for i, c := range changes {
		if c.Type == diff.Removed {
			continue
		}
		if _, isExtra := extraDownloads[c.Name]; isExtra {
			continue
		}
		if !db.IsGTNH(c.Name) {
			continue
		}

		mavenWG.Add(1)
		go func(idx int, modName, currentVer string) {
			defer mavenWG.Done()
			mavenSem <- struct{}{}
			defer func() { <-mavenSem }()

			latestVer, err := maven.LatestNonPreVersion(ctx, modName)
			if err != nil {
				return
			}
			if semver.Compare(latestVer, currentVer) > 0 {
				mavenMu.Lock()
				mavenResults = append(mavenResults, mavenResult{idx: idx, version: latestVer})
				mavenMu.Unlock()
			}
		}(i, c.Name, c.NewVersion)
	}
	mavenWG.Wait()

	for _, r := range mavenResults {
		logging.Debugf("Verbose: maven latest override %s %s -> %s\n", changes[r.idx].Name, changes[r.idx].NewVersion, r.version)
		changes[r.idx].NewVersion = r.version
		if changes[r.idx].Type == diff.Unchanged {
			changes[r.idx].Type = diff.Updated
		}
	}

	// Pass 3: check GitHub releases for GTNH mods only. Only runs when a token is provided.
	if opts.GithubToken != "" {
		logging.Infoln("Checking GitHub for latest releases...")

		type ghResult struct {
			idx    int
			result *github.LatestResult
		}

		var (
			mu      sync.Mutex
			results []ghResult
			wg      sync.WaitGroup
			sem     = make(chan struct{}, opts.Concurrency)
		)

		for i, c := range changes {
			if c.Type == diff.Removed {
				continue
			}
			if _, isExtra := extraDownloads[c.Name]; isExtra {
				continue
			}
			// Only check GTNH-hosted mods
			if !db.IsGTNH(c.Name) {
				continue
			}
			repo := db.GitHubRepo(c.Name)
			if repo == "" {
				continue
			}

			wg.Add(1)
			go func(idx int, name, repo, currentVer string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				gh, err := github.FetchLatestRelease(ctx, repo, opts.GithubToken)
				if err != nil {
					return
				}
				// Only use the GitHub version if it's semantically newer
				if semver.Compare(gh.Version, currentVer) > 0 {
					mu.Lock()
					results = append(results, ghResult{idx: idx, result: gh})
					mu.Unlock()
				}
			}(i, c.Name, repo, c.NewVersion)
		}
		wg.Wait()

		for _, r := range results {
			logging.Debugf("Verbose: github latest override %s %s -> %s (asset=%s)\n", changes[r.idx].Name, changes[r.idx].NewVersion, r.result.Version, r.result.Filename)
			changes[r.idx].NewVersion = r.result.Version
			if changes[r.idx].Type == diff.Unchanged {
				changes[r.idx].Type = diff.Updated
			}
			latestDownloads[changes[r.idx].Name] = resolvedExtra{
				URL:         r.result.URL,
				Filename:    r.result.Filename,
				IsGitHubAPI: r.result.IsAPI,
			}
		}
	}

	// After all overrides, a downgrade/update can collapse back to the currently
	// installed version. Normalize these to unchanged so we don't print or apply
	// no-op updates like "x -> x".
	for i := range changes {
		if changes[i].Type == diff.Updated && changes[i].OldVersion == changes[i].NewVersion {
			changes[i].Type = diff.Unchanged
		}
	}
}

// resolveExtraMod resolves an extra mod spec into version/side info and download details.
func resolveExtraMod(ctx context.Context, name string, spec config.ExtraModSpec, db *assets.AssetsDB, githubToken string, latest bool) (diff.ResolvedExtraMod, resolvedExtra, error) {
	modSide := spec.Side
	if modSide == "" {
		modSide = "BOTH"
	}
	logging.Debugf("Verbose: resolveExtraMod name=%s source=%q requested-version=%q side=%s latest=%t\n", name, spec.Source, spec.Version, modSide, latest)

	switch {
	case spec.Source == "":
		// Assets DB source
		version := spec.Version
		if version == "" || latest {
			var v string
			var err error
			if latest {
				v, err = db.LatestNonPreVersion(name)
			} else {
				v, err = db.LatestVersion(name)
			}
			if err != nil {
				return diff.ResolvedExtraMod{}, resolvedExtra{}, err
			}
			version = v
		}

		// Try Maven first for GTNH-hosted mods
		if db.IsGTNH(name) {
			mavenURL, mavenFn := maven.DownloadURL(name, version)
			logging.Debugf("Verbose: extra mod %s using maven download filename=%s\n", name, mavenFn)
			return diff.ResolvedExtraMod{Version: version, Side: modSide}, resolvedExtra{
				URL:      mavenURL,
				Filename: mavenFn,
			}, nil
		}

		url, filename, isAPI, err := db.ResolveDownload(name, version)
		if err != nil {
			return diff.ResolvedExtraMod{}, resolvedExtra{}, err
		}

		dlInfo := resolvedExtra{URL: url, Filename: filename, IsGitHubAPI: isAPI}

		// If API URL and we have a token, prefer authenticated download
		if isAPI && githubToken != "" {
			apiURL, fn, err := db.ResolveDownloadWithAuth(name, version)
			if err == nil {
				dlInfo.URL = apiURL
				dlInfo.Filename = fn
				dlInfo.IsGitHubAPI = true
			}
		}

		return diff.ResolvedExtraMod{Version: version, Side: modSide}, dlInfo, nil

	case strings.HasPrefix(spec.Source, "github:"):
		repo := strings.TrimPrefix(spec.Source, "github:")
		version := spec.Version
		logging.Debugf("Verbose: extra mod %s using GitHub source repo=%s requested-version=%q\n", name, repo, version)

		// Fetch releases
		var apiURL string
		if version == "" {
			apiURL = fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
		} else {
			apiURL = fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", repo, version)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
		if err != nil {
			return diff.ResolvedExtraMod{}, resolvedExtra{}, fmt.Errorf("creating request: %w", err)
		}
		if githubToken != "" {
			req.Header.Set("Authorization", "token "+githubToken)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return diff.ResolvedExtraMod{}, resolvedExtra{}, fmt.Errorf("fetching GitHub release: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return diff.ResolvedExtraMod{}, resolvedExtra{}, fmt.Errorf("GitHub API returned HTTP %d for %s", resp.StatusCode, apiURL)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return diff.ResolvedExtraMod{}, resolvedExtra{}, fmt.Errorf("reading GitHub response: %w", err)
		}

		var release github.Release
		if err := json.Unmarshal(body, &release); err != nil {
			return diff.ResolvedExtraMod{}, resolvedExtra{}, fmt.Errorf("parsing GitHub release: %w", err)
		}

		version = release.TagName

		asset := github.PickPrimaryJar(release.Assets, version)
		if asset == nil {
			return diff.ResolvedExtraMod{}, resolvedExtra{}, fmt.Errorf("no primary .jar asset found in release %s of %s", version, repo)
		}
		downloadURL := asset.BrowserDownloadURL
		isGitHubAPI := false
		if githubToken != "" && strings.TrimSpace(asset.URL) != "" {
			downloadURL = strings.TrimSpace(asset.URL)
			isGitHubAPI = true
		}
		if strings.TrimSpace(downloadURL) == "" {
			return diff.ResolvedExtraMod{}, resolvedExtra{}, fmt.Errorf("release asset %s has no download URL", asset.Name)
		}
		logging.Debugf("Verbose: extra mod %s GitHub release=%s asset=%s\n", name, version, asset.Name)
		return diff.ResolvedExtraMod{Version: version, Side: modSide}, resolvedExtra{
			URL:         downloadURL,
			Filename:    asset.Name,
			IsGitHubAPI: isGitHubAPI,
		}, nil

	default:
		// Direct URL source
		url := spec.Source
		filename := path.Base(url)
		if filename == "" || filename == "." || filename == "/" {
			filename = name + ".jar"
		}
		version := spec.Version
		if version == "" {
			version = url // use URL as version identifier
		}
		logging.Debugf("Verbose: extra mod %s using direct URL filename=%s\n", name, filename)

		return diff.ResolvedExtraMod{Version: version, Side: modSide}, resolvedExtra{
			URL:      url,
			Filename: filename,
		}, nil
	}
}

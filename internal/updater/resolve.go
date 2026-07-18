package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/caedis/gtnh-daily-updater/internal/assets"
	"github.com/caedis/gtnh-daily-updater/internal/config"
	"github.com/caedis/gtnh-daily-updater/internal/curseforge"
	"github.com/caedis/gtnh-daily-updater/internal/diff"
	"github.com/caedis/gtnh-daily-updater/internal/downloader"
	"github.com/caedis/gtnh-daily-updater/internal/github"
	"github.com/caedis/gtnh-daily-updater/internal/logging"
	"github.com/caedis/gtnh-daily-updater/internal/maven"
	"github.com/caedis/gtnh-daily-updater/internal/modrinth"
	"github.com/caedis/gtnh-daily-updater/internal/semver"
)

// resolveModDownload resolves the download URL and filename for a mod, trying
// extra downloads, --latest GitHub downloads, GitHub auth/public, and Maven in order.
func resolveModDownload(ctx context.Context, db *assets.AssetsDB, modName, version, githubToken string, extraDownloads, latestDownloads map[string]resolvedExtra) (dl downloader.Download, ok bool) {
	// Extra mod with pre-resolved download info
	if dlInfo, isExtra := extraDownloads[modName]; isExtra {
		dl := downloader.Download{
			URL:          dlInfo.URL,
			Filename:     dlInfo.Filename,
			ModName:      modName,
			IsGitHubAPI:  dlInfo.IsGitHubAPI,
			ExpectedHash: dlInfo.ExpectedHash,
			HashAlgo:     dlInfo.HashAlgo,
		}
		return withMavenFallback(ctx, dl, db, modName, version), true
	}

	// --latest resolved from GitHub directly
	if dlInfo, found := latestDownloads[modName]; found {
		dl := downloader.Download{
			URL:          dlInfo.URL,
			Filename:     dlInfo.Filename,
			ModName:      modName,
			IsGitHubAPI:  dlInfo.IsGitHubAPI,
			ExpectedHash: dlInfo.ExpectedHash,
			HashAlgo:     dlInfo.HashAlgo,
		}
		return withMavenFallback(ctx, dl, db, modName, version), true
	}

	// GitHub with auth
	if githubToken != "" {
		if apiURL, fn, err := db.ResolveDownloadWithAuth(modName, version); err == nil {
			dl := downloader.Download{
				URL:         apiURL,
				Filename:    fn,
				ModName:     modName,
				IsGitHubAPI: true,
			}
			return withMavenFallback(ctx, dl, db, modName, version), true
		}
	}

	// GitHub public download
	if url, filename, _, err := db.ResolveDownload(modName, version); err == nil {
		dl := downloader.Download{
			URL:      url,
			Filename: filename,
			ModName:  modName,
		}
		return withMavenFallback(ctx, dl, db, modName, version), true
	}

	// Maven fallback for GTNH-hosted mods
	if db.IsGTNH(modName) {
		if mavenURL, mavenFn, err := maven.DownloadURL(ctx, modName, version); err == nil && mavenURL != "" && mavenFn != "" {
			d := downloader.Download{URL: mavenURL, Filename: mavenFn, ModName: modName}
			if sha, _ := maven.FetchSHA256(ctx, mavenURL); sha != "" {
				d.ExpectedHash = sha
				d.HashAlgo = "sha256"
			}
			return d, true
		}
	}

	return downloader.Download{}, false
}

func withMavenFallback(ctx context.Context, dl downloader.Download, db *assets.AssetsDB, modName, version string) downloader.Download {
	if !db.IsGTNH(modName) || !isGitHubDownload(dl.URL, dl.IsGitHubAPI) {
		return dl
	}
	mavenURL, _, err := maven.DownloadURL(ctx, modName, version)
	if err != nil || strings.TrimSpace(mavenURL) == "" || mavenURL == dl.URL {
		return dl
	}
	dl.MavenFallbackURL = mavenURL
	if sha, _ := maven.FetchSHA256(ctx, mavenURL); sha != "" {
		dl.MavenFallbackHash = sha
	}
	return dl
}

func isGitHubDownload(rawURL string, isGitHubAPI bool) bool {
	if isGitHubAPI {
		return true
	}
	url := strings.ToLower(strings.TrimSpace(rawURL))
	return strings.HasPrefix(url, "https://api.github.com/") || strings.HasPrefix(url, "https://github.com/")
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
		var latestVer string
		var err error
		if opts.AllowPreRelease {
			latestVer, err = db.LatestAnyVersion(c.Name)
		} else {
			latestVer, err = db.LatestNonPreVersion(c.Name)
		}
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

	// Pass 2: for each GTNH-hosted mod resolve the latest release preferring
	// GitHub (token if available, anonymous fallback) and dropping to Nexus/Maven
	// only when GitHub is unreachable. A reachable GitHub response is authoritative.
	logging.Infoln("Checking GitHub and Maven for latest versions...")

	type latestResult struct {
		idx     int
		version string
		extra   *resolvedExtra // non-nil when resolved from GitHub
	}

	var (
		mu      sync.Mutex
		results []latestResult
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
		if !db.IsGTNH(c.Name) {
			continue
		}

		wg.Add(1)
		go func(idx int, name, currentVer string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if repo := db.GitHubRepo(name); repo != "" {
				if gh := fetchLatestReleasePreferToken(ctx, repo, opts.GithubToken, opts.AllowPreRelease); gh != nil {
					// GitHub reachable: authoritative. Override only when newer.
					if semver.Compare(gh.Version, currentVer) > 0 {
						extra := resolvedExtra{
							URL:         gh.URL,
							Filename:    gh.Filename,
							IsGitHubAPI: gh.IsAPI,
						}
						if d := strings.TrimPrefix(gh.Digest, "sha256:"); d != "" && d != gh.Digest {
							extra.ExpectedHash = d
							extra.HashAlgo = "sha256"
						}
						mu.Lock()
						results = append(results, latestResult{idx: idx, version: gh.Version, extra: &extra})
						mu.Unlock()
					}
					return // do not consult Maven
				}
			}

			// GitHub unreachable (or no known repo): fall back to Nexus/Maven.
			var mavenVer string
			var err error
			if opts.AllowPreRelease {
				mavenVer, err = maven.LatestAnyVersion(ctx, name)
			} else {
				mavenVer, err = maven.LatestNonPreVersion(ctx, name)
			}
			if err != nil {
				return
			}
			if semver.Compare(mavenVer, currentVer) > 0 {
				mu.Lock()
				results = append(results, latestResult{idx: idx, version: mavenVer})
				mu.Unlock()
			}
		}(i, c.Name, c.NewVersion)
	}
	wg.Wait()

	for _, r := range results {
		logging.Debugf("Verbose: latest override %s %s -> %s (github=%t)\n", changes[r.idx].Name, changes[r.idx].NewVersion, r.version, r.extra != nil)
		changes[r.idx].NewVersion = r.version
		if changes[r.idx].Type == diff.Unchanged {
			changes[r.idx].Type = diff.Updated
		}
		if r.extra != nil {
			latestDownloads[changes[r.idx].Name] = *r.extra
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

// fetchLatestReleasePreferToken resolves a repo's latest GitHub release,
// preferring an authenticated request when a token is set and retrying once
// anonymously if the authenticated attempt errors. Returns nil only when GitHub
// is unreachable after both attempts.
func fetchLatestReleasePreferToken(ctx context.Context, repo, token string, allowPre bool) *github.LatestResult {
	gh, err := github.FetchLatestRelease(ctx, repo, token, allowPre)
	if err != nil && token != "" {
		gh, err = github.FetchLatestRelease(ctx, repo, "", allowPre)
	}
	if err != nil {
		return nil
	}
	return gh
}

// resolveExtraMod resolves an extra mod spec into version/side info and download details.
func resolveExtraMod(ctx context.Context, name string, spec config.ExtraModSpec, db *assets.AssetsDB, githubToken, curseforgeKey string, latest bool) (diff.ResolvedExtraMod, resolvedExtra, error) {
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
			mavenURL, mavenFn, err := maven.DownloadURL(ctx, name, version)
			if err != nil {
				return diff.ResolvedExtraMod{}, resolvedExtra{}, err
			}
			logging.Debugf("Verbose: extra mod %s using maven download filename=%s\n", name, mavenFn)
			extra := resolvedExtra{URL: mavenURL, Filename: mavenFn}
			if sha, _ := maven.FetchSHA256(ctx, mavenURL); sha != "" {
				extra.ExpectedHash = sha
				extra.HashAlgo = "sha256"
			}
			return diff.ResolvedExtraMod{Version: version, Side: modSide}, extra, nil
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

	case strings.HasPrefix(spec.Source, "curseforge:"):
		if curseforgeKey == "" {
			return diff.ResolvedExtraMod{}, resolvedExtra{}, fmt.Errorf("CURSEFORGE_API_KEY is required for CurseForge mods (set via env or --curseforge-key)")
		}
		rest := strings.TrimPrefix(spec.Source, "curseforge:")
		projectID, fileID, err := curseforge.ParseSource(rest)
		if err != nil {
			return diff.ResolvedExtraMod{}, resolvedExtra{}, err
		}

		var file curseforge.File
		if fileID != 0 {
			file, err = curseforge.FetchFile(ctx, projectID, fileID, curseforgeKey)
		} else {
			file, err = curseforge.FetchLatestFile(ctx, projectID, curseforge.GTNHGameVersion, curseforgeKey)
		}
		if err != nil {
			return diff.ResolvedExtraMod{}, resolvedExtra{}, err
		}

		downloadURL, err := curseforge.ResolveDownloadURL(ctx, projectID, file, curseforgeKey)
		if err != nil {
			return diff.ResolvedExtraMod{}, resolvedExtra{}, err
		}

		version := strconv.Itoa(file.ID)
		logging.Debugf("Verbose: extra mod %s CurseForge project=%d file=%d filename=%s\n", name, projectID, file.ID, file.FileName)
		extra := resolvedExtra{URL: downloadURL, Filename: file.FileName}
		if sha := file.SHA1(); sha != "" {
			extra.ExpectedHash = sha
			extra.HashAlgo = "sha1"
		}
		return diff.ResolvedExtraMod{Version: version, Side: modSide}, extra, nil

	case strings.HasPrefix(spec.Source, "modrinth:"):
		rest := strings.TrimPrefix(spec.Source, "modrinth:")
		project, versionID, err := modrinth.ParseSource(rest)
		if err != nil {
			return diff.ResolvedExtraMod{}, resolvedExtra{}, err
		}

		var ver modrinth.Version
		if versionID != "" {
			ver, err = modrinth.FetchVersion(ctx, versionID)
		} else {
			ver, err = modrinth.FetchLatestVersion(ctx, project, modrinth.GTNHGameVersion, modrinth.GTNHLoader)
		}
		if err != nil {
			return diff.ResolvedExtraMod{}, resolvedExtra{}, err
		}

		file, err := modrinth.PrimaryFile(ver)
		if err != nil {
			return diff.ResolvedExtraMod{}, resolvedExtra{}, err
		}

		logging.Debugf("Verbose: extra mod %s Modrinth project=%s version=%s filename=%s\n", name, project, ver.ID, file.Filename)
		extra := resolvedExtra{URL: file.URL, Filename: file.Filename}
		if file.Hashes.SHA512 != "" {
			extra.ExpectedHash = file.Hashes.SHA512
			extra.HashAlgo = "sha512"
		}
		return diff.ResolvedExtraMod{Version: ver.ID, Side: modSide}, extra, nil

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

		var asset *github.ReleaseAsset
		if strings.TrimSpace(spec.Match) != "" {
			re, err := regexp.Compile(spec.Match)
			if err != nil {
				return diff.ResolvedExtraMod{}, resolvedExtra{}, fmt.Errorf("invalid match pattern %q for extra %s: %w", spec.Match, name, err)
			}
			asset = github.PickJarMatching(release.Assets, re)
			if asset == nil {
				candidates := github.JarAssetNames(release.Assets)
				return diff.ResolvedExtraMod{}, resolvedExtra{}, fmt.Errorf("match pattern %q for extra %s did not uniquely identify a jar in release %s of %s; candidates: %s", spec.Match, name, version, repo, strings.Join(candidates, ", "))
			}
		} else {
			asset = github.PickPrimaryJar(release.Assets, version)
			if asset == nil {
				return diff.ResolvedExtraMod{}, resolvedExtra{}, fmt.Errorf("no primary .jar asset found in release %s of %s", version, repo)
			}
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
		extra := resolvedExtra{URL: downloadURL, Filename: asset.Name, IsGitHubAPI: isGitHubAPI}
		if d := strings.TrimPrefix(asset.Digest, "sha256:"); d != "" && d != asset.Digest {
			extra.ExpectedHash = d
			extra.HashAlgo = "sha256"
		}
		return diff.ResolvedExtraMod{Version: version, Side: modSide}, extra, nil

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

package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caedis/gtnh-daily-updater/internal/fileutil"
	"github.com/caedis/gtnh-daily-updater/internal/logging"
)

type Download struct {
	URL      string
	Filename string
	// ModName is used to organize cache into per-mod subdirectories.
	ModName string
	// IsGitHubAPI indicates the URL is a GitHub API URL that needs special headers
	IsGitHubAPI bool
	// MavenFallbackURL is used when a GitHub download fails after retries.
	MavenFallbackURL string
}

type Result struct {
	Download Download
	Err      error
}

type Progress struct {
	Completed int64
	Total     int64
}

const maxRetries = 3

// Run downloads files concurrently to destDir with the given concurrency.
// It calls onProgress after each completed download.
// githubToken is used for GitHub API URLs if non-empty.
// If cacheDir is non-empty, files are cached there and copied to destDir on subsequent runs.
func Run(ctx context.Context, downloads []Download, destDir string, concurrency int, githubToken, cacheDir string, onProgress func(Progress)) []Result {
	if concurrency < 1 {
		concurrency = 6
	}

	total := int64(len(downloads))
	var completed atomic.Int64

	results := make([]Result, len(downloads))
	work := make(chan int, len(downloads))

	for i := range downloads {
		work <- i
	}
	close(work)

	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				dl := downloads[i]
				err := downloadFileWithRetry(ctx, dl, destDir, githubToken, cacheDir)
				results[i] = Result{Download: dl, Err: err}

				n := completed.Add(1)
				if onProgress != nil {
					onProgress(Progress{Completed: n, Total: total})
				}
			}
		}()
	}

	wg.Wait()
	return results
}

func downloadFileWithRetry(ctx context.Context, dl Download, destDir, githubToken, cacheDir string) error {
	lastErr := downloadFileWithRetryURL(ctx, dl, destDir, githubToken, cacheDir)
	if lastErr == nil {
		return nil
	}
	if dl.MavenFallbackURL == "" {
		return lastErr
	}

	fallback := dl
	fallback.URL = dl.MavenFallbackURL
	fallback.IsGitHubAPI = false
	fallback.MavenFallbackURL = ""
	logging.Debugf(
		"Verbose: github download failed for %s (%v); trying maven fallback url=%s\n",
		dl.Filename,
		lastErr,
		fallback.URL,
	)
	fallbackErr := downloadFileWithRetryURL(ctx, fallback, destDir, githubToken, cacheDir)
	if fallbackErr == nil {
		return nil
	}

	return fmt.Errorf("downloading %s: github failed: %w; maven fallback failed: %v", dl.Filename, lastErr, fallbackErr)
}

func retryWithBackoff(ctx context.Context, n int, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt < n; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
	}
	return lastErr
}

func downloadFileWithRetryURL(ctx context.Context, dl Download, destDir, githubToken, cacheDir string) error {
	attempt := 0
	return retryWithBackoff(ctx, maxRetries, func() error {
		if attempt > 0 {
			logging.Debugf("Verbose: retrying download %s attempt=%d/%d\n", dl.Filename, attempt+1, maxRetries)
		}
		attempt++
		return downloadFile(ctx, dl, destDir, githubToken, cacheDir)
	})
}

func downloadFile(ctx context.Context, dl Download, destDir, githubToken, cacheDir string) error {
	safeFilename := fileutil.SanitizeFilename(dl.Filename)
	safeModName := fileutil.SanitizeFilename(dl.ModName)
	destPath := filepath.Join(destDir, safeFilename)
	logging.Debugf("Verbose: download start mod=%s filename=%s url=%s\n", dl.ModName, dl.Filename, dl.URL)

	// Check cache first
	if cacheDir != "" {
		modCacheDir := filepath.Join(cacheDir, safeModName)
		cachePath := filepath.Join(modCacheDir, safeFilename)
		if _, err := os.Stat(cachePath); err == nil {
			logging.Debugf("Verbose: cache hit mod=%s file=%s\n", dl.ModName, dl.Filename)
			return copyFile(cachePath, destPath)
		}
		// Fall back to old unsanitized cache path for backwards compatibility
		oldCachePath := filepath.Join(cacheDir, dl.ModName, dl.Filename)
		if oldCachePath != cachePath {
			if _, err := os.Stat(oldCachePath); err == nil {
				logging.Debugf("Verbose: cache hit (legacy path) mod=%s file=%s\n", dl.ModName, dl.Filename)
				return copyFile(oldCachePath, destPath)
			}
		}
		logging.Debugf("Verbose: cache miss mod=%s file=%s\n", dl.ModName, dl.Filename)
	}

	// Download the file
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dl.URL, nil)
	if err != nil {
		return fmt.Errorf("creating request for %s: %w", dl.Filename, err)
	}

	if dl.IsGitHubAPI && githubToken != "" {
		req.Header.Set("Accept", "application/octet-stream")
		req.Header.Set("Authorization", "token "+githubToken)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", dl.Filename, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading %s: HTTP %d", dl.Filename, resp.StatusCode)
	}

	// If caching, download to cache first, then copy to dest
	if cacheDir != "" {
		modCacheDir := filepath.Join(cacheDir, safeModName)
		if err := os.MkdirAll(modCacheDir, 0755); err != nil {
			return fmt.Errorf("creating cache dir for %s: %w", dl.ModName, err)
		}
		cachePath := filepath.Join(modCacheDir, safeFilename)
		cacheTmp := cachePath + ".tmp"

		f, err := os.Create(cacheTmp)
		if err != nil {
			return fmt.Errorf("creating cache file for %s: %w", dl.Filename, err)
		}

		_, err = io.Copy(f, resp.Body)
		closeErr := f.Close()
		if err != nil {
			os.Remove(cacheTmp)
			return fmt.Errorf("writing %s: %w", dl.Filename, err)
		}
		if closeErr != nil {
			os.Remove(cacheTmp)
			return fmt.Errorf("closing %s: %w", dl.Filename, closeErr)
		}

		if err := os.Rename(cacheTmp, cachePath); err != nil {
			os.Remove(cacheTmp)
			return fmt.Errorf("finalizing cache for %s: %w", dl.Filename, err)
		}

		evictOldCacheFiles(modCacheDir, 5)
		return copyFile(cachePath, destPath)
	}

	// No cache — download directly to destDir
	tmpPath := destPath + ".tmp"

	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("creating %s: %w", dl.Filename, err)
	}

	_, err = io.Copy(f, resp.Body)
	closeErr := f.Close()
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("writing %s: %w", dl.Filename, err)
	}
	if closeErr != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing %s: %w", dl.Filename, closeErr)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("finalizing %s: %w", dl.Filename, err)
	}
	logging.Debugf("Verbose: download complete file=%s\n", dl.Filename)

	return nil
}

// copyFile copies src to dst using an atomic write (write to dst.tmp, then rename).
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening %s: %w", src, err)
	}
	defer in.Close()

	tmpPath := dst + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("creating %s: %w", tmpPath, err)
	}

	_, err = io.Copy(out, in)
	closeErr := out.Close()
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("writing %s: %w", dst, err)
	}
	if closeErr != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing %s: %w", dst, closeErr)
	}

	if err := os.Rename(tmpPath, dst); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("finalizing %s: %w", dst, err)
	}

	return nil
}

// DownloadToFile downloads a single file from the given URL to destPath with retries.
func DownloadToFile(ctx context.Context, url, destPath, githubToken string, isGitHubAPI bool) error {
	return retryWithBackoff(ctx, maxRetries, func() error {
		return downloadToFileOnce(ctx, url, destPath, githubToken, isGitHubAPI)
	})
}

func downloadToFileOnce(ctx context.Context, url, destPath, githubToken string, isGitHubAPI bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	if isGitHubAPI && githubToken != "" {
		req.Header.Set("Accept", "application/octet-stream")
		req.Header.Set("Authorization", "token "+githubToken)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("downloading: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading: HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating file: %w", err)
	}

	_, err = io.Copy(f, resp.Body)
	closeErr := f.Close()
	if err != nil {
		return fmt.Errorf("writing file: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("closing file: %w", closeErr)
	}

	return nil
}

// evictOldCacheFiles removes the oldest files in dir, keeping only the newest
// keep entries. Errors are logged but not returned since eviction is best-effort.
func evictOldCacheFiles(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	type fileEntry struct {
		name    string
		modTime time.Time
	}

	var files []fileEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileEntry{name: e.Name(), modTime: info.ModTime()})
	}

	if len(files) <= keep {
		return
	}

	// Sort newest first
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	for _, f := range files[keep:] {
		path := filepath.Join(dir, f.name)
		if err := os.Remove(path); err != nil {
			logging.Debugf("Verbose: failed to evict cached file %s: %v\n", path, err)
		} else {
			logging.Debugf("Verbose: evicted old cached file %s\n", path)
		}
	}
}

package downloader

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caedis/gtnh-daily-updater/internal/fileutil"
	"github.com/caedis/gtnh-daily-updater/internal/logging"
)

// ErrHashMismatch indicates a downloaded or cached file's hash did not match
// the expected hash after all retries.
var ErrHashMismatch = errors.New("hash mismatch")

type Download struct {
	URL      string
	Filename string
	// ModName is used to organize cache into per-mod subdirectories.
	ModName string
	// IsGitHubAPI indicates the URL is a GitHub API URL that needs special headers
	IsGitHubAPI bool
	// MavenFallbackURL is used when a GitHub download fails after retries.
	MavenFallbackURL string
	// ExpectedHash is the lowercase hex digest the downloaded bytes must match.
	// Empty disables validation.
	ExpectedHash string
	// HashAlgo selects the algorithm: "sha256", "sha1", "sha512". Empty disables.
	HashAlgo string
	// MavenFallbackHash is the expected sha256 for MavenFallbackURL. Empty disables on fallback.
	MavenFallbackHash string
	// Disabled writes the on-disk file with a `.disabled` suffix (Prism/MultiMC
	// disabled-mod convention). The cache entry still uses the clean filename so
	// it stays shareable with enabled installs.
	Disabled bool
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
	if dl.MavenFallbackHash != "" {
		fallback.ExpectedHash = dl.MavenFallbackHash
		fallback.HashAlgo = "sha256"
	} else {
		fallback.ExpectedHash = ""
		fallback.HashAlgo = ""
	}
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
	destName := safeFilename
	if dl.Disabled {
		destName += ".disabled"
	}
	destPath := filepath.Join(destDir, destName)
	logging.Debugf("Verbose: download start mod=%s filename=%s url=%s\n", dl.ModName, dl.Filename, dl.URL)

	// Check cache first
	if cacheDir != "" {
		modCacheDir := filepath.Join(cacheDir, safeModName)
		cachePath := filepath.Join(modCacheDir, safeFilename)
		if _, err := os.Stat(cachePath); err == nil {
			if vErr := validateCachedFile(cachePath, dl.HashAlgo, dl.ExpectedHash); vErr != nil {
				if errors.Is(vErr, ErrHashMismatch) {
					logging.Debugf("Verbose: cache invalid mod=%s file=%s err=%v\n", dl.ModName, dl.Filename, vErr)
					os.Remove(cachePath)
					os.Remove(cachePath + ".sha256")
				} else {
					return vErr
				}
			} else {
				logging.Debugf("Verbose: cache hit mod=%s file=%s\n", dl.ModName, dl.Filename)
				return copyFile(cachePath, destPath)
			}
		}
		// Fall back to old unsanitized cache path for backwards compatibility
		oldCachePath := filepath.Join(cacheDir, dl.ModName, dl.Filename)
		if oldCachePath != cachePath {
			if _, err := os.Stat(oldCachePath); err == nil {
				if vErr := validateCachedFile(oldCachePath, dl.HashAlgo, dl.ExpectedHash); vErr != nil {
					if errors.Is(vErr, ErrHashMismatch) {
						logging.Debugf("Verbose: legacy cache invalid mod=%s file=%s err=%v\n", dl.ModName, dl.Filename, vErr)
						os.Remove(oldCachePath)
						os.Remove(oldCachePath + ".sha256")
					} else {
						return vErr
					}
				} else {
					logging.Debugf("Verbose: cache hit (legacy path) mod=%s file=%s\n", dl.ModName, dl.Filename)
					return copyFile(oldCachePath, destPath)
				}
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

	if cacheDir != "" {
		modCacheDir := filepath.Join(cacheDir, safeModName)
		if err := os.MkdirAll(modCacheDir, 0755); err != nil {
			return fmt.Errorf("creating cache dir for %s: %w", dl.ModName, err)
		}
		cachePath := filepath.Join(modCacheDir, safeFilename)
		if err := writeCacheAndHash(resp.Body, cachePath, dl.HashAlgo, dl.ExpectedHash, dl.Filename); err != nil {
			return err
		}
		evictOldCacheFiles(modCacheDir, 5)
		return copyFile(cachePath, destPath)
	}

	if err := writeAndHash(resp.Body, destPath, dl.HashAlgo, dl.ExpectedHash, dl.Filename); err != nil {
		return err
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

type hasher struct {
	h hash.Hash
}

func newHasher(algo string) (*hasher, error) {
	switch algo {
	case "sha256":
		return &hasher{h: sha256.New()}, nil
	case "sha1":
		return &hasher{h: sha1.New()}, nil
	case "sha512":
		return &hasher{h: sha512.New()}, nil
	default:
		return nil, fmt.Errorf("unsupported hash algo %q", algo)
	}
}

func (h *hasher) Write(p []byte) (int, error) { return h.h.Write(p) }
func (h *hasher) Hex() string                 { return hex.EncodeToString(h.h.Sum(nil)) }

// readCacheSidecar reads the sha256 sidecar file for jarPath (jarPath+".sha256").
// Returns ("", os.ErrNotExist) if missing, or the trimmed lowercase hex on success.
func readCacheSidecar(jarPath string) (string, error) {
	data, err := os.ReadFile(jarPath + ".sha256")
	if err != nil {
		return "", err
	}
	return strings.ToLower(strings.TrimSpace(string(data))), nil
}

// fileSHA256 streams path through a sha256 hasher and returns the lowercase hex digest.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// validateCachedFile checks a cached jar for integrity. It prefers the sha256
// sidecar (jarPath+".sha256") if present; falls back to upstreamAlgo/upstreamExpected
// if no sidecar; skips validation if neither is available.
// Returns ErrHashMismatch (wrapped) on mismatch, nil on pass or skip.
func validateCachedFile(path, upstreamAlgo, upstreamExpected string) error {
	if sha, err := readCacheSidecar(path); err == nil && sha != "" {
		got, err := fileSHA256(path)
		if err != nil {
			return err
		}
		if got != sha {
			return fmt.Errorf("cache %s: %w (sidecar got=%s want=%s)", filepath.Base(path), ErrHashMismatch, got, sha)
		}
		return nil
	}
	// No sidecar - fall back to upstream hash if known.
	if upstreamAlgo == "" || upstreamExpected == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h, err := newHasher(upstreamAlgo)
	if err != nil {
		return err
	}
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := h.Hex()
	want := strings.ToLower(upstreamExpected)
	if got != want {
		return fmt.Errorf("cache %s: %w (algo=%s got=%s want=%s)", filepath.Base(path), ErrHashMismatch, upstreamAlgo, got, want)
	}
	return nil
}

// writeCacheAndHash writes src to cachePath via a .tmp file, validating the
// upstream hash (if algo+expected non-empty) and always computing a sha256
// sidecar file (cachePath+".sha256") for future cache validation.
// On upstream hash mismatch the tmp is removed and ErrHashMismatch is returned.
// If writing the sidecar fails it is logged but does not fail the download.
func writeCacheAndHash(src io.Reader, cachePath, upstreamAlgo, upstreamExpected, label string) error {
	tmpPath := cachePath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("creating %s: %w", label, err)
	}

	sha256h := sha256.New()
	var writers []io.Writer
	writers = append(writers, f, sha256h)

	var upstreamHasher *hasher
	// If upstream algo is sha256 we reuse sha256h to avoid double-hashing.
	if upstreamAlgo != "" && upstreamExpected != "" {
		if upstreamAlgo == "sha256" {
			// sha256h already covers this; set upstreamHasher to nil sentinel handled below.
		} else {
			upstreamHasher, err = newHasher(upstreamAlgo)
			if err != nil {
				f.Close()
				os.Remove(tmpPath)
				return err
			}
			writers = append(writers, upstreamHasher)
		}
	}

	w := io.MultiWriter(writers...)
	_, copyErr := io.Copy(w, src)
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("writing %s: %w", label, copyErr)
	}
	if closeErr != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing %s: %w", label, closeErr)
	}

	// Validate upstream hash if requested.
	if upstreamAlgo != "" && upstreamExpected != "" {
		want := strings.ToLower(upstreamExpected)
		var got string
		if upstreamAlgo == "sha256" {
			got = hex.EncodeToString(sha256h.Sum(nil))
		} else {
			got = upstreamHasher.Hex()
		}
		if got != want {
			os.Remove(tmpPath)
			return fmt.Errorf("%s: %w (algo=%s got=%s want=%s)", label, ErrHashMismatch, upstreamAlgo, got, want)
		}
	}

	if err := os.Rename(tmpPath, cachePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("finalizing %s: %w", label, err)
	}

	// Write sha256 sidecar atomically. Failure is non-fatal.
	sha256hex := hex.EncodeToString(sha256h.Sum(nil))
	if werr := os.WriteFile(cachePath+".sha256", []byte(sha256hex+"\n"), 0644); werr != nil {
		logging.Debugf("Verbose: failed to write sidecar for %s: %v\n", label, werr)
	}

	return nil
}

// writeAndHash copies src to dstPath via a .tmp file, optionally validating the
// hash. On mismatch the .tmp is removed and an ErrHashMismatch-wrapped error
// is returned.
func writeAndHash(src io.Reader, dstPath, algo, expected, label string) error {
	tmpPath := dstPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("creating %s: %w", label, err)
	}

	var h *hasher
	var w io.Writer = f
	if algo != "" && expected != "" {
		h, err = newHasher(algo)
		if err != nil {
			f.Close()
			os.Remove(tmpPath)
			return err
		}
		w = io.MultiWriter(f, h)
	}

	_, copyErr := io.Copy(w, src)
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("writing %s: %w", label, copyErr)
	}
	if closeErr != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing %s: %w", label, closeErr)
	}

	if h != nil {
		got := h.Hex()
		want := strings.ToLower(expected)
		if got != want {
			os.Remove(tmpPath)
			return fmt.Errorf("%s: %w (algo=%s got=%s want=%s)", label, ErrHashMismatch, algo, got, want)
		}
	}

	if err := os.Rename(tmpPath, dstPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("finalizing %s: %w", label, err)
	}
	return nil
}

// evictOldCacheFiles removes the oldest jar files in dir, keeping only the newest
// keep entries. Sidecar (.sha256) files are excluded from counting and are removed
// alongside their jar when the jar is evicted. Errors are logged but not returned
// since eviction is best-effort.
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
		// Skip sidecar files; they are managed together with their jar.
		if strings.HasSuffix(e.Name(), ".sha256") {
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
		// Remove the sidecar alongside the jar (ignore error; it may not exist).
		os.Remove(path + ".sha256")
	}
}

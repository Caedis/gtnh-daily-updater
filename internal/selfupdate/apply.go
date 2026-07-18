package selfupdate

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/caedis/gtnh-daily-updater/internal/downloader"
)

// Download fetches the release asset zip to destZip.
func Download(ctx context.Context, info *LatestInfo, destZip string) error {
	if err := os.MkdirAll(filepath.Dir(destZip), 0o755); err != nil {
		return fmt.Errorf("preparing download dir: %w", err)
	}
	return downloader.DownloadToFile(ctx, info.AssetURL, destZip, "", false)
}

// VerifySHA256 computes the SHA256 of path and compares it to expected (hex).
func VerifySHA256(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hashing %s: %w", path, err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, expected) {
		return fmt.Errorf("sha256 mismatch: got %s want %s", got, expected)
	}
	return nil
}

// ExtractBinary opens zipPath, finds the binary entry, and writes it to dest.
func ExtractBinary(zipPath, dest string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("opening zip %s: %w", zipPath, err)
	}
	defer zr.Close()

	want := BinaryName()
	var entry *zip.File
	for _, f := range zr.File {
		base := filepath.Base(f.Name)
		if strings.EqualFold(base, want) {
			entry = f
			break
		}
	}
	if entry == nil {
		return fmt.Errorf("zip %s does not contain %s", zipPath, want)
	}

	rc, err := entry.Open()
	if err != nil {
		return fmt.Errorf("opening zip entry: %w", err)
	}
	defer rc.Close()

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return fmt.Errorf("writing %s: %w", dest, err)
	}
	if err := out.Close(); err != nil {
		return err
	}
	return nil
}

// Replace swaps the running executable with newBinary. On Windows this renames
// the current binary to <exe>.old (Windows allows renaming a running .exe but
// not overwriting it). Callers should remove .old on the next startup.
func Replace(currentExe, newBinary string) error {
	currentExe, err := filepath.Abs(currentExe)
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(currentExe); err == nil {
		currentExe = resolved
	}

	if runtime.GOOS == "windows" {
		old := currentExe + ".old"
		_ = os.Remove(old)
		if err := os.Rename(currentExe, old); err != nil {
			return fmt.Errorf("moving current exe: %w", err)
		}
		if err := os.Rename(newBinary, currentExe); err != nil {
			// cross-device rename fails when cache and exe are on
			// different drives; fall back to copy (target path is free
			// now that the old exe was renamed aside).
			if copyErr := copyFile(newBinary, currentExe); copyErr != nil {
				// best-effort rollback
				_ = os.Remove(currentExe)
				_ = os.Rename(old, currentExe)
				return fmt.Errorf("installing new exe: %w (rename) / %w (copy)", err, copyErr)
			}
			_ = os.Remove(newBinary)
		}
		return nil
	}

	if err := os.Rename(newBinary, currentExe); err != nil {
		// fall back to copy + rename for cross-device moves
		if copyErr := copyFile(newBinary, currentExe+".new"); copyErr != nil {
			return fmt.Errorf("installing new exe: %w (rename) / %w (copy)", err, copyErr)
		}
		if err := os.Rename(currentExe+".new", currentExe); err != nil {
			return fmt.Errorf("installing new exe: %w", err)
		}
		_ = os.Remove(newBinary)
	}
	return nil
}

// CleanupOldExe removes the leftover <exe>.old from a prior Windows update.
// Safe to call on any platform; no-op when the file does not exist.
func CleanupOldExe() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	_ = os.Remove(exe + ".old")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

package fileutil

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// CopyFile copies a single file from src to dst, preserving permissions.
// Parent directories of dst are created as needed.
func CopyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}

	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}

// CopyDirExcluding recursively copies src to dst, skipping named top-level subdirectories.
func CopyDirExcluding(src, dst string, excludeTopLevel ...string) error {
	excludeSet := make(map[string]bool, len(excludeTopLevel))
	for _, e := range excludeTopLevel {
		excludeSet[e] = true
	}

	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		// Skip excluded top-level subdirs
		topLevel := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		if topLevel != "." && excludeSet[topLevel] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		target := filepath.Join(dst, rel)
		if d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(target, info.Mode())
		}
		return CopyFile(path, target)
	})
}

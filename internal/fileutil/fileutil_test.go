package fileutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomicCreatesAndReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	if err := WriteFileAtomic(path, []byte("first"), 0o644); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first" {
		t.Fatalf("content = %q, want %q", got, "first")
	}

	if err := WriteFileAtomic(path, []byte("second"), 0o644); err != nil {
		t.Fatalf("WriteFileAtomic replace: %v", err)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second" {
		t.Fatalf("content = %q, want %q", got, "second")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("dir has %d entries, want 1 (temp file left behind)", len(entries))
	}
}

func TestWriteFileAtomicAppliesPerm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	if err := WriteFileAtomic(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows only reflects the owner-write bit; check readability of the mode
	// we asked for rather than an exact match on that platform.
	if info.Mode().Perm()&0o600 != 0o600 {
		t.Fatalf("perm = %v, want at least 0600", info.Mode().Perm())
	}
}

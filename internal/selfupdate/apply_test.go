package selfupdate

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeZip(t *testing.T, dir string, entries map[string][]byte) (string, string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zw.Create: %v", err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatalf("write entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	path := filepath.Join(dir, "asset.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return path, hex.EncodeToString(sum[:])
}

func TestVerifySHA256(t *testing.T) {
	dir := t.TempDir()
	path, sum := writeZip(t, dir, map[string][]byte{BinaryName(): []byte("hello")})

	if err := VerifySHA256(path, sum); err != nil {
		t.Fatalf("VerifySHA256: %v", err)
	}
	if err := VerifySHA256(path, "00"); err == nil {
		t.Fatal("expected mismatch error")
	}
}

func TestExtractBinary(t *testing.T) {
	dir := t.TempDir()
	body := []byte("binary-bytes")
	path, _ := writeZip(t, dir, map[string][]byte{BinaryName(): body})

	dest := filepath.Join(dir, "out")
	if err := ExtractBinary(path, dest); err != nil {
		t.Fatalf("ExtractBinary: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("contents mismatch: %q", got)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("binary not executable: %v", info.Mode())
	}
}

func TestExtractBinaryMissingEntry(t *testing.T) {
	dir := t.TempDir()
	path, _ := writeZip(t, dir, map[string][]byte{"README.md": []byte("x")})
	if err := ExtractBinary(path, filepath.Join(dir, "out")); err == nil {
		t.Fatal("expected error for missing binary entry")
	}
}

func TestReplace(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "current")
	if err := os.WriteFile(current, []byte("old"), 0o755); err != nil {
		t.Fatalf("WriteFile current: %v", err)
	}
	newBin := filepath.Join(dir, "new")
	if err := os.WriteFile(newBin, []byte("new"), 0o755); err != nil {
		t.Fatalf("WriteFile new: %v", err)
	}

	if err := Replace(current, newBin); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	got, err := os.ReadFile(current)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "new" {
		t.Fatalf("after replace contents=%q want %q", got, "new")
	}
}

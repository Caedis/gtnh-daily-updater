package lwjgl3ify

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

// writeTestZip creates a zip at zipPath with the given entries (name -> content).
// Directory entries have names ending in "/" and empty content.
func writeTestZip(t *testing.T, zipPath string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create entry %q: %v", name, err)
		}
		if content != "" {
			if _, err := w.Write([]byte(content)); err != nil {
				t.Fatalf("zip write entry %q: %v", name, err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
}

func TestExtractMultimcZip_ExtractsAllowedPaths(t *testing.T) {
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "test.zip")
	instanceDir := filepath.Join(tmp, "instance")
	if err := os.MkdirAll(instanceDir, 0755); err != nil {
		t.Fatal(err)
	}

	writeTestZip(t, zipPath, map[string]string{
		"mmc-pack.json":                   `{"pack":1}`,
		"libraries/":                      "",
		"libraries/lwjgl3ify-forge.jar":   "jarbytes",
		"patties/":                        "", // wrong prefix
		"patches/net.minecraft.json":      `{"patch":1}`,
		"README.txt":                      "ignored",
		"randomdir/should-not-extract.txt": "ignored",
	})

	if err := extractMultimcZip(zipPath, instanceDir); err != nil {
		t.Fatalf("extractMultimcZip: %v", err)
	}

	wantPresent := []string{
		"mmc-pack.json",
		"libraries/lwjgl3ify-forge.jar",
		"patches/net.minecraft.json",
	}
	for _, rel := range wantPresent {
		p := filepath.Join(instanceDir, rel)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s extracted: %v", rel, err)
		}
	}

	wantAbsent := []string{
		"README.txt",
		"randomdir/should-not-extract.txt",
		"patties",
	}
	for _, rel := range wantAbsent {
		p := filepath.Join(instanceDir, rel)
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("expected %s not to exist, stat err=%v", rel, err)
		}
	}
}

// TestExtractMultimcZip_RelativeInstanceDir verifies that a relative instanceDir
// (including ".") works. With the previous filepath.Clean-based check, an
// instanceDir of "." caused the prefix check to reject all entries because
// Clean("./mmc-pack.json") == "mmc-pack.json" has no "./" prefix. Using
// filepath.Abs on both sides fixes this.
func TestExtractMultimcZip_RelativeInstanceDir(t *testing.T) {
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "test.zip")
	writeTestZip(t, zipPath, map[string]string{
		"mmc-pack.json": `{"pack":1}`,
	})

	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}

	if err := extractMultimcZip(zipPath, "."); err != nil {
		t.Fatalf("extractMultimcZip with '.': %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmp, "mmc-pack.json")); err != nil {
		t.Errorf("expected mmc-pack.json extracted into cwd: %v", err)
	}
}

// TestExtractMultimcZip_BlocksPathTraversal verifies traversal entries are
// skipped, not written outside the instance directory.
func TestExtractMultimcZip_BlocksPathTraversal(t *testing.T) {
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "test.zip")
	instanceDir := filepath.Join(tmp, "instance")
	if err := os.MkdirAll(instanceDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Entries that pass the allowedPrefixes filter ("libraries/", "patches/")
	// but try to escape via "..". These should be filtered by the abs-path
	// security check, not extracted.
	writeTestZip(t, zipPath, map[string]string{
		"libraries/../../evil.jar": "pwn",
		"patches/../../evil.json":  "pwn",
		"mmc-pack.json":            `{"ok":1}`,
	})

	if err := extractMultimcZip(zipPath, instanceDir); err != nil {
		t.Fatalf("extractMultimcZip: %v", err)
	}

	// Sibling of tmp should never be touched.
	parent := filepath.Dir(tmp)
	for _, name := range []string{"evil.jar", "evil.json"} {
		if _, err := os.Stat(filepath.Join(parent, name)); !os.IsNotExist(err) {
			t.Errorf("traversal wrote %s outside instance (err=%v)", name, err)
		}
		if _, err := os.Stat(filepath.Join(tmp, name)); !os.IsNotExist(err) {
			t.Errorf("traversal wrote %s into tmp root (err=%v)", name, err)
		}
	}

	// Legit entry still extracted.
	if _, err := os.Stat(filepath.Join(instanceDir, "mmc-pack.json")); err != nil {
		t.Errorf("expected mmc-pack.json extracted: %v", err)
	}
}

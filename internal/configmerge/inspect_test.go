package configmerge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiffConfigFiles(t *testing.T) {
	t.Parallel()

	gameDir := t.TempDir()
	configDir := filepath.Join(gameDir, "config")
	mustMkdirAll(t, configDir)

	mustWriteFile(t, filepath.Join(configDir, "added.cfg"), "new-value\n")
	mustWriteFile(t, filepath.Join(configDir, "modified.cfg"), "new-value\n")
	mustWriteFile(t, filepath.Join(configDir, "unchanged.cfg"), "same-value\n")

	baseline := map[string]string{
		"deleted.cfg":   hashBytes([]byte("old-value\n")),
		"modified.cfg":  hashBytes([]byte("old-value\n")),
		"unchanged.cfg": hashBytes([]byte("same-value\n")),
	}

	diffs, err := DiffConfigFiles(gameDir, baseline, false)
	if err != nil {
		t.Fatalf("DiffConfigFiles failed: %v", err)
	}

	if len(diffs) != 3 {
		t.Fatalf("expected 3 differences, got %d (%+v)", len(diffs), diffs)
	}

	got := make(map[string]DiffStatus)
	for _, d := range diffs {
		got[d.Path] = d.Status
	}

	if got["added.cfg"] != DiffAdded {
		t.Fatalf("added.cfg status=%s want=%s", got["added.cfg"], DiffAdded)
	}
	if got["deleted.cfg"] != DiffRemoved {
		t.Fatalf("deleted.cfg status=%s want=%s", got["deleted.cfg"], DiffRemoved)
	}
	if got["modified.cfg"] != DiffModified {
		t.Fatalf("modified.cfg status=%s want=%s", got["modified.cfg"], DiffModified)
	}
	if _, exists := got["unchanged.cfg"]; exists {
		t.Fatalf("unchanged.cfg should be omitted when includeUnchanged=false")
	}
}

func TestDiffConfigFiles_ModpackScopedBaseline(t *testing.T) {
	t.Parallel()

	gameDir := t.TempDir()
	mustMkdirAll(t, filepath.Join(gameDir, "config"))
	mustMkdirAll(t, filepath.Join(gameDir, "scripts"))

	mustWriteFile(t, filepath.Join(gameDir, "config", "changed.cfg"), "new-value\n")
	mustWriteFile(t, filepath.Join(gameDir, "scripts", "added.zs"), "// new\n")

	baseline := map[string]string{
		"config/changed.cfg": hashBytes([]byte("old-value\n")),
		"scripts/deleted.zs": hashBytes([]byte("// old\n")),
	}

	diffs, err := DiffConfigFiles(gameDir, baseline, false)
	if err != nil {
		t.Fatalf("DiffConfigFiles failed: %v", err)
	}

	got := make(map[string]DiffStatus)
	for _, d := range diffs {
		got[d.Path] = d.Status
	}

	if got[filepath.Join("config", "changed.cfg")] != DiffModified {
		t.Fatalf("config/changed.cfg status=%s want=%s", got[filepath.Join("config", "changed.cfg")], DiffModified)
	}
	if got[filepath.Join("scripts", "added.zs")] != DiffAdded {
		t.Fatalf("scripts/added.zs status=%s want=%s", got[filepath.Join("scripts", "added.zs")], DiffAdded)
	}
	if got[filepath.Join("scripts", "deleted.zs")] != DiffRemoved {
		t.Fatalf("scripts/deleted.zs status=%s want=%s", got[filepath.Join("scripts", "deleted.zs")], DiffRemoved)
	}
}

func mustMkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) failed: %v", dir, err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) failed: %v", path, err)
	}
}

package updater

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/caedis/gtnh-daily-updater/internal/assets"
	"github.com/caedis/gtnh-daily-updater/internal/manifest"
)

func TestPickBestMatch(t *testing.T) {
	matches := []assets.FilenameMatch{
		{ModName: "first", Version: "1.0.0"},
		{ModName: "in-manifest", Version: "2.0.0"},
	}
	manifestMods := map[string]manifest.ModInfo{
		"in-manifest": {Version: "2.0.0", Side: "BOTH"},
	}

	got := pickBestMatch(matches, manifestMods)
	if got.ModName != "in-manifest" {
		t.Fatalf("pickBestMatch selected %q, want in-manifest", got.ModName)
	}
}

func TestListTopLevelJarFiles(t *testing.T) {
	modsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(modsDir, "top.jar"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modsDir, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(modsDir, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modsDir, "nested", "inner.jar"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	files, err := listTopLevelJarFiles(modsDir)
	if err != nil {
		t.Fatalf("listTopLevelJarFiles failed: %v", err)
	}

	if len(files) != 1 || !files["top.jar"] {
		t.Fatalf("unexpected top-level jar files: %+v", files)
	}
}

func TestScanInstalledMods(t *testing.T) {
	modsDir := t.TempDir()
	for _, fn := range []string{"a.jar", "b.jar", "c.jar", "dup.jar", "unmatched.jar"} {
		if err := os.WriteFile(filepath.Join(modsDir, fn), []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) failed: %v", fn, err)
		}
	}

	filenameIdx := map[string][]assets.FilenameMatch{
		"a.jar": {
			{ModName: "excluded-mod", Version: "1.0.0", Side: "BOTH"},
		},
		"b.jar": {
			{ModName: "server-only", Version: "1.0.0", Side: "SERVER"},
		},
		"c.jar": {
			{ModName: "client-mod", Version: "1.0.0", Side: "CLIENT"},
		},
		"dup.jar": {
			{ModName: "first-candidate", Version: "1.0.0", Side: "BOTH"},
			{ModName: "manifest-candidate", Version: "2.0.0", Side: "BOTH"},
		},
	}

	manifestMods := map[string]manifest.ModInfo{
		"manifest-candidate": {Version: "2.0.0", Side: "CLIENT"},
		"client-mod":         {Version: "1.0.0", Side: "CLIENT"},
	}

	mods, err := scanInstalledMods(
		modsDir,
		filenameIdx,
		manifestMods,
		map[string]bool{"excluded-mod": true},
		"client",
	)
	if err != nil {
		t.Fatalf("scanInstalledMods failed: %v", err)
	}

	if len(mods) != 2 {
		t.Fatalf("expected 2 installed mods, got %d (%+v)", len(mods), mods)
	}
	if _, ok := mods["excluded-mod"]; ok {
		t.Fatalf("excluded mod should not be present")
	}
	if _, ok := mods["server-only"]; ok {
		t.Fatalf("server-only mod should not be present in client mode")
	}
	if got := mods["manifest-candidate"]; got.Version != "2.0.0" || got.Filename != "dup.jar" || got.Side != "CLIENT" {
		t.Fatalf("unexpected manifest-candidate entry: %+v", got)
	}
	if got := mods["client-mod"]; got.Version != "1.0.0" || got.Filename != "c.jar" {
		t.Fatalf("unexpected client-mod entry: %+v", got)
	}
}

func TestScanInstalledModsUsesManifestSideForFiltering(t *testing.T) {
	modsDir := t.TempDir()
	const jarName = "external.jar"
	if err := os.WriteFile(filepath.Join(modsDir, jarName), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) failed: %v", jarName, err)
	}

	filenameIdx := map[string][]assets.FilenameMatch{
		jarName: {
			// Empty/unknown side metadata from assets DB should not prevent detection
			// when the manifest provides a valid side.
			{ModName: "external-mod", Version: "1.2.3", Side: ""},
		},
	}
	manifestMods := map[string]manifest.ModInfo{
		"external-mod": {Version: "1.2.3", Side: "CLIENT"},
	}

	mods, err := scanInstalledMods(modsDir, filenameIdx, manifestMods, nil, "client")
	if err != nil {
		t.Fatalf("scanInstalledMods failed: %v", err)
	}

	got, ok := mods["external-mod"]
	if !ok {
		t.Fatalf("expected external-mod to be detected, got %+v", mods)
	}
	if got.Filename != jarName || got.Version != "1.2.3" || got.Side != "CLIENT" {
		t.Fatalf("unexpected detected mod: %+v", got)
	}
}

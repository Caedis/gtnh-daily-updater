package updater

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/caedis/gtnh-daily-updater/internal/assets"
	"github.com/caedis/gtnh-daily-updater/internal/config"
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

func TestBuildVersionPattern(t *testing.T) {
	tests := []struct {
		filename    string
		version     string
		wantMatch   []string
		wantNoMatch []string
		wantOk      bool
	}{
		{
			filename:    "buildcraft-7.1.55.jar",
			version:     "7.1.55",
			wantOk:      true,
			wantMatch:   []string{"buildcraft-7.1.55.jar", "buildcraft-CUSTOMBUILD.jar", "buildcraft-7.1.99.jar", "BUILDCRAFT-7.1.55.jar"},
			wantNoMatch: []string{"BuildCraftCompat-7.1.20.jar", "BuildCraftOilTweak-1.1.3.jar"},
		},
		{
			filename: "mod-1.0.0.jar",
			version:  "9.9.9", // version not in filename
			wantOk:   false,
		},
		{
			filename: "mod-1.0.0.jar",
			version:  "", // empty version
			wantOk:   false,
		},
	}
	for _, tc := range tests {
		pat, ok := buildVersionPattern(tc.filename, tc.version)
		if ok != tc.wantOk {
			t.Errorf("buildVersionPattern(%q, %q) ok=%v, want %v", tc.filename, tc.version, ok, tc.wantOk)
			continue
		}
		if !ok {
			continue
		}
		for _, s := range tc.wantMatch {
			if !pat.MatchString(s) {
				t.Errorf("pattern %q should match %q but didn't", pat.String(), s)
			}
		}
		for _, s := range tc.wantNoMatch {
			if pat.MatchString(s) {
				t.Errorf("pattern %q should NOT match %q but did", pat.String(), s)
			}
		}
	}
}

// stubAssetsDB builds a minimal AssetsDB with the given (modName, version, filename) entries.
func stubAssetsDB(entries []struct{ name, version, filename string }) *assets.AssetsDB {
	db := &assets.AssetsDB{}
	for _, e := range entries {
		db.Mods = append(db.Mods, assets.AssetEntry{
			Name: e.name,
			Versions: []assets.VersionAsset{
				{VersionTag: e.version, Filename: e.filename},
			},
		})
	}
	db.BuildIndex()
	return db
}

func TestDetectStaleJars(t *testing.T) {
	db := stubAssetsDB([]struct{ name, version, filename string }{
		{"buildcraft", "7.1.55", "buildcraft-7.1.55.jar"},
		{"BuildCraftCompat", "7.1.20", "BuildCraftCompat-7.1.20.jar"},
		{"BuildCraftOilTweak", "1.1.3", "BuildCraftOilTweak-1.1.3.jar"},
		{"somemod", "2.0.0", "somemod-2.0.0.jar"},
	})

	manifestMods := map[string]manifest.ModInfo{
		"buildcraft":         {Version: "7.1.55", Side: "BOTH"},
		"BuildCraftCompat":   {Version: "7.1.20", Side: "BOTH"},
		"BuildCraftOilTweak": {Version: "1.1.3", Side: "BOTH"},
		"somemod":            {Version: "2.0.0", Side: "BOTH"},
	}

	// Simulate: custom builds present on disk, nothing in scannedMods yet.
	diskJars := map[string]bool{
		"buildcraft-CUSTOMBUILD.jar":         true,
		"BuildCraftCompat-7.1.20-custom.jar": true,
		"BuildCraftOilTweak-1.1.3-patch.jar": true,
		"somemod-1.9.9.jar":                  true,
	}

	result := detectStaleJars(manifestMods, map[string]config.InstalledMod{}, diskJars, db)

	if len(result) != 4 {
		t.Fatalf("expected 4 stale jars, got %d: %+v", len(result), result)
	}
	for _, modName := range []string{"buildcraft", "BuildCraftCompat", "BuildCraftOilTweak", "somemod"} {
		got, ok := result[modName]
		if !ok {
			t.Errorf("expected %q to be detected as stale", modName)
			continue
		}
		if got.Version != "" {
			t.Errorf("%q: expected Version=\"\", got %q", modName, got.Version)
		}
		if got.Filename == "" {
			t.Errorf("%q: expected Filename to be set", modName)
		}
	}

	// Ensure buildcraft didn't steal a BuildCraft* jar.
	if f := result["buildcraft"].Filename; f != "buildcraft-CUSTOMBUILD.jar" {
		t.Errorf("buildcraft matched wrong jar: %q", f)
	}
}

func TestDetectStaleJarsNoDoubleClaim(t *testing.T) {
	// Both mods whose pattern could match the same jar: only the first (reverse-alpha) wins.
	db := stubAssetsDB([]struct{ name, version, filename string }{
		{"zzz-mod", "1.0.0", "zzz-mod-1.0.0.jar"},
		{"aaa-mod", "1.0.0", "aaa-mod-1.0.0.jar"},
	})
	manifestMods := map[string]manifest.ModInfo{
		"zzz-mod": {Version: "1.0.0", Side: "BOTH"},
		"aaa-mod": {Version: "1.0.0", Side: "BOTH"},
	}
	diskJars := map[string]bool{
		"zzz-mod-CUSTOM.jar": true,
	}

	result := detectStaleJars(manifestMods, map[string]config.InstalledMod{}, diskJars, db)
	// Only zzz-mod (comes last alphabetically → first in reverse order) should match.
	if _, ok := result["zzz-mod"]; !ok {
		t.Errorf("expected zzz-mod to claim the jar")
	}
	if _, ok := result["aaa-mod"]; ok {
		t.Errorf("aaa-mod should not have claimed an already-taken jar")
	}
}

func TestDetectStaleJarsAlreadyScanned(t *testing.T) {
	db := stubAssetsDB([]struct{ name, version, filename string }{
		{"mymod", "1.0.0", "mymod-1.0.0.jar"},
	})
	manifestMods := map[string]manifest.ModInfo{
		"mymod": {Version: "1.0.0", Side: "BOTH"},
	}
	diskJars := map[string]bool{
		"mymod-CUSTOM.jar": true,
	}
	// Mod already in scannedMods — should not appear in stale results.
	scannedMods := map[string]config.InstalledMod{
		"mymod": {Version: "1.0.0", Filename: "mymod-1.0.0.jar"},
	}

	result := detectStaleJars(manifestMods, scannedMods, diskJars, db)
	if len(result) != 0 {
		t.Errorf("expected no stale jars when mod already scanned, got %+v", result)
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

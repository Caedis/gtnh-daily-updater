package assets

import (
	"testing"

	"github.com/caedis/gtnh-daily-updater/internal/fileutil"
)

// TestBuildFilenameIndexSanitizedKeys verifies the reverse index maps the raw
// assets-DB filename, its current sanitized form, and its legacy sanitized form
// back to the same mod, each carrying the canonical raw filename. This is what
// lets a jar written to disk under any past sanitization rule be recognized on
// the next scan instead of being re-downloaded.
func TestBuildFilenameIndexSanitizedKeys(t *testing.T) {
	// Apostrophe + spaces are kept by the current rule but were dropped/hyphenated
	// by the legacy rule, so the legacy on-disk name differs from raw.
	raw := "Biomes O' Plenty-1.0.0.jar"
	variants := fileutil.FilenameVariants(raw)
	if len(variants) < 2 {
		t.Fatalf("test precondition: expected raw and a legacy variant, got %v", variants)
	}

	db := &AssetsDB{
		Mods: []AssetEntry{
			{
				Name:   "BiomesOPlenty",
				Source: "external", // non-GTNH: no maven alias
				Versions: []VersionAsset{
					{VersionTag: "1.0.0", Filename: raw},
				},
			},
		},
	}
	db.BuildIndex()

	idx := db.BuildFilenameIndex()

	for _, key := range variants {
		matches := idx[key]
		if len(matches) != 1 {
			t.Fatalf("index[%q]: expected 1 match, got %d (%+v)", key, len(matches), matches)
		}
		m := matches[0]
		if m.ModName != "BiomesOPlenty" || m.Version != "1.0.0" {
			t.Errorf("index[%q]: unexpected match %+v", key, m)
		}
		if m.RawFilename != raw {
			t.Errorf("index[%q]: RawFilename=%q, want %q", key, m.RawFilename, raw)
		}
	}
}

// TestBuildFilenameIndexCleanNameNoDuplicate verifies a filename that needs no
// sanitization produces a single index entry, not a duplicate.
func TestBuildFilenameIndexCleanNameNoDuplicate(t *testing.T) {
	clean := "lwjgl3ify-2.1.5.jar"
	db := &AssetsDB{
		Mods: []AssetEntry{
			{
				Name:   "lwjgl3ify",
				Source: "external",
				Versions: []VersionAsset{
					{VersionTag: "2.1.5", Filename: clean},
				},
			},
		},
	}
	db.BuildIndex()

	idx := db.BuildFilenameIndex()
	if len(idx[clean]) != 1 {
		t.Fatalf("index[%q]: expected exactly 1 match, got %d", clean, len(idx[clean]))
	}
	if idx[clean][0].RawFilename != clean {
		t.Errorf("RawFilename=%q, want %q", idx[clean][0].RawFilename, clean)
	}
}

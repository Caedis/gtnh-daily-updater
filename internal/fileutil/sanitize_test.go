package fileutil

import (
	"slices"
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		// Legal-everywhere characters are now preserved.
		{"brackets kept", "BiblioCraft[v1.11.7][MC1.7.10].jar", "BiblioCraft[v1.11.7][MC1.7.10].jar"},
		{"apostrophe and spaces kept", "Biomes O' Plenty-1.0.0.jar", "Biomes O' Plenty-1.0.0.jar"},
		{"parens kept", "Advanced Solar Panel (Unofficial)-1.0.jar", "Advanced Solar Panel (Unofficial)-1.0.jar"},
		{"plus kept", "lwjgl3ify-2.1.5+forge.jar", "lwjgl3ify-2.1.5+forge.jar"},
		{"clean name unchanged", "GT5-Unofficial-5.09.49.171.jar", "GT5-Unofficial-5.09.49.171.jar"},

		// Percent-encodings are decoded before sanitization.
		{"percent-encoded plus", "CraftPresence-2.0.0%2B1.7.10.jar", "CraftPresence-2.0.0+1.7.10.jar"},
		{"lowercase percent hex", "mod-1.0%2b1.7.10.jar", "mod-1.0+1.7.10.jar"},

		// Windows-forbidden characters are replaced with underscore.
		{"colon replaced", "mod:weird-1.0.jar", "mod_weird-1.0.jar"},
		{"slash replaced", "a/b-1.0.jar", "a_b-1.0.jar"},
		{"pipe and star replaced", "a|b*c-1.0.jar", "a_b_c-1.0.jar"},
		{"decoded slash replaced", "a%2Fb-1.0.jar", "a_b-1.0.jar"},

		// Trailing dots/spaces stripped; leading/trailing space trimmed.
		{"trailing space and dot", "  mod-1.0.jar . ", "mod-1.0.jar"},

		// Windows reserved device names get a prefix.
		{"reserved name", "NUL.jar", "_NUL.jar"},
		{"reserved name case-insensitive", "con.jar", "_con.jar"},
		{"reserved-like but distinct", "CONFIG.jar", "CONFIG.jar"},

		// Degenerate inputs.
		{"empty", "", "_"},
		{"dots only", "..", "_"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SanitizeFilename(tc.in); got != tc.want {
				t.Errorf("SanitizeFilename(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSanitizeFilenameControlChars(t *testing.T) {
	if got := SanitizeFilename("mod\x00\x1f-1.0.jar"); got != "mod__-1.0.jar" {
		t.Errorf("control chars not replaced: %q", got)
	}
}

// TestFilenameVariants verifies the index-key generator returns the raw name,
// the current sanitized form, and the legacy (older-rule) sanitized form, so a
// jar written by any historical rule still resolves to its mod.
func TestFilenameVariants(t *testing.T) {
	raw := "Biomes O' Plenty-1.0.0.jar"
	got := FilenameVariants(raw)

	// raw is preserved as-is under the loosened rules.
	if !slices.Contains(got, raw) {
		t.Errorf("variants %v missing raw %q", got, raw)
	}
	// legacy rule dropped the apostrophe and turned spaces into hyphens.
	const legacy = "Biomes-O-Plenty-1.0.0.jar"
	if !slices.Contains(got, legacy) {
		t.Errorf("variants %v missing legacy form %q", got, legacy)
	}
	// No duplicates.
	seen := map[string]bool{}
	for _, v := range got {
		if seen[v] {
			t.Errorf("variants contain duplicate %q: %v", v, got)
		}
		seen[v] = true
	}
}

func TestFilenameVariantsCleanNameSingle(t *testing.T) {
	raw := "lwjgl3ify-2.1.5.jar"
	got := FilenameVariants(raw)
	if len(got) != 1 || got[0] != raw {
		t.Errorf("clean name should yield single variant, got %v", got)
	}
}

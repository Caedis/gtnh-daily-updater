package configmerge

import (
	"strings"
	"testing"
)

func TestNormalizeDiffPathCandidates(t *testing.T) {
	t.Run("config relative path", func(t *testing.T) {
		candidates, err := normalizeDiffPathCandidates("GregTech/Pollution.cfg")
		if err != nil {
			t.Fatalf("normalizeDiffPathCandidates failed: %v", err)
		}

		want := []string{"GregTech/Pollution.cfg", "config/GregTech/Pollution.cfg"}
		if len(candidates) != len(want) {
			t.Fatalf("candidate length=%d want=%d (%v)", len(candidates), len(want), candidates)
		}
		for i := range want {
			if candidates[i] != want[i] {
				t.Fatalf("candidate[%d]=%q want=%q", i, candidates[i], want[i])
			}
		}
	})

	t.Run("prefixed config path", func(t *testing.T) {
		candidates, err := normalizeDiffPathCandidates("config/GregTech/Pollution.cfg")
		if err != nil {
			t.Fatalf("normalizeDiffPathCandidates failed: %v", err)
		}

		want := []string{"config/GregTech/Pollution.cfg", "GregTech/Pollution.cfg"}
		if len(candidates) != len(want) {
			t.Fatalf("candidate length=%d want=%d (%v)", len(candidates), len(want), candidates)
		}
		for i := range want {
			if candidates[i] != want[i] {
				t.Fatalf("candidate[%d]=%q want=%q", i, candidates[i], want[i])
			}
		}
	})

	t.Run("windows style separators", func(t *testing.T) {
		candidates, err := normalizeDiffPathCandidates(`GregTech\Pollution.cfg`)
		if err != nil {
			t.Fatalf("normalizeDiffPathCandidates failed: %v", err)
		}

		if candidates[0] != "GregTech/Pollution.cfg" {
			t.Fatalf("candidate[0]=%q want=%q", candidates[0], "GregTech/Pollution.cfg")
		}
	})

	t.Run("reject traversal path", func(t *testing.T) {
		if _, err := normalizeDiffPathCandidates("../GregTech/Pollution.cfg"); err == nil {
			t.Fatalf("expected traversal path to fail")
		}
	})
}

func TestRenderUnifiedLineDiff(t *testing.T) {
	t.Run("returns empty for unchanged", func(t *testing.T) {
		diff := renderUnifiedLineDiff([]byte("a\nb\n"), []byte("a\nb\n"), "old", "new")
		if diff != "" {
			t.Fatalf("expected empty diff, got %q", diff)
		}
	})

	t.Run("renders text modifications", func(t *testing.T) {
		diff := renderUnifiedLineDiff(
			[]byte("a\nb\nc\n"),
			[]byte("a\nx\nc\n"),
			"old/path",
			"new/path",
		)

		for _, needle := range []string{
			"--- old/path",
			"+++ new/path",
			"-b",
			"+x",
		} {
			if !strings.Contains(diff, needle) {
				t.Fatalf("diff missing %q:\n%s", needle, diff)
			}
		}
	})

	t.Run("renders additions from empty", func(t *testing.T) {
		diff := renderUnifiedLineDiff(nil, []byte("new-line\n"), "old/path", "new/path")
		if !strings.Contains(diff, "+new-line") {
			t.Fatalf("expected added line marker in diff:\n%s", diff)
		}
	})

	t.Run("reports binary differences", func(t *testing.T) {
		diff := renderUnifiedLineDiff(
			[]byte{0x00, 0x01, 0x02},
			[]byte{0x00, 0x01, 0x03},
			"old/path",
			"new/path",
		)
		if !strings.Contains(diff, "Binary files differ:") {
			t.Fatalf("expected binary diff notice, got %q", diff)
		}
	})
}

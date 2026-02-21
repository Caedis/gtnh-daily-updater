package configmerge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeText(t *testing.T) {
	t.Run("takes theirs when ours unchanged", func(t *testing.T) {
		base := []byte("a\nb\n")
		theirs := []byte("a\nB\n")
		ours := []byte("a\nb\n")

		merged, conflicts := MergeText(base, theirs, ours)
		if string(merged) != string(theirs) {
			t.Fatalf("merged mismatch: got=%q want=%q", merged, theirs)
		}
		if len(conflicts) != 0 {
			t.Fatalf("expected no conflicts, got %v", conflicts)
		}
	})

	t.Run("reports conflict and keeps ours for conflicting edit", func(t *testing.T) {
		base := []byte("a\nb\nc\n")
		theirs := []byte("a\nB\nc\n")
		ours := []byte("a\nX\nc\n")

		merged, conflicts := MergeText(base, theirs, ours)
		if !strings.Contains(string(merged), "X") || strings.Contains(string(merged), "B") {
			t.Fatalf("expected merged text to keep ours: %q", merged)
		}
		if len(conflicts) == 0 {
			t.Fatalf("expected conflict")
		}
	})
}

func TestMergeJSON(t *testing.T) {
	t.Run("user wins on conflicting scalar change", func(t *testing.T) {
		base := []byte(`{"a":1,"b":2}`)
		theirs := []byte(`{"a":2,"b":2}`)
		ours := []byte(`{"a":3,"b":2}`)

		merged, conflicts := MergeJSON(base, theirs, ours)
		var obj map[string]interface{}
		if err := json.Unmarshal(merged, &obj); err != nil {
			t.Fatalf("invalid merged json: %v", err)
		}

		if obj["a"].(float64) != 3 {
			t.Fatalf("expected user value for a, got %v", obj["a"])
		}
		if len(conflicts) == 0 {
			t.Fatalf("expected conflict entry")
		}
	})

	t.Run("pack wins when user removed and pack changed", func(t *testing.T) {
		base := []byte(`{"k":1}`)
		theirs := []byte(`{"k":2}`)
		ours := []byte(`{}`)

		merged, conflicts := MergeJSON(base, theirs, ours)
		var obj map[string]interface{}
		if err := json.Unmarshal(merged, &obj); err != nil {
			t.Fatalf("invalid merged json: %v", err)
		}

		if obj["k"].(float64) != 2 {
			t.Fatalf("expected pack value for k, got %v", obj["k"])
		}
		if len(conflicts) == 0 {
			t.Fatalf("expected conflict entry")
		}
	})
}

func TestMergeCfg_UserWinsOnConflict(t *testing.T) {
	base := []byte("general {\n    I:foo=1\n}\n")
	theirs := []byte("general {\n    I:foo=2\n}\n")
	ours := []byte("general {\n    I:foo=3\n}\n")

	merged, conflicts := MergeCfg(base, theirs, ours)
	if !strings.Contains(string(merged), "I:foo=3") {
		t.Fatalf("expected merged cfg to keep user value, got:\n%s", merged)
	}
	if len(conflicts) == 0 {
		t.Fatalf("expected conflict for differing user/pack changes")
	}
}

func TestMergeFile_NoBaseIsConflict(t *testing.T) {
	merged, conflicts := mergeFile("settings.cfg", nil, []byte("a"), []byte("b"))
	if merged != nil {
		t.Fatalf("expected nil merged output when base is missing")
	}
	if len(conflicts) == 0 {
		t.Fatalf("expected conflict when base is missing")
	}
}

func TestFindPackRoot(t *testing.T) {
	t.Run("direct config dir", func(t *testing.T) {
		tmp := t.TempDir()
		direct := filepath.Join(tmp, "config")
		if err := os.MkdirAll(direct, 0o755); err != nil {
			t.Fatalf("MkdirAll failed: %v", err)
		}
		if got := findPackRoot(tmp); got != tmp {
			t.Fatalf("findPackRoot=%q want=%q", got, tmp)
		}
	})

	t.Run("one level nested config dir", func(t *testing.T) {
		tmp := t.TempDir()
		nested := filepath.Join(tmp, "pack-root", "config")
		if err := os.MkdirAll(nested, 0o755); err != nil {
			t.Fatalf("MkdirAll failed: %v", err)
		}
		want := filepath.Dir(nested)
		if got := findPackRoot(tmp); got != want {
			t.Fatalf("findPackRoot=%q want=%q", got, want)
		}
	})
}

func TestLookupBaseHash_BackwardCompatibleConfigPath(t *testing.T) {
	old := map[string]string{
		filepath.Join("cofh", "world.cfg"): "legacy-hash",
	}

	got, ok := lookupBaseHash(old, filepath.Join("config", "cofh", "world.cfg"))
	if !ok {
		t.Fatalf("lookupBaseHash did not match legacy config path")
	}
	if got != "legacy-hash" {
		t.Fatalf("lookupBaseHash=%q want=%q", got, "legacy-hash")
	}
}

func TestComputeConfigHashes(t *testing.T) {
	t.Run("missing config dir returns empty map", func(t *testing.T) {
		tmp := t.TempDir()
		hashes, err := ComputeConfigHashes(tmp)
		if err != nil {
			t.Fatalf("ComputeConfigHashes failed: %v", err)
		}
		if len(hashes) != 0 {
			t.Fatalf("expected no hashes, got %d", len(hashes))
		}
	})

	t.Run("hashes files under config dir", func(t *testing.T) {
		tmp := t.TempDir()
		configDir := filepath.Join(tmp, "config")
		filePath := filepath.Join(configDir, "a", "settings.txt")
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			t.Fatalf("MkdirAll failed: %v", err)
		}
		content := []byte("hello world")
		if err := os.WriteFile(filePath, content, 0o644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		hashes, err := ComputeConfigHashes(tmp)
		if err != nil {
			t.Fatalf("ComputeConfigHashes failed: %v", err)
		}

		if got := hashes["a/settings.txt"]; got != hashBytes(content) {
			t.Fatalf("unexpected hash: got=%q want=%q", got, hashBytes(content))
		}
	})
}

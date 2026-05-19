package globalconfig

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func setConfigHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("AppData", dir)
	case "darwin":
		t.Setenv("HOME", dir)
	default:
		t.Setenv("XDG_CONFIG_HOME", dir)
	}
	return dir
}

func TestLoadMissingReturnsDefaults(t *testing.T) {
	setConfigHome(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.AutoUpdateCheck || cfg.IncludePrereleases {
		t.Fatalf("expected zero-value config, got %+v", cfg)
	}
}

func TestWriteDefaultIfMissingCreatesFile(t *testing.T) {
	setConfigHome(t)
	if err := WriteDefaultIfMissing(); err != nil {
		t.Fatalf("WriteDefaultIfMissing: %v", err)
	}
	p, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("default template is empty")
	}

	// Second call is a no-op.
	info1, _ := os.Stat(p)
	if err := WriteDefaultIfMissing(); err != nil {
		t.Fatalf("second WriteDefaultIfMissing: %v", err)
	}
	info2, _ := os.Stat(p)
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Fatal("WriteDefaultIfMissing overwrote existing file")
	}
}

func TestLoadParsesValues(t *testing.T) {
	setConfigHome(t)
	p, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	body := "auto_update_check = true\ninclude_prereleases = true\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.AutoUpdateCheck || !cfg.IncludePrereleases {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

package updater

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/caedis/gtnh-daily-updater/internal/assets"
	"github.com/caedis/gtnh-daily-updater/internal/manifest"
)

// stampFixture builds a Prism-style layout (gameDir = instanceDir/.minecraft)
// with one gameDir-level stamp target (config/DreamCoreMod.properties) and one
// instanceDir-level target (server.properties), so a swapped (instanceDir,
// gameDir) argument pair would fail these tests.
func stampFixture(t *testing.T) (instanceDir, gameDir string) {
	t.Helper()
	instanceDir = t.TempDir()
	gameDir = filepath.Join(instanceDir, ".minecraft")

	cfgPath := filepath.Join(gameDir, "config", "DreamCoreMod.properties")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("displayedModpackVersion=2.9.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	propPath := filepath.Join(instanceDir, "server.properties")
	if err := os.WriteFile(propPath, []byte("motd=GT:New Horizons\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return instanceDir, gameDir
}

func TestStampVersionIfNeededStampsDailyVersion(t *testing.T) {
	instanceDir, gameDir := stampFixture(t)
	m := &manifest.DailyManifest{LastUpdated: "2026-07-28T13:58:48.371055+00:00"}
	db := &assets.AssetsDB{LatestDaily: 648}
	result := &UpdateResult{}

	stampVersionIfNeeded(instanceDir, gameDir, m, db, manifest.ModeDaily, "2.9.0-nightly-2026-07-28", Options{}, result)

	if !slices.Contains(result.StampedFiles, "config/DreamCoreMod.properties") {
		t.Fatalf("StampedFiles = %v, want DreamCoreMod.properties", result.StampedFiles)
	}
	if !slices.Contains(result.StampedFiles, "server.properties") {
		t.Fatalf("StampedFiles = %v, want server.properties", result.StampedFiles)
	}

	got, err := os.ReadFile(filepath.Join(gameDir, "config", "DreamCoreMod.properties"))
	if err != nil {
		t.Fatal(err)
	}
	want := "displayedModpackVersion=2.9.x (Daily 648) - 2026-07-28\n"
	if string(got) != want {
		t.Errorf("properties = %q, want %q", got, want)
	}

	// instanceDir-level target: catches an (instanceDir, gameDir) argument swap.
	gotProp, err := os.ReadFile(filepath.Join(instanceDir, "server.properties"))
	if err != nil {
		t.Fatal(err)
	}
	wantProp := "motd=GT:New Horizons 2.9.x (Daily 648) - 2026-07-28\n"
	if string(gotProp) != wantProp {
		t.Errorf("server.properties = %q, want %q", gotProp, wantProp)
	}
}

func TestStampVersionIfNeededSkipsDryRunAndOptOut(t *testing.T) {
	for _, opts := range []Options{{DryRun: true}, {NoVersionStamp: true}} {
		instanceDir, gameDir := stampFixture(t)
		m := &manifest.DailyManifest{LastUpdated: "2026-07-28T13:58:48.371055+00:00"}
		db := &assets.AssetsDB{LatestDaily: 648}
		result := &UpdateResult{}

		stampVersionIfNeeded(instanceDir, gameDir, m, db, manifest.ModeDaily, "2.9.0-nightly-2026-07-28", opts, result)

		if len(result.StampedFiles) != 0 {
			t.Errorf("opts %+v: StampedFiles = %v, want none", opts, result.StampedFiles)
		}
		got, err := os.ReadFile(filepath.Join(gameDir, "config", "DreamCoreMod.properties"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "displayedModpackVersion=2.9.0\n" {
			t.Errorf("opts %+v: properties = %q, want untouched", opts, got)
		}
	}
}

// TestStampVersionIfNeededSkipsWhenConfigSkipped covers the case where
// snapshotAndUpdateConfigsIfNeeded left state.ConfigVersion behind (the
// .gtnh-configs repo is missing): the instance never actually moved to
// configVersion, so nothing should be stamped.
func TestStampVersionIfNeededSkipsWhenConfigSkipped(t *testing.T) {
	instanceDir, gameDir := stampFixture(t)
	m := &manifest.DailyManifest{LastUpdated: "2026-07-28T13:58:48.371055+00:00"}
	db := &assets.AssetsDB{LatestDaily: 648}
	result := &UpdateResult{ConfigSkipped: true}

	stampVersionIfNeeded(instanceDir, gameDir, m, db, manifest.ModeDaily, "2.9.0-nightly-2026-07-28", Options{}, result)

	if len(result.StampedFiles) != 0 {
		t.Errorf("StampedFiles = %v, want none", result.StampedFiles)
	}
	got, err := os.ReadFile(filepath.Join(gameDir, "config", "DreamCoreMod.properties"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "displayedModpackVersion=2.9.0\n" {
		t.Errorf("properties = %q, want untouched", got)
	}
	gotProp, err := os.ReadFile(filepath.Join(instanceDir, "server.properties"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotProp) != "motd=GT:New Horizons\n" {
		t.Errorf("server.properties = %q, want untouched", gotProp)
	}
}

func TestStampVersionIfNeededUsesExperimentalCounter(t *testing.T) {
	instanceDir, gameDir := stampFixture(t)
	m := &manifest.DailyManifest{LastUpdated: "2026-07-28T13:58:48.371055+00:00"}
	db := &assets.AssetsDB{LatestDaily: 648, LatestExperimental: 141}
	result := &UpdateResult{}

	stampVersionIfNeeded(instanceDir, gameDir, m, db, manifest.ModeExperimental, "2.9.0-nightly-2026-07-28", Options{}, result)

	got, err := os.ReadFile(filepath.Join(gameDir, "config", "DreamCoreMod.properties"))
	if err != nil {
		t.Fatal(err)
	}
	want := "displayedModpackVersion=2.9.x (Experimental 141) - 2026-07-28\n"
	if string(got) != want {
		t.Errorf("properties = %q, want %q", got, want)
	}
}

func TestStampVersionIfNeededLatestMarksCountWithPlus(t *testing.T) {
	instanceDir, gameDir := stampFixture(t)
	m := &manifest.DailyManifest{LastUpdated: "2026-07-28T13:58:48.371055+00:00"}
	db := &assets.AssetsDB{LatestDaily: 648}
	result := &UpdateResult{}

	stampVersionIfNeeded(instanceDir, gameDir, m, db, manifest.ModeDaily, "2.9.0", Options{Latest: true}, result)

	got, err := os.ReadFile(filepath.Join(gameDir, "config", "DreamCoreMod.properties"))
	if err != nil {
		t.Fatal(err)
	}
	want := "displayedModpackVersion=2.9.x (Daily 648+) - 2026-07-28\n"
	if string(got) != want {
		t.Errorf("properties = %q, want %q", got, want)
	}
}

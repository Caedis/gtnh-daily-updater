package updater

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/caedis/gtnh-daily-updater/internal/assets"
	"github.com/caedis/gtnh-daily-updater/internal/config"
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

	v := buildDisplayVersion(m, db, manifest.ModeDaily, "2.9.0-nightly-2026-07-28", Options{})
	stampVersionIfNeeded(instanceDir, gameDir, v, Options{}, result)

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

		v := buildDisplayVersion(m, db, manifest.ModeDaily, "2.9.0-nightly-2026-07-28", opts)
		stampVersionIfNeeded(instanceDir, gameDir, v, opts, result)

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

	v := buildDisplayVersion(m, db, manifest.ModeDaily, "2.9.0-nightly-2026-07-28", Options{})
	stampVersionIfNeeded(instanceDir, gameDir, v, Options{}, result)

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

	v := buildDisplayVersion(m, db, manifest.ModeExperimental, "2.9.0-nightly-2026-07-28", Options{})
	stampVersionIfNeeded(instanceDir, gameDir, v, Options{}, result)

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

	v := buildDisplayVersion(m, db, manifest.ModeDaily, "2.9.0", Options{Latest: true})
	stampVersionIfNeeded(instanceDir, gameDir, v, Options{Latest: true}, result)

	got, err := os.ReadFile(filepath.Join(gameDir, "config", "DreamCoreMod.properties"))
	if err != nil {
		t.Fatal(err)
	}
	want := "displayedModpackVersion=2.9.x (Daily 648+) - 2026-07-28\n"
	if string(got) != want {
		t.Errorf("properties = %q, want %q", got, want)
	}
}

func TestBuildDisplayVersionUsesModeCounter(t *testing.T) {
	m := &manifest.DailyManifest{LastUpdated: "2026-07-28T13:58:48.371055+00:00"}
	db := &assets.AssetsDB{LatestDaily: 648, LatestExperimental: 141}

	daily := buildDisplayVersion(m, db, manifest.ModeDaily, "2.9.0-nightly-2026-07-28", Options{})
	if daily.Long != "2.9.x (Daily 648) - 2026-07-28" {
		t.Errorf("daily = %q", daily.Long)
	}

	exp := buildDisplayVersion(m, db, manifest.ModeExperimental, "2.9.0-nightly-2026-07-28", Options{})
	if exp.Long != "2.9.x (Experimental 141) - 2026-07-28" {
		t.Errorf("experimental = %q", exp.Long)
	}

	latest := buildDisplayVersion(m, db, manifest.ModeDaily, "2.9.0-nightly-2026-07-28", Options{Latest: true})
	if latest.Long != "2.9.x (Daily 648+) - 2026-07-28" {
		t.Errorf("latest = %q", latest.Long)
	}
}

// TestRecordDisplayVersionIfChanged covers the already-up-to-date early
// return in run.go, which never reaches persistUpdatedState: a pre-feature
// instance (empty DisplayVersion) must get the built version recorded, and a
// second identical call must leave the file untouched (no-op save).
func TestRecordDisplayVersionIfChanged(t *testing.T) {
	tmp := t.TempDir()
	state := &config.LocalState{Side: "server", ConfigVersion: "cfg-1", Mods: map[string]config.InstalledMod{}}
	if err := state.Save(tmp); err != nil {
		t.Fatalf("Save: %v", err)
	}

	recordDisplayVersionIfChanged(tmp, state, "2.9.x (Daily 648) - 2026-07-28")

	loaded, err := config.Load(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DisplayVersion != "2.9.x (Daily 648) - 2026-07-28" {
		t.Errorf("DisplayVersion = %q, want stamped", loaded.DisplayVersion)
	}

	statBefore, err := os.Stat(filepath.Join(tmp, config.StateFile))
	if err != nil {
		t.Fatal(err)
	}

	// Second call with the already-recorded value: must not rewrite the file.
	recordDisplayVersionIfChanged(tmp, state, "2.9.x (Daily 648) - 2026-07-28")

	statAfter, err := os.Stat(filepath.Join(tmp, config.StateFile))
	if err != nil {
		t.Fatal(err)
	}
	if statBefore.ModTime() != statAfter.ModTime() {
		t.Errorf("state file was rewritten on an unchanged call: before=%v after=%v", statBefore.ModTime(), statAfter.ModTime())
	}
}

func TestPersistUpdatedStateStoresDisplayVersion(t *testing.T) {
	tmp := t.TempDir()
	state := &config.LocalState{Side: "server", ConfigVersion: "cfg-old", Mods: map[string]config.InstalledMod{}}
	m := &manifest.DailyManifest{LastUpdated: "2026-07-28T13:58:48.371055+00:00"}
	db := &assets.AssetsDB{LatestDaily: 648}
	result := &UpdateResult{}
	rollback := func(err error) error { return err }

	err := persistUpdatedState(context.Background(), state, nil, m, manifest.ModeDaily,
		Options{InstanceDir: tmp}, db, nil, nil, rollback,
		"cfg-new", "2.9.x (Daily 648) - 2026-07-28", result)
	if err != nil {
		t.Fatalf("persistUpdatedState: %v", err)
	}

	loaded, err := config.Load(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DisplayVersion != "2.9.x (Daily 648) - 2026-07-28" {
		t.Errorf("DisplayVersion = %q", loaded.DisplayVersion)
	}
	if loaded.ConfigVersion != "cfg-new" {
		t.Errorf("ConfigVersion = %q, want cfg-new", loaded.ConfigVersion)
	}
}

func TestPersistUpdatedStateSkipsDisplayVersionWhenConfigSkipped(t *testing.T) {
	tmp := t.TempDir()
	state := &config.LocalState{Side: "server", ConfigVersion: "cfg-old", Mods: map[string]config.InstalledMod{}}
	m := &manifest.DailyManifest{LastUpdated: "2026-07-28T13:58:48.371055+00:00"}
	db := &assets.AssetsDB{LatestDaily: 648}
	result := &UpdateResult{ConfigSkipped: true}
	rollback := func(err error) error { return err }

	err := persistUpdatedState(context.Background(), state, nil, m, manifest.ModeDaily,
		Options{InstanceDir: tmp}, db, nil, nil, rollback,
		"cfg-new", "2.9.x (Daily 648) - 2026-07-28", result)
	if err != nil {
		t.Fatalf("persistUpdatedState: %v", err)
	}

	loaded, err := config.Load(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DisplayVersion != "" {
		t.Errorf("DisplayVersion = %q, want empty when the config update was skipped", loaded.DisplayVersion)
	}
	if loaded.ConfigVersion != "cfg-old" {
		t.Errorf("ConfigVersion = %q, want cfg-old", loaded.ConfigVersion)
	}
}

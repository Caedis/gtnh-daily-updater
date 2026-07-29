package updater

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caedis/gtnh-daily-updater/internal/assets"
	"github.com/caedis/gtnh-daily-updater/internal/config"
	"github.com/caedis/gtnh-daily-updater/internal/fileutil"
	"github.com/caedis/gtnh-daily-updater/internal/github"
	"github.com/caedis/gtnh-daily-updater/internal/gitconfigs"
	"github.com/caedis/gtnh-daily-updater/internal/logging"
	"github.com/caedis/gtnh-daily-updater/internal/manifest"
	"github.com/caedis/gtnh-daily-updater/internal/maven"
)

// TestRun_SanitizedFilenameNotRedownloaded reproduces issue #49: a mod whose
// canonical assets-DB filename contains characters stripped by an older
// sanitization rule was written to disk under that legacy name, but the scan
// keyed on the raw name failed to recognize it — so every run re-downloaded it.
// The scan must resolve the legacy on-disk jar back to its mod and report it
// Unchanged (dry-run leaves the file untouched).
func TestRun_SanitizedFilenameNotRedownloaded(t *testing.T) {
	instanceDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(instanceDir, "mods"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	const rawJar = "Biomes O' Plenty-1.0.0.jar"
	// Pick the legacy on-disk variant (apostrophe dropped, spaces hyphenated) an
	// older release would have written.
	var legacyJar string
	for _, v := range fileutil.FilenameVariants(rawJar) {
		if v != rawJar {
			legacyJar = v
			break
		}
	}
	if legacyJar == "" {
		t.Fatalf("test precondition: expected a legacy variant different from raw")
	}
	if err := os.WriteFile(filepath.Join(instanceDir, "mods", legacyJar), []byte("jar"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	state := &config.LocalState{
		Side:          "client",
		ManifestDate:  "2026-02-19",
		ConfigVersion: "cfg-1",
		Mods:          map[string]config.InstalledMod{},
	}
	if err := state.Save(instanceDir); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	server := newUpdaterMockServer(t, mockManifestAndAssets{
		manifest: map[string]any{
			"version":       "daily",
			"last_version":  "daily-previous",
			"last_updated":  "2026-02-20",
			"config":        "cfg-1",
			"github_mods":   map[string]any{"BiomesOPlenty": map[string]any{"version": "1.0.0", "side": "BOTH"}},
			"external_mods": map[string]any{},
		},
		assets: map[string]any{
			"config": map[string]any{"versions": []any{}},
			"mods": []any{
				map[string]any{
					"name":           "BiomesOPlenty",
					"latest_version": "1.0.0",
					"source":         "https://example.test/mod",
					"side":           "BOTH",
					"versions": []any{
						map[string]any{
							"version_tag":          "1.0.0",
							"filename":             rawJar,
							"download_url":         "https://example.test/bop.jar",
							"browser_download_url": "https://example.test/bop.jar",
							"prerelease":           false,
						},
					},
				},
			},
		},
	})
	defer server.Close()

	restoreClient := rewriteDefaultHTTPClient(t, server)
	defer restoreClient()

	result, err := Run(context.Background(), Options{
		InstanceDir: instanceDir,
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Added != 0 || result.Updated != 0 || result.Removed != 0 || result.Unchanged != 1 {
		t.Fatalf("expected the sanitized jar to be recognized as unchanged, got summary: %+v", result)
	}
}

func TestRun_DoesNotShortCircuitOnManifestTimestampWithLatest(t *testing.T) {
	instanceDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(instanceDir, "mods"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(instanceDir, "mods", "TestMod-1.0.0.jar"), []byte("jar"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	state := &config.LocalState{
		Side:          "client",
		ManifestDate:  "2026-02-20",
		ConfigVersion: "cfg-1",
		Mods: map[string]config.InstalledMod{
			"TestMod": {
				Version:  "1.0.0",
				Filename: "TestMod-1.0.0.jar",
				Side:     "BOTH",
			},
		},
	}
	if err := state.Save(instanceDir); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	server := newUpdaterMockServer(t, mockManifestAndAssets{
		manifest: map[string]any{
			"version":       "daily",
			"last_version":  "daily-previous",
			"last_updated":  "2026-02-20",
			"config":        "cfg-1",
			"github_mods":   map[string]any{"TestMod": map[string]any{"version": "1.0.0", "side": "BOTH"}},
			"external_mods": map[string]any{},
		},
		assets: map[string]any{
			"config": map[string]any{"versions": []any{}},
			"mods": []any{
				map[string]any{
					"name":           "TestMod",
					"latest_version": "1.1.0",
					"source":         "https://example.test/mod",
					"side":           "BOTH",
					"versions": []any{
						map[string]any{
							"version_tag":          "1.1.0",
							"filename":             "TestMod-1.1.0.jar",
							"download_url":         "https://example.test/TestMod-1.1.0.jar",
							"browser_download_url": "https://example.test/TestMod-1.1.0.jar",
							"prerelease":           false,
						},
						map[string]any{
							"version_tag":          "1.0.0",
							"filename":             "TestMod-1.0.0.jar",
							"download_url":         "https://example.test/TestMod-1.0.0.jar",
							"browser_download_url": "https://example.test/TestMod-1.0.0.jar",
							"prerelease":           false,
						},
					},
				},
			},
		},
	})
	defer server.Close()

	restoreClient := rewriteDefaultHTTPClient(t, server)
	defer restoreClient()

	result, err := Run(context.Background(), Options{
		InstanceDir: instanceDir,
		DryRun:      true,
		Latest:      true,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Updated != 1 || result.Added != 0 || result.Removed != 0 {
		t.Fatalf("unexpected summary: %+v", result)
	}
}

// TestRun_LegacyNamedJarMigratedNoDuplicate verifies that when an on-disk jar
// carries a legacy-sanitized name, a real update migrates it (the old jar is
// removed and the new one written under the current sanitized name) without
// leaving a duplicate behind.
func TestRun_LegacyNamedJarMigratedNoDuplicate(t *testing.T) {
	instanceDir := t.TempDir()
	modsDir := filepath.Join(instanceDir, "mods")
	if err := os.MkdirAll(modsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	const rawOld = "Biomes O' Plenty-1.0.0.jar"
	var legacyOld string
	for _, v := range fileutil.FilenameVariants(rawOld) {
		if v != rawOld {
			legacyOld = v
			break
		}
	}
	if legacyOld == "" {
		t.Fatalf("test precondition: expected a legacy variant for %q", rawOld)
	}
	if err := os.WriteFile(filepath.Join(modsDir, legacyOld), []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Empty state: the scan must discover the legacy jar via the index.
	state := &config.LocalState{
		Side:          "client",
		ConfigVersion: "cfg-1",
		Mods:          map[string]config.InstalledMod{},
	}
	if err := state.Save(instanceDir); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	const newJar = "Biomes O' Plenty-2.0.0.jar"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/GTNewHorizons/DreamAssemblerXXL/master/releases/manifests/daily.json":
			writeJSON(t, w, map[string]any{
				"version":       "daily",
				"last_version":  "daily-previous",
				"last_updated":  "2026-02-20",
				"config":        "cfg-1",
				"github_mods":   map[string]any{"BiomesOPlenty": map[string]any{"version": "2.0.0", "side": "BOTH"}},
				"external_mods": map[string]any{},
			})
		case "/GTNewHorizons/DreamAssemblerXXL/master/gtnh-assets.json":
			writeJSON(t, w, map[string]any{
				"config": map[string]any{"versions": []any{}},
				"mods": []any{
					map[string]any{
						"name":           "BiomesOPlenty",
						"latest_version": "2.0.0",
						"source":         "https://example.test/mod",
						"side":           "BOTH",
						"versions": []any{
							map[string]any{
								"version_tag":          "2.0.0",
								"filename":             newJar,
								"download_url":         "https://example.test/bop2.jar",
								"browser_download_url": "https://example.test/bop2.jar",
								"prerelease":           false,
							},
							map[string]any{
								"version_tag":          "1.0.0",
								"filename":             rawOld,
								"download_url":         "https://example.test/bop1.jar",
								"browser_download_url": "https://example.test/bop1.jar",
								"prerelease":           false,
							},
						},
					},
				},
			})
		case "/bop2.jar":
			if _, err := w.Write([]byte("new")); err != nil {
				t.Fatalf("writing jar: %v", err)
			}
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	restoreClient := rewriteDefaultHTTPClient(t, server)
	defer restoreClient()

	result, err := Run(context.Background(), Options{InstanceDir: instanceDir, NoCache: true})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Updated != 1 || result.Added != 0 || result.Removed != 0 {
		t.Fatalf("unexpected summary: %+v", result)
	}

	// New jar present with the current sanitized name and new content.
	if data, err := os.ReadFile(filepath.Join(modsDir, newJar)); err != nil || string(data) != "new" {
		t.Fatalf("new jar contents=%q err=%v", string(data), err)
	}
	// No leftover jars: exactly one file in mods/.
	entries, err := os.ReadDir(modsDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != newJar {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected only %q in mods dir, got %v", newJar, names)
	}
}

func TestRun_UsesExperimentalManifestFromState(t *testing.T) {
	instanceDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(instanceDir, "mods"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	state := &config.LocalState{
		Side:          "client",
		Mode:          "experimental",
		ManifestDate:  "2026-02-20",
		ConfigVersion: "cfg-exp-1",
		Mods:          map[string]config.InstalledMod{},
	}
	if err := state.Save(instanceDir); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	var dailyRequests, experimentalRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/GTNewHorizons/DreamAssemblerXXL/master/releases/manifests/daily.json":
			dailyRequests++
			t.Fatalf("unexpected daily manifest request for experimental state")
		case "/GTNewHorizons/DreamAssemblerXXL/master/releases/manifests/experimental.json":
			experimentalRequests++
			writeJSON(t, w, map[string]any{
				"version":       "experimental",
				"last_version":  "experimental-previous",
				"last_updated":  "2026-02-21",
				"config":        "cfg-exp-1",
				"github_mods":   map[string]any{},
				"external_mods": map[string]any{},
			})
		case "/GTNewHorizons/DreamAssemblerXXL/master/gtnh-assets.json":
			writeJSON(t, w, map[string]any{
				"config": map[string]any{"versions": []any{}},
				"mods":   []any{},
			})
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	restoreClient := rewriteDefaultHTTPClient(t, server)
	defer restoreClient()

	result, err := Run(context.Background(), Options{
		InstanceDir: instanceDir,
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Added != 0 || result.Updated != 0 || result.Removed != 0 {
		t.Fatalf("unexpected summary: %+v", result)
	}
	if dailyRequests != 0 {
		t.Fatalf("daily requests = %d, want 0", dailyRequests)
	}
	if experimentalRequests == 0 {
		t.Fatalf("expected at least one experimental manifest request")
	}
}

func TestRun_LatestDowngradeResolvedBackToInstalledBecomesUnchanged(t *testing.T) {
	instanceDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(instanceDir, "mods"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(instanceDir, "mods", "TestMod-1.1.0.jar"), []byte("jar"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	state := &config.LocalState{
		Side:          "client",
		ManifestDate:  "2026-02-19",
		ConfigVersion: "cfg-1",
		Mods: map[string]config.InstalledMod{
			"TestMod": {
				Version:  "1.1.0",
				Filename: "TestMod-1.1.0.jar",
				Side:     "BOTH",
			},
		},
	}
	if err := state.Save(instanceDir); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	server := newUpdaterMockServer(t, mockManifestAndAssets{
		manifest: map[string]any{
			"version":       "daily",
			"last_version":  "daily-previous",
			"last_updated":  "2026-02-20",
			"config":        "cfg-1",
			"github_mods":   map[string]any{"TestMod": map[string]any{"version": "1.0.0", "side": "BOTH"}},
			"external_mods": map[string]any{},
		},
		assets: map[string]any{
			"config": map[string]any{"versions": []any{}},
			"mods": []any{
				map[string]any{
					"name":           "TestMod",
					"latest_version": "1.1.0",
					"source":         "https://example.test/mod",
					"side":           "BOTH",
					"versions": []any{
						map[string]any{
							"version_tag":          "1.1.0",
							"filename":             "TestMod-1.1.0.jar",
							"download_url":         "https://example.test/TestMod-1.1.0.jar",
							"browser_download_url": "https://example.test/TestMod-1.1.0.jar",
							"prerelease":           false,
						},
						map[string]any{
							"version_tag":          "1.0.0",
							"filename":             "TestMod-1.0.0.jar",
							"download_url":         "https://example.test/TestMod-1.0.0.jar",
							"browser_download_url": "https://example.test/TestMod-1.0.0.jar",
							"prerelease":           false,
						},
					},
				},
			},
		},
	})
	defer server.Close()

	restoreClient := rewriteDefaultHTTPClient(t, server)
	defer restoreClient()

	result, err := Run(context.Background(), Options{
		InstanceDir: instanceDir,
		DryRun:      true,
		Latest:      true,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Updated != 0 || result.Added != 0 || result.Removed != 0 || result.Unchanged != 1 {
		t.Fatalf("unexpected summary: %+v", result)
	}
}

func TestRun_RemovesExcludedInstalledMod(t *testing.T) {
	instanceDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(instanceDir, "mods"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	const jarName = "TestMod-1.0.0.jar"
	jarPath := filepath.Join(instanceDir, "mods", jarName)
	if err := os.WriteFile(jarPath, []byte("jar"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	state := &config.LocalState{
		Side:          "client",
		ManifestDate:  "2026-02-19",
		ConfigVersion: "cfg-1",
		Mods: map[string]config.InstalledMod{
			"TestMod": {
				Version:  "1.0.0",
				Filename: jarName,
				Side:     "BOTH",
			},
		},
		ExcludeMods: []string{"TestMod"},
	}
	if err := state.Save(instanceDir); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	server := newUpdaterMockServer(t, mockManifestAndAssets{
		manifest: map[string]any{
			"version":       "daily",
			"last_version":  "daily-previous",
			"last_updated":  "2026-02-20",
			"config":        "cfg-1",
			"github_mods":   map[string]any{"TestMod": map[string]any{"version": "1.0.0", "side": "BOTH"}},
			"external_mods": map[string]any{},
		},
		assets: map[string]any{
			"config": map[string]any{"versions": []any{}},
			"mods": []any{
				map[string]any{
					"name":           "TestMod",
					"latest_version": "1.0.0",
					"source":         "https://example.test/mod",
					"side":           "BOTH",
					"versions": []any{
						map[string]any{
							"version_tag":          "1.0.0",
							"filename":             jarName,
							"download_url":         "https://example.test/TestMod-1.0.0.jar",
							"browser_download_url": "https://example.test/TestMod-1.0.0.jar",
							"prerelease":           false,
						},
					},
				},
			},
		},
	})
	defer server.Close()

	restoreClient := rewriteDefaultHTTPClient(t, server)
	defer restoreClient()

	result, err := Run(context.Background(), Options{
		InstanceDir: instanceDir,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Removed != 1 || result.Added != 0 || result.Updated != 0 {
		t.Fatalf("unexpected summary: %+v", result)
	}

	if _, err := os.Stat(jarPath); !os.IsNotExist(err) {
		t.Fatalf("excluded jar still exists, stat err=%v", err)
	}

	updatedState, err := config.Load(instanceDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if _, ok := updatedState.Mods["TestMod"]; ok {
		t.Fatalf("excluded mod still tracked in state: %+v", updatedState.Mods["TestMod"])
	}
}

func TestRun_LatestOutOfAssetsDBIsNotRepeatedlyAdded(t *testing.T) {
	instanceDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(instanceDir, "mods"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	const installedJar = "TestMod-2.0.0.jar"
	if err := os.WriteFile(filepath.Join(instanceDir, "mods", installedJar), []byte("jar"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	state := &config.LocalState{
		Side:          "client",
		ManifestDate:  "2026-02-19",
		ConfigVersion: "cfg-1",
		Mods: map[string]config.InstalledMod{
			"TestMod": {
				Version:  "2.0.0",
				Filename: installedJar,
				Side:     "BOTH",
			},
		},
	}
	if err := state.Save(instanceDir); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	server := newUpdaterMockServer(t, mockManifestAndAssets{
		manifest: map[string]any{
			"version":       "daily",
			"last_version":  "daily-previous",
			"last_updated":  "2026-02-20",
			"config":        "cfg-1",
			"github_mods":   map[string]any{"TestMod": map[string]any{"version": "1.0.0", "side": "BOTH"}},
			"external_mods": map[string]any{},
		},
		assets: map[string]any{
			"config": map[string]any{"versions": []any{}},
			"mods": []any{
				map[string]any{
					"name":           "TestMod",
					"latest_version": "1.0.0",
					"source":         "https://example.test/mod",
					"side":           "BOTH",
					"versions": []any{
						map[string]any{
							"version_tag":          "1.0.0",
							"filename":             "TestMod-1.0.0.jar",
							"download_url":         "https://example.test/TestMod-1.0.0.jar",
							"browser_download_url": "https://example.test/TestMod-1.0.0.jar",
							"prerelease":           false,
						},
					},
				},
			},
		},
	})
	defer server.Close()

	restoreClient := rewriteDefaultHTTPClient(t, server)
	defer restoreClient()

	result, err := Run(context.Background(), Options{
		InstanceDir: instanceDir,
		DryRun:      true,
		Latest:      true,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Added != 0 {
		t.Fatalf("expected no added mods, got summary: %+v", result)
	}
	if result.Updated != 1 {
		t.Fatalf("expected tracked mod to remain update candidate, got summary: %+v", result)
	}
}

func TestRun_GitHubDownloadFailureFallsBackToMaven(t *testing.T) {
	maven.ResetGroupCache()
	instanceDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(instanceDir, "mods"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	state := &config.LocalState{
		Side:          "client",
		ManifestDate:  "2026-02-19",
		ConfigVersion: "cfg-1",
		Mods:          map[string]config.InstalledMod{},
	}
	if err := state.Save(instanceDir); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	var githubAttempts, mavenAttempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/GTNewHorizons/DreamAssemblerXXL/master/releases/manifests/daily.json":
			writeJSON(t, w, map[string]any{
				"version":       "daily",
				"last_version":  "daily-previous",
				"last_updated":  "2026-02-20",
				"config":        "cfg-1",
				"github_mods":   map[string]any{"TestMod": map[string]any{"version": "1.0.0", "side": "BOTH"}},
				"external_mods": map[string]any{},
			})
		case "/GTNewHorizons/DreamAssemblerXXL/master/gtnh-assets.json":
			writeJSON(t, w, map[string]any{
				"config": map[string]any{"versions": []any{}},
				"mods": []any{
					map[string]any{
						"name":           "TestMod",
						"latest_version": "1.0.0",
						"source":         "",
						"side":           "BOTH",
						"versions": []any{
							map[string]any{
								"version_tag":          "1.0.0",
								"filename":             "TestMod-1.0.0.jar",
								"download_url":         "https://api.github.com/repos/GTNewHorizons/TestMod/releases/assets/1",
								"browser_download_url": "https://github.com/GTNewHorizons/TestMod/releases/download/1.0.0/TestMod-1.0.0.jar",
								"prerelease":           false,
							},
						},
					},
				},
			})
		case "/repos/GTNewHorizons/TestMod/releases/assets/1":
			githubAttempts++
			if got := r.Header.Get("Authorization"); got != "token test-token" {
				t.Fatalf("unexpected Authorization header: %q", got)
			}
			w.WriteHeader(http.StatusForbidden)
		case "/repository/releases/com/github/GTNewHorizons/TestMod/1.0.0/TestMod-1.0.0.jar":
			mavenAttempts++
			if _, err := w.Write([]byte("from-maven")); err != nil {
				t.Fatalf("writing maven response: %v", err)
			}
		case "/repository/releases/com/github/GTNewHorizons/TestMod/1.0.0/TestMod-1.0.0.jar.sha256":
			// withMavenFallback probes for a fallback hash; none published here.
			w.WriteHeader(http.StatusNotFound)
		case "/service/rest/v1/search":
			writeJSON(t, w, map[string]any{
				"items": []any{map[string]any{"group": "com.github.GTNewHorizons", "version": "1.0.0"}},
			})
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	restoreClient := rewriteAllHTTPClients(t, server)
	defer restoreClient()

	result, err := Run(context.Background(), Options{
		InstanceDir: instanceDir,
		GithubToken: "test-token",
		NoCache:     true,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Added != 1 || result.Updated != 0 || result.Removed != 0 {
		t.Fatalf("unexpected summary: %+v", result)
	}
	if githubAttempts == 0 {
		t.Fatalf("expected at least one GitHub attempt")
	}
	if mavenAttempts == 0 {
		t.Fatalf("expected Maven fallback attempt")
	}

	jarPath := filepath.Join(instanceDir, "mods", "TestMod-1.0.0.jar")
	data, err := os.ReadFile(jarPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(data) != "from-maven" {
		t.Fatalf("unexpected jar contents: %q", string(data))
	}
}

func TestRun_NonGTNHGroupResolvesAndDownloads(t *testing.T) {
	// Automagy is a GTNH-manifest mod published under group tuhljin.automagy,
	// not com.github.GTNewHorizons. The Maven fallback must resolve that group
	// from Nexus search and download from the slashed group path.
	maven.ResetGroupCache()
	instanceDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(instanceDir, "mods"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	state := &config.LocalState{
		Side:          "client",
		ManifestDate:  "2026-02-19",
		ConfigVersion: "cfg-1",
		Mods:          map[string]config.InstalledMod{},
	}
	if err := state.Save(instanceDir); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	var githubAttempts, mavenAttempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/GTNewHorizons/DreamAssemblerXXL/master/releases/manifests/daily.json":
			writeJSON(t, w, map[string]any{
				"version":       "daily",
				"last_version":  "daily-previous",
				"last_updated":  "2026-02-20",
				"config":        "cfg-1",
				"github_mods":   map[string]any{"Automagy": map[string]any{"version": "1.0.0", "side": "BOTH"}},
				"external_mods": map[string]any{},
			})
		case "/GTNewHorizons/DreamAssemblerXXL/master/gtnh-assets.json":
			writeJSON(t, w, map[string]any{
				"config": map[string]any{"versions": []any{}},
				"mods": []any{
					map[string]any{
						"name":           "Automagy",
						"latest_version": "1.0.0",
						"source":         "",
						"side":           "BOTH",
						"versions": []any{
							map[string]any{
								"version_tag":          "1.0.0",
								"filename":             "Automagy-1.0.0.jar",
								"download_url":         "https://api.github.com/repos/GTNewHorizons/Automagy/releases/assets/1",
								"browser_download_url": "https://github.com/GTNewHorizons/Automagy/releases/download/1.0.0/Automagy-1.0.0.jar",
								"prerelease":           false,
							},
						},
					},
				},
			})
		case "/repos/GTNewHorizons/Automagy/releases/assets/1":
			githubAttempts++
			w.WriteHeader(http.StatusForbidden)
		case "/repository/releases/tuhljin/automagy/Automagy/1.0.0/Automagy-1.0.0.jar":
			mavenAttempts++
			if _, err := w.Write([]byte("from-nongtnh-maven")); err != nil {
				t.Fatalf("writing maven response: %v", err)
			}
		case "/repository/releases/tuhljin/automagy/Automagy/1.0.0/Automagy-1.0.0.jar.sha256":
			w.WriteHeader(http.StatusNotFound)
		case "/service/rest/v1/search":
			if got := r.URL.Query().Get("maven.artifactId"); got != "Automagy" {
				t.Fatalf("unexpected search artifactId: %q", got)
			}
			writeJSON(t, w, map[string]any{
				"items": []any{map[string]any{"group": "tuhljin.automagy", "version": "0.29.7-GTNH"}},
			})
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	restoreClient := rewriteAllHTTPClients(t, server)
	defer restoreClient()

	result, err := Run(context.Background(), Options{
		InstanceDir: instanceDir,
		GithubToken: "test-token",
		NoCache:     true,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Added != 1 || result.Updated != 0 || result.Removed != 0 {
		t.Fatalf("unexpected summary: %+v", result)
	}
	if githubAttempts == 0 {
		t.Fatalf("expected at least one GitHub attempt")
	}
	if mavenAttempts == 0 {
		t.Fatalf("expected Maven fallback attempt via resolved non-GTNH group path")
	}

	jarPath := filepath.Join(instanceDir, "mods", "Automagy-1.0.0.jar")
	data, err := os.ReadFile(jarPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(data) != "from-nongtnh-maven" {
		t.Fatalf("unexpected jar contents: %q", string(data))
	}
}

func TestRun_UpdatesDisabledModKeepsDisabledSuffix(t *testing.T) {
	instanceDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(instanceDir, "mods"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	const oldJar = "TestMod-1.0.0.jar.disabled"
	if err := os.WriteFile(filepath.Join(instanceDir, "mods", oldJar), []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	state := &config.LocalState{
		Side:          "client",
		ManifestDate:  "2026-02-19",
		ConfigVersion: "cfg-1",
		Mods:          map[string]config.InstalledMod{},
	}
	if err := state.Save(instanceDir); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/GTNewHorizons/DreamAssemblerXXL/master/releases/manifests/daily.json":
			writeJSON(t, w, map[string]any{
				"version":       "daily",
				"last_version":  "daily-previous",
				"last_updated":  "2026-02-20",
				"config":        "cfg-1",
				"github_mods":   map[string]any{"TestMod": map[string]any{"version": "2.0.0", "side": "BOTH"}},
				"external_mods": map[string]any{},
			})
		case "/GTNewHorizons/DreamAssemblerXXL/master/gtnh-assets.json":
			writeJSON(t, w, map[string]any{
				"config": map[string]any{"versions": []any{}},
				"mods": []any{
					map[string]any{
						"name":           "TestMod",
						"latest_version": "2.0.0",
						"source":         "",
						"side":           "BOTH",
						"versions": []any{
							map[string]any{
								"version_tag":          "2.0.0",
								"filename":             "TestMod-2.0.0.jar",
								"download_url":         "https://example.test/TestMod-2.0.0.jar",
								"browser_download_url": "https://example.test/TestMod-2.0.0.jar",
								"prerelease":           false,
							},
							map[string]any{
								"version_tag":          "1.0.0",
								"filename":             "TestMod-1.0.0.jar",
								"download_url":         "https://example.test/TestMod-1.0.0.jar",
								"browser_download_url": "https://example.test/TestMod-1.0.0.jar",
								"prerelease":           false,
							},
						},
					},
				},
			})
		case "/TestMod-2.0.0.jar":
			if _, err := w.Write([]byte("new")); err != nil {
				t.Fatalf("writing jar response: %v", err)
			}
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	restoreClient := rewriteDefaultHTTPClient(t, server)
	defer restoreClient()

	result, err := Run(context.Background(), Options{
		InstanceDir: instanceDir,
		NoCache:     true,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Updated != 1 || result.Added != 0 || result.Removed != 0 {
		t.Fatalf("unexpected summary: %+v", result)
	}

	// Old disabled jar gone.
	if _, err := os.Stat(filepath.Join(instanceDir, "mods", oldJar)); !os.IsNotExist(err) {
		t.Fatalf("old disabled jar still exists, stat err=%v", err)
	}
	// New jar keeps the disabled suffix and never appears enabled.
	newJar := filepath.Join(instanceDir, "mods", "TestMod-2.0.0.jar.disabled")
	if data, err := os.ReadFile(newJar); err != nil || string(data) != "new" {
		t.Fatalf("new disabled jar contents=%q err=%v", string(data), err)
	}
	if _, err := os.Stat(filepath.Join(instanceDir, "mods", "TestMod-2.0.0.jar")); !os.IsNotExist(err) {
		t.Fatalf("enabled jar should not exist, stat err=%v", err)
	}

	updatedState, err := config.Load(instanceDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if got := updatedState.Mods["TestMod"]; got.Filename != "TestMod-2.0.0.jar.disabled" || got.Version != "2.0.0" {
		t.Fatalf("unexpected tracked mod: %+v", got)
	}
}

func TestResolveExtraMod_GitHubSourceUsesAPIURLWithToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/releases/latest" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "token test-token" {
			t.Fatalf("unexpected Authorization header: %q", got)
		}
		writeJSON(t, w, map[string]any{
			"tag_name": "v1.2.3",
			"assets": []any{
				map[string]any{
					"name":                 "mod-1.2.3.jar",
					"browser_download_url": "https://example.test/browser.jar",
					"url":                  "https://api.github.com/assets/123",
				},
			},
		})
	}))
	defer server.Close()

	restoreClient := rewriteDefaultHTTPClient(t, server)
	defer restoreClient()

	_, dl, err := resolveExtraMod(
		context.Background(),
		"mod",
		config.ExtraModSpec{Source: "github:owner/repo"},
		nil,
		"test-token",
		"",
		false,
	)
	if err != nil {
		t.Fatalf("resolveExtraMod failed: %v", err)
	}
	if !dl.IsGitHubAPI {
		t.Fatalf("IsGitHubAPI=false want=true")
	}
	if dl.URL != "https://api.github.com/assets/123" {
		t.Fatalf("download URL=%q want API URL", dl.URL)
	}
	if dl.Filename != "mod-1.2.3.jar" {
		t.Fatalf("filename=%q want mod-1.2.3.jar", dl.Filename)
	}
}

func TestResolveExtraMod_GitHubSourceUsesBrowserURLWithoutToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/releases/latest" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("unexpected Authorization header: %q", got)
		}
		writeJSON(t, w, map[string]any{
			"tag_name": "v1.2.3",
			"assets": []any{
				map[string]any{
					"name":                 "mod-1.2.3.jar",
					"browser_download_url": "https://example.test/browser.jar",
					"url":                  "https://api.github.com/assets/123",
				},
			},
		})
	}))
	defer server.Close()

	restoreClient := rewriteDefaultHTTPClient(t, server)
	defer restoreClient()

	_, dl, err := resolveExtraMod(
		context.Background(),
		"mod",
		config.ExtraModSpec{Source: "github:owner/repo"},
		nil,
		"",
		"",
		false,
	)
	if err != nil {
		t.Fatalf("resolveExtraMod failed: %v", err)
	}
	if dl.IsGitHubAPI {
		t.Fatalf("IsGitHubAPI=true want=false")
	}
	if dl.URL != "https://example.test/browser.jar" {
		t.Fatalf("download URL=%q want browser URL", dl.URL)
	}
}

func TestStatus_ResolvesUnpinnedExtraVersionsBeforeDiff(t *testing.T) {
	instanceDir := t.TempDir()
	state := &config.LocalState{
		Side:          "client",
		ManifestDate:  "2026-02-19",
		ConfigVersion: "cfg-1",
		Mods: map[string]config.InstalledMod{
			"ExtraMod": {Version: "1.0.0", Filename: "ExtraMod-1.0.0.jar", Side: "BOTH"},
		},
		ExtraMods: map[string]config.ExtraModSpec{
			"ExtraMod": {Source: "", Version: "", Side: "BOTH"},
		},
	}
	if err := state.Save(instanceDir); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	server := newUpdaterMockServer(t, mockManifestAndAssets{
		manifest: map[string]any{
			"version":       "daily",
			"last_version":  "daily-previous",
			"last_updated":  "2026-02-20",
			"config":        "cfg-1",
			"github_mods":   map[string]any{},
			"external_mods": map[string]any{},
		},
		assets: map[string]any{
			"config": map[string]any{"versions": []any{}},
			"mods": []any{
				map[string]any{
					"name":           "ExtraMod",
					"latest_version": "1.0.0",
					"source":         "https://example.test/mod",
					"side":           "BOTH",
					"versions": []any{
						map[string]any{
							"version_tag":          "1.0.0",
							"filename":             "ExtraMod-1.0.0.jar",
							"download_url":         "https://example.test/ExtraMod-1.0.0.jar",
							"browser_download_url": "https://example.test/ExtraMod-1.0.0.jar",
							"prerelease":           false,
						},
					},
				},
			},
		},
	})
	defer server.Close()

	restoreClient := rewriteDefaultHTTPClient(t, server)
	defer restoreClient()

	logPath := filepath.Join(t.TempDir(), "status.log")
	if err := logging.SetOutputFile(logPath); err != nil {
		t.Fatalf("SetOutputFile failed: %v", err)
	}
	defer func() {
		_ = logging.SetOutputFile("")
	}()

	if err := Status(context.Background(), instanceDir, ""); err != nil {
		t.Fatalf("Status failed: %v", err)
	}

	output, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	text := string(output)
	if !strings.Contains(text, "0 added, 0 removed, 0 updated, 1 unchanged") {
		t.Fatalf("unexpected status summary output:\n%s", text)
	}
}

type mockManifestAndAssets struct {
	manifest map[string]any
	assets   map[string]any
}

func newUpdaterMockServer(t *testing.T, data mockManifestAndAssets) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/GTNewHorizons/DreamAssemblerXXL/master/releases/manifests/daily.json":
			writeJSON(t, w, data.manifest)
		case "/GTNewHorizons/DreamAssemblerXXL/master/releases/manifests/experimental.json":
			writeJSON(t, w, data.manifest)
		case "/GTNewHorizons/DreamAssemblerXXL/master/gtnh-assets.json":
			writeJSON(t, w, data.assets)
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
}

func writeJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("encoding JSON response: %v", err)
	}
}

func rewriteDefaultHTTPClient(t *testing.T, server *httptest.Server) func() {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse failed: %v", err)
	}

	oldClient := http.DefaultClient
	http.DefaultClient = &http.Client{
		Transport: &updaterRewriteHostTransport{
			host: parsed.Host,
			rt:   server.Client().Transport,
		},
	}

	return func() {
		http.DefaultClient = oldClient
	}
}

type updaterRewriteHostTransport struct {
	host string
	rt   http.RoundTripper
}

func (t *updaterRewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = "http"
	cloned.URL.Host = t.host
	return t.rt.RoundTrip(cloned)
}

// rewriteAllHTTPClients points the default client AND the github/maven
// package-level clients at the test server, so FetchLatestRelease and Maven
// metadata lookups (which capture their own client at init) are also rewritten.
func rewriteAllHTTPClients(t *testing.T, server *httptest.Server) func() {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse failed: %v", err)
	}

	client := &http.Client{
		Transport: &updaterRewriteHostTransport{
			host: parsed.Host,
			rt:   server.Client().Transport,
		},
	}

	oldDefault := http.DefaultClient
	http.DefaultClient = client
	oldGitHub := github.SetHTTPClient(client)
	oldMaven := maven.HTTPClient
	maven.HTTPClient = client

	return func() {
		http.DefaultClient = oldDefault
		github.SetHTTPClient(oldGitHub)
		maven.HTTPClient = oldMaven
	}
}

func TestResolveLatest_GitHubPreferredOverMavenNoToken(t *testing.T) {
	maven.ResetGroupCache()
	instanceDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(instanceDir, "mods"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	state := &config.LocalState{
		Side:          "client",
		ManifestDate:  "2026-02-19",
		ConfigVersion: "cfg-1",
		Mods:          map[string]config.InstalledMod{},
	}
	if err := state.Save(instanceDir); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	var githubReleasesHits, mavenMetadataHits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/GTNewHorizons/DreamAssemblerXXL/master/releases/manifests/daily.json":
			writeJSON(t, w, map[string]any{
				"version":       "daily",
				"last_version":  "daily-previous",
				"last_updated":  "2026-02-20",
				"config":        "cfg-1",
				"github_mods":   map[string]any{"TestMod": map[string]any{"version": "1.0.0", "side": "BOTH"}},
				"external_mods": map[string]any{},
			})
		case "/GTNewHorizons/DreamAssemblerXXL/master/gtnh-assets.json":
			writeJSON(t, w, map[string]any{
				"config": map[string]any{"versions": []any{}},
				"mods": []any{
					map[string]any{
						"name":           "TestMod",
						"latest_version": "1.0.0",
						"source":         "",
						"side":           "BOTH",
						"versions": []any{
							map[string]any{
								"version_tag":          "1.0.0",
								"filename":             "TestMod-1.0.0.jar",
								"download_url":         "https://api.github.com/repos/GTNewHorizons/TestMod/releases/assets/1",
								"browser_download_url": "https://github.com/GTNewHorizons/TestMod/releases/download/1.0.0/TestMod-1.0.0.jar",
								"prerelease":           false,
							},
						},
					},
				},
			})
		case "/repos/GTNewHorizons/TestMod/releases":
			githubReleasesHits++
			writeJSON(t, w, []any{
				map[string]any{
					"tag_name":   "1.2.0",
					"prerelease": false,
					"assets": []any{
						map[string]any{
							"name":                 "TestMod-1.2.0.jar",
							"browser_download_url": "https://github.com/GTNewHorizons/TestMod/releases/download/1.2.0/TestMod-1.2.0.jar",
							"url":                  "https://api.github.com/repos/GTNewHorizons/TestMod/releases/assets/2",
						},
					},
				},
			})
		case "/repository/releases/com/github/GTNewHorizons/TestMod/maven-metadata.xml":
			mavenMetadataHits++
			w.Header().Set("Content-Type", "application/xml")
			if _, err := w.Write([]byte(`<metadata><versioning><release>1.1.0</release><versions><version>1.0.0</version><version>1.1.0</version></versions></versioning></metadata>`)); err != nil {
				t.Fatalf("write maven metadata: %v", err)
			}
		case "/GTNewHorizons/TestMod/releases/download/1.2.0/TestMod-1.2.0.jar":
			if _, err := w.Write([]byte("from-github-1.2.0")); err != nil {
				t.Fatalf("write jar: %v", err)
			}
		case "/repository/releases/com/github/GTNewHorizons/TestMod/1.2.0/TestMod-1.2.0.jar.sha256",
			"/repository/releases/com/github/GTNewHorizons/TestMod/1.2.0/TestMod-1.2.0.jar":
			// withMavenFallback probes Maven for a fallback hash/URL on the
			// GitHub-chosen version; the GitHub download succeeds so this is unused.
			w.WriteHeader(http.StatusNotFound)
		case "/repos/GTNewHorizons/GT-New-Horizons-Modpack/releases":
			writeJSON(t, w, []any{})
		case "/service/rest/v1/search":
			writeJSON(t, w, map[string]any{
				"items": []any{map[string]any{"group": "com.github.GTNewHorizons", "version": "1.0.0"}},
			})
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	restore := rewriteAllHTTPClients(t, server)
	defer restore()

	result, err := Run(context.Background(), Options{
		InstanceDir: instanceDir,
		Latest:      true,
		Concurrency: 2,
		NoCache:     true,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Added != 1 {
		t.Fatalf("unexpected summary: %+v", result)
	}
	if githubReleasesHits == 0 {
		t.Fatalf("expected GitHub releases to be queried")
	}
	if mavenMetadataHits != 0 {
		t.Fatalf("Maven metadata must NOT be consulted when GitHub is reachable; hits=%d", mavenMetadataHits)
	}
	if _, err := os.Stat(filepath.Join(instanceDir, "mods", "TestMod-1.2.0.jar")); err != nil {
		t.Fatalf("expected GitHub version 1.2.0 jar on disk: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(instanceDir, "mods", "TestMod-1.2.0.jar"))
	if string(data) != "from-github-1.2.0" {
		t.Fatalf("unexpected jar contents: %q", string(data))
	}
}

func TestResolveLatest_AuthFailsFallsBackToAnon(t *testing.T) {
	maven.ResetGroupCache()
	instanceDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(instanceDir, "mods"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	state := &config.LocalState{
		Side: "client", ManifestDate: "2026-02-19", ConfigVersion: "cfg-1",
		Mods: map[string]config.InstalledMod{},
	}
	if err := state.Save(instanceDir); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	var authedAttempts, anonAttempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/GTNewHorizons/DreamAssemblerXXL/master/releases/manifests/daily.json":
			writeJSON(t, w, map[string]any{
				"version": "daily", "last_version": "daily-previous", "last_updated": "2026-02-20",
				"config":      "cfg-1",
				"github_mods": map[string]any{"TestMod": map[string]any{"version": "1.0.0", "side": "BOTH"}},
				"external_mods": map[string]any{},
			})
		case "/GTNewHorizons/DreamAssemblerXXL/master/gtnh-assets.json":
			writeJSON(t, w, map[string]any{
				"config": map[string]any{"versions": []any{}},
				"mods": []any{map[string]any{
					"name": "TestMod", "latest_version": "1.0.0", "source": "", "side": "BOTH",
					"versions": []any{map[string]any{
						"version_tag": "1.0.0", "filename": "TestMod-1.0.0.jar",
						"download_url":         "https://api.github.com/repos/GTNewHorizons/TestMod/releases/assets/1",
						"browser_download_url": "https://github.com/GTNewHorizons/TestMod/releases/download/1.0.0/TestMod-1.0.0.jar",
						"prerelease":           false,
					}},
				}},
			})
		case "/repos/GTNewHorizons/TestMod/releases":
			if r.Header.Get("Authorization") != "" {
				authedAttempts++
				w.WriteHeader(http.StatusForbidden)
				return
			}
			anonAttempts++
			writeJSON(t, w, []any{map[string]any{
				"tag_name": "1.2.0", "prerelease": false,
				"assets": []any{map[string]any{
					"name":                 "TestMod-1.2.0.jar",
					"browser_download_url": "https://github.com/GTNewHorizons/TestMod/releases/download/1.2.0/TestMod-1.2.0.jar",
				}},
			}})
		case "/GTNewHorizons/TestMod/releases/download/1.2.0/TestMod-1.2.0.jar":
			if _, err := w.Write([]byte("anon-jar")); err != nil {
				t.Fatalf("write jar: %v", err)
			}
		case "/repository/releases/com/github/GTNewHorizons/TestMod/1.2.0/TestMod-1.2.0.jar.sha256",
			"/repository/releases/com/github/GTNewHorizons/TestMod/1.2.0/TestMod-1.2.0.jar":
			w.WriteHeader(http.StatusNotFound)
		case "/repos/GTNewHorizons/GT-New-Horizons-Modpack/releases":
			writeJSON(t, w, []any{})
		case "/service/rest/v1/search":
			writeJSON(t, w, map[string]any{
				"items": []any{map[string]any{"group": "com.github.GTNewHorizons", "version": "1.0.0"}},
			})
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	restore := rewriteAllHTTPClients(t, server)
	defer restore()

	if _, err := Run(context.Background(), Options{
		InstanceDir: instanceDir, Latest: true, Concurrency: 2, NoCache: true,
		GithubToken: "test-token",
	}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if authedAttempts == 0 {
		t.Fatalf("expected an authenticated attempt first")
	}
	if anonAttempts == 0 {
		t.Fatalf("expected an anonymous retry after auth failure")
	}
	if _, err := os.Stat(filepath.Join(instanceDir, "mods", "TestMod-1.2.0.jar")); err != nil {
		t.Fatalf("expected 1.2.0 jar from anon retry: %v", err)
	}
}

func TestResolveLatest_GitHubUnreachableUsesMaven(t *testing.T) {
	maven.ResetGroupCache()
	instanceDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(instanceDir, "mods"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	state := &config.LocalState{
		Side: "client", ManifestDate: "2026-02-19", ConfigVersion: "cfg-1",
		Mods: map[string]config.InstalledMod{},
	}
	if err := state.Save(instanceDir); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	var mavenMetadataHits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/GTNewHorizons/DreamAssemblerXXL/master/releases/manifests/daily.json":
			writeJSON(t, w, map[string]any{
				"version": "daily", "last_version": "daily-previous", "last_updated": "2026-02-20",
				"config":      "cfg-1",
				"github_mods": map[string]any{"TestMod": map[string]any{"version": "1.0.0", "side": "BOTH"}},
				"external_mods": map[string]any{},
			})
		case "/GTNewHorizons/DreamAssemblerXXL/master/gtnh-assets.json":
			writeJSON(t, w, map[string]any{
				"config": map[string]any{"versions": []any{}},
				"mods": []any{map[string]any{
					"name": "TestMod", "latest_version": "1.0.0", "source": "", "side": "BOTH",
					"versions": []any{map[string]any{
						"version_tag": "1.0.0", "filename": "TestMod-1.0.0.jar",
						"download_url":         "https://api.github.com/repos/GTNewHorizons/TestMod/releases/assets/1",
						"browser_download_url": "https://github.com/GTNewHorizons/TestMod/releases/download/1.0.0/TestMod-1.0.0.jar",
						"prerelease":           false,
					}},
				}},
			})
		case "/repos/GTNewHorizons/TestMod/releases":
			w.WriteHeader(http.StatusInternalServerError)
		case "/repository/releases/com/github/GTNewHorizons/TestMod/maven-metadata.xml":
			mavenMetadataHits++
			w.Header().Set("Content-Type", "application/xml")
			if _, err := w.Write([]byte(`<metadata><versioning><release>1.1.0</release><versions><version>1.0.0</version><version>1.1.0</version></versions></versioning></metadata>`)); err != nil {
				t.Fatalf("write maven metadata: %v", err)
			}
		case "/repository/releases/com/github/GTNewHorizons/TestMod/1.1.0/TestMod-1.1.0.jar":
			if _, err := w.Write([]byte("from-maven-1.1.0")); err != nil {
				t.Fatalf("write jar: %v", err)
			}
		case "/repository/releases/com/github/GTNewHorizons/TestMod/1.1.0/TestMod-1.1.0.jar.sha256":
			w.WriteHeader(http.StatusNotFound)
		case "/repos/GTNewHorizons/GT-New-Horizons-Modpack/releases":
			w.WriteHeader(http.StatusInternalServerError)
		case "/service/rest/v1/search":
			writeJSON(t, w, map[string]any{
				"items": []any{map[string]any{"group": "com.github.GTNewHorizons", "version": "1.0.0"}},
			})
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	restore := rewriteAllHTTPClients(t, server)
	defer restore()

	if _, err := Run(context.Background(), Options{
		InstanceDir: instanceDir, Latest: true, Concurrency: 2, NoCache: true,
	}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if mavenMetadataHits == 0 {
		t.Fatalf("expected Maven fallback when GitHub is unreachable")
	}
	if _, err := os.Stat(filepath.Join(instanceDir, "mods", "TestMod-1.1.0.jar")); err != nil {
		t.Fatalf("expected Maven version 1.1.0 jar on disk: %v", err)
	}
}

func TestResolveLatest_GitHubOlderDoesNotConsultMaven(t *testing.T) {
	maven.ResetGroupCache()
	instanceDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(instanceDir, "mods"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	state := &config.LocalState{
		Side: "client", ManifestDate: "2026-02-19", ConfigVersion: "cfg-1",
		Mods: map[string]config.InstalledMod{},
	}
	if err := state.Save(instanceDir); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	var mavenMetadataHits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/GTNewHorizons/DreamAssemblerXXL/master/releases/manifests/daily.json":
			writeJSON(t, w, map[string]any{
				"version": "daily", "last_version": "daily-previous", "last_updated": "2026-02-20",
				"config":      "cfg-1",
				"github_mods": map[string]any{"TestMod": map[string]any{"version": "1.0.0", "side": "BOTH"}},
				"external_mods": map[string]any{},
			})
		case "/GTNewHorizons/DreamAssemblerXXL/master/gtnh-assets.json":
			writeJSON(t, w, map[string]any{
				"config": map[string]any{"versions": []any{}},
				"mods": []any{map[string]any{
					"name": "TestMod", "latest_version": "1.0.0", "source": "", "side": "BOTH",
					"versions": []any{map[string]any{
						"version_tag": "1.0.0", "filename": "TestMod-1.0.0.jar",
						"download_url":         "https://api.github.com/repos/GTNewHorizons/TestMod/releases/assets/1",
						"browser_download_url": "https://github.com/GTNewHorizons/TestMod/releases/download/1.0.0/TestMod-1.0.0.jar",
						"prerelease":           false,
					}},
				}},
			})
		case "/repos/GTNewHorizons/TestMod/releases":
			writeJSON(t, w, []any{map[string]any{
				"tag_name": "1.0.0", "prerelease": false,
				"assets": []any{map[string]any{
					"name":                 "TestMod-1.0.0.jar",
					"browser_download_url": "https://github.com/GTNewHorizons/TestMod/releases/download/1.0.0/TestMod-1.0.0.jar",
				}},
			}})
		case "/repository/releases/com/github/GTNewHorizons/TestMod/maven-metadata.xml":
			mavenMetadataHits++
			w.Header().Set("Content-Type", "application/xml")
			if _, err := w.Write([]byte(`<metadata><versioning><release>1.1.0</release><versions><version>1.0.0</version><version>1.1.0</version></versions></versioning></metadata>`)); err != nil {
				t.Fatalf("write maven metadata: %v", err)
			}
		case "/GTNewHorizons/TestMod/releases/download/1.0.0/TestMod-1.0.0.jar":
			if _, err := w.Write([]byte("from-github-1.0.0")); err != nil {
				t.Fatalf("write jar: %v", err)
			}
		case "/repository/releases/com/github/GTNewHorizons/TestMod/1.0.0/TestMod-1.0.0.jar.sha256",
			"/repository/releases/com/github/GTNewHorizons/TestMod/1.0.0/TestMod-1.0.0.jar":
			w.WriteHeader(http.StatusNotFound)
		case "/repos/GTNewHorizons/GT-New-Horizons-Modpack/releases":
			writeJSON(t, w, []any{})
		case "/service/rest/v1/search":
			writeJSON(t, w, map[string]any{
				"items": []any{map[string]any{"group": "com.github.GTNewHorizons", "version": "1.0.0"}},
			})
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	restore := rewriteAllHTTPClients(t, server)
	defer restore()

	result, err := Run(context.Background(), Options{
		InstanceDir: instanceDir, Latest: true, Concurrency: 2, NoCache: true,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Updated != 0 {
		t.Fatalf("expected no update when GitHub latest == current; got %+v", result)
	}
	if mavenMetadataHits != 0 {
		t.Fatalf("Maven must NOT be consulted when GitHub is reachable; hits=%d", mavenMetadataHits)
	}
	if _, err := os.Stat(filepath.Join(instanceDir, "mods", "TestMod-1.0.0.jar")); err != nil {
		t.Fatalf("expected 1.0.0 jar (added at manifest version): %v", err)
	}
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// TestRun_StampsVersionAfterConfigSnapshot pins the ordering invariant: version
// stamping must run after snapshotAndUpdateConfigsIfNeeded's Snapshot step,
// because a git merge there would otherwise restore the pack's un-stamped
// default and wipe an earlier stamp. The config version is left UNCHANGED
// versus the manifest so only Snapshot runs (no ApplyUpdate, no network),
// keeping the whole run offline.
func TestRun_StampsVersionAfterConfigSnapshot(t *testing.T) {
	if !gitconfigs.IsGitAvailable() {
		t.Skip("git not available")
	}
	instanceDir := t.TempDir()
	gameDir := instanceDir // direct/server layout
	if err := os.MkdirAll(filepath.Join(gameDir, "mods"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	const defaultLine = "displayedModpackVersion=2.9.0\n"
	cfgPath := filepath.Join(gameDir, "config", "DreamCoreMod.properties")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(cfgPath, []byte(defaultLine), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// .gtnh-configs repo on `local`, seeded with the same (unstamped) config the
	// instance carries, as it would be after Init.
	repoDir := gitconfigs.ConfigRepoDir(gameDir)
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	gitCmd(t, repoDir, "init", "-b", "main")
	gitCmd(t, repoDir, "config", "user.name", "test")
	gitCmd(t, repoDir, "config", "user.email", "test@example.com")
	gitCmd(t, repoDir, "config", "commit.gpgsign", "false")
	repoCfg := filepath.Join(repoDir, "config", "DreamCoreMod.properties")
	if err := os.MkdirAll(filepath.Dir(repoCfg), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(repoCfg, []byte(defaultLine), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	gitCmd(t, repoDir, "add", "-A")
	gitCmd(t, repoDir, "commit", "-m", "pack v1")
	gitCmd(t, repoDir, "checkout", "-b", "local")

	state := &config.LocalState{
		Side:          "server",
		ManifestDate:  "2026-07-27",
		ConfigVersion: "cfg-1",
		Mods:          map[string]config.InstalledMod{},
	}
	if err := state.Save(instanceDir); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	m := &manifest.DailyManifest{
		Version:      "daily",
		LastUpdated:  "2026-07-28T00:00:00+00:00",
		Config:       "cfg-1", // unchanged vs state: ApplyUpdate is never invoked
		GithubMods:   map[string]manifest.ModInfo{},
		ExternalMods: map[string]manifest.ModInfo{},
	}
	db := &assets.AssetsDB{LatestDaily: 648}

	result, err := Run(context.Background(), Options{
		InstanceDir: instanceDir,
		Force:       true,
		Shared:      &SharedData{Manifest: m, AssetsDB: db, Mode: "daily"},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.ConfigSkipped {
		t.Fatalf("expected config not skipped, got ConfigSkipped=true")
	}

	// The instance's own file must carry the stamp.
	got, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if !strings.Contains(string(got), "Daily 648") {
		t.Fatalf("instance config not stamped: %q", got)
	}

	// The snapshot commit (made before stamping in Run's call order) must NOT
	// contain the stamp — proving the stamp landed after the snapshot.
	repoContent := gitOutput(t, repoDir, "show", "HEAD:config/DreamCoreMod.properties")
	if strings.Contains(repoContent, "Daily 648") {
		t.Fatalf("snapshot commit already contains the stamp: stamping ran before the snapshot: %q", repoContent)
	}
	if strings.TrimSpace(repoContent) != strings.TrimSpace(defaultLine) {
		t.Fatalf("snapshot commit content = %q, want unstamped default %q", repoContent, defaultLine)
	}
}

package updater

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caedis/gtnh-daily-updater/internal/assets"
	"github.com/caedis/gtnh-daily-updater/internal/config"
	"github.com/caedis/gtnh-daily-updater/internal/manifest"
)

func TestRunRepairsDefaultGuide(t *testing.T) {
	const tag = "2.10.0-nightly-2026-11-24"
	const latestPath = "/repos/GTNewHorizons/GTNH-Guide-Pack/releases/latest"
	const assetPath = "/GTNewHorizons/GTNH-Guide-Pack/releases/download/"
	guide := guideTestZIP(t, "ae2_mechanics/bytes_and_types.md", []byte("page"))
	newGuide := guideTestZIP(t, "ae2_mechanics/bytes_and_types.md", []byte("updated page"))
	for _, tc := range []struct {
		name, side, mode      string
		dryRun, force, latest bool
		deleteAfterFirstRun   bool
		changeRelease         bool
		missingAsset          bool
		badDigest             bool
		wantWarning           bool
		old                   []byte
		body                  []byte
		status, requests      int
		want                  []byte
	}{
		{name: "replace stale", old: []byte("old guide"), body: guide, requests: 1, want: guide},
		{name: "restore missing", body: guide, requests: 1, want: guide},
		{name: "restore after deletion", body: guide, requests: 2, want: guide, deleteAfterFirstRun: true},
		{name: "changed latest on same pack", body: guide, requests: 2, want: guide, changeRelease: true},
		{name: "experimental", mode: manifest.ModeExperimental, body: guide, requests: 1, want: guide},
		{name: "latest", mode: manifest.ModeExperimental, latest: true, body: guide, requests: 1, want: guide},
		{name: "force", force: true, body: guide, requests: 2, want: guide},
		{name: "server", side: "server"},
		{name: "dry run", dryRun: true, old: []byte("old guide"), want: []byte("old guide")},
		{name: "unavailable", status: http.StatusNotFound, requests: 2, old: []byte("old guide"), want: []byte("old guide"), wantWarning: true},
		{name: "missing asset", missingAsset: true, old: []byte("old guide"), want: []byte("old guide"), wantWarning: true},
		{name: "invalid ZIP", body: []byte("broken"), requests: 2, old: []byte("old guide"), want: []byte("old guide"), wantWarning: true},
		{name: "digest mismatch", body: guide, badDigest: true, requests: 2, old: []byte("old guide"), want: []byte("old guide"), wantWarning: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			var latestRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.latest && r.URL.Path == "/repos/GTNewHorizons/GT-New-Horizons-Modpack/releases" {
					_, _ = w.Write([]byte(`[{"tag_name":"` + tag + `"}]`))
					return
				}
				if r.URL.Path == latestPath {
					version := "0.1.30"
					if latestRequests.Add(1) == 2 && tc.changeRelease {
						version = "0.1.26.2"
					}
					asset := "GTNH-Guide-Pack-" + version + ".zip"
					var releaseAssets []map[string]string
					if !tc.missingAsset {
						entry := map[string]string{"name": asset, "browser_download_url": "https://github.com" + assetPath + version + "/" + asset}
						if tc.badDigest {
							entry["digest"] = "sha256:" + strings.Repeat("0", 64)
						}
						releaseAssets = append(releaseAssets, entry)
					}
					writeJSON(t, w, map[string]any{"tag_name": version, "assets": releaseAssets})
					return
				}
				if r.URL.Path == assetPath+"0.1.26.2/GTNH-Guide-Pack-0.1.26.2.zip" && tc.changeRelease {
					requests.Add(1)
					_, _ = w.Write(newGuide)
					return
				}
				if r.URL.Path != assetPath+"0.1.30/GTNH-Guide-Pack-0.1.30.zip" {
					t.Errorf("unexpected request: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				requests.Add(1)
				if tc.status != 0 {
					w.WriteHeader(tc.status)
					return
				}
				_, _ = w.Write(tc.body)
			}))
			defer server.Close()
			defer rewriteAllHTTPClients(t, server, true)()

			instance := t.TempDir()
			gameDir := filepath.Join(instance, ".minecraft")
			if err := os.MkdirAll(filepath.Join(gameDir, "mods"), 0755); err != nil {
				t.Fatal(err)
			}
			side, mode := tc.side, tc.mode
			if side == "" {
				side = "client"
			}
			if mode == "" {
				mode = manifest.ModeDaily
			}
			state := config.LocalState{Side: side, Mode: mode, ConfigVersion: tag}
			if err := state.Save(instance); err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(gameDir, "config", "guidenh", "DefaultGuide.zip")
			if tc.old != nil {
				if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(dest, tc.old, 0644); err != nil {
					t.Fatal(err)
				}
			}
			manifestTag := tag
			if tc.latest {
				manifestTag = "2.10.0-nightly-2026-11-23"
			}
			opts := Options{
				InstanceDir: instance, NoVersionStamp: true, DryRun: tc.dryRun, Force: tc.force, Latest: tc.latest,
				Shared: &SharedData{Mode: mode, Manifest: &manifest.DailyManifest{Config: manifestTag}, AssetsDB: &assets.AssetsDB{}},
			}
			for run := range 2 {
				result, err := Run(context.Background(), opts)
				if err != nil {
					t.Fatal(err)
				}
				if (result.DefaultGuideError != nil) != tc.wantWarning {
					t.Fatalf("DefaultGuideError = %v, want warning %v", result.DefaultGuideError, tc.wantWarning)
				}
				if warn := result.DefaultGuideError; warn != nil && !strings.Contains(warn.Error(), "DefaultGuide.zip (0.1.30)") {
					t.Fatalf("warning lacks release context: %v", warn)
				}
				got, err := os.ReadFile(dest)
				if tc.want == nil && os.IsNotExist(err) {
					continue
				}
				want := tc.want
				if run == 1 && tc.changeRelease {
					want = newGuide
				}
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("DefaultGuide.zip = %d bytes, %v; want %d bytes", len(got), err, len(want))
				}
				if run == 0 && tc.deleteAfterFirstRun {
					if err := os.Remove(dest); err != nil {
						t.Fatal(err)
					}
				}
			}
			if got := int(requests.Load()); got != tc.requests {
				t.Errorf("download requests = %d, want %d", got, tc.requests)
			}
			wantLatestRequests := int32(2)
			if tc.side == "server" || tc.dryRun {
				wantLatestRequests = 0
			}
			if got := latestRequests.Load(); got != wantLatestRequests {
				t.Errorf("latest release requests = %d, want %d", got, wantLatestRequests)
			}
		})
	}
}

func guideTestZIP(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	entry, err := writer.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

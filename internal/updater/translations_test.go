package updater

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/caedis/gtnh-daily-updater/internal/assets"
	"github.com/caedis/gtnh-daily-updater/internal/config"
	"github.com/caedis/gtnh-daily-updater/internal/manifest"
)

func TestUpdateTranslation(t *testing.T) {
	const translated = "config/txloader/load/gregtech/lang/ru_RU.lang"
	const unrelated = "config/txloader/load/gregtech/lang/en_US.lang"
	const gregtech = "GregTech_ru_RU.lang"
	const release = "GTNH-ru_RU-Translation-Daily-2026-11-18+526.zip"

	for _, tc := range []struct {
		name       string
		status     int
		invalidZIP bool
		side       string
		mode       string
		dryRun     bool
	}{
		{name: "replace complete files and preserve unrelated files", status: http.StatusOK, side: "client", mode: manifest.ModeDaily},
		{name: "preserve installed translation on HTTP error", status: http.StatusNotFound, side: "client", mode: manifest.ModeDaily},
		{name: "preserve installed translation on invalid archive", status: http.StatusOK, invalidZIP: true, side: "client", mode: manifest.ModeDaily},
		{name: "skip daily server", status: http.StatusOK, side: "server", mode: manifest.ModeDaily},
		{name: "skip experimental client", status: http.StatusOK, side: "client", mode: manifest.ModeExperimental},
		{name: "skip dry run", status: http.StatusOK, side: "client", mode: manifest.ModeDaily, dryRun: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range map[string]string{
				"options.txt": "lang:ru_ru\n",
				translated:    "local edit\nlocal key\n",
				unrelated:     "other language",
				gregtech:      "local GregTech",
			} {
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			var data bytes.Buffer
			archive := zip.NewWriter(&data)
			for _, entry := range [][2]string{{translated, "official\n"}, {gregtech, "official GregTech"}} {
				writer, err := archive.Create(entry[0])
				if err != nil {
					t.Fatal(err)
				}
				if _, err := writer.Write([]byte(entry[1])); err != nil {
					t.Fatal(err)
				}
			}
			if tc.invalidZIP {
				if _, err := archive.Create("config/../unrelated.txt"); err != nil {
					t.Fatal(err)
				}
			}
			if err := archive.Close(); err != nil {
				t.Fatal(err)
			}
			shouldDownload := tc.side == "client" && tc.mode == manifest.ModeDaily && !tc.dryRun
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.WriteHeader(tc.status)
				if tc.status == http.StatusOK {
					_, _ = w.Write(data.Bytes())
				}
			}))
			defer server.Close()
			db := &assets.AssetsDB{Translations: assets.AssetEntry{Versions: []assets.VersionAsset{
				{VersionTag: "de_DE-latest", Filename: "GTNH-de_DE-Translation-Daily-2026-11-18+526.zip"},
				{VersionTag: "ru_RU-latest", Filename: release, BrowserDownloadURL: server.URL},
			}}}

			state := &config.LocalState{Side: tc.side, Mode: tc.mode, ConfigVersion: "cfg"}
			if err := state.Save(dir); err != nil {
				t.Fatal(err)
			}
			_, err := Run(t.Context(), Options{
				InstanceDir: dir,
				DryRun:      tc.dryRun,
				Shared:      &SharedData{Mode: tc.mode, Manifest: &manifest.DailyManifest{Config: "cfg"}, AssetsDB: db},
			})
			if err != nil {
				t.Fatal(err)
			}
			if (requests.Load() > 0) != shouldDownload {
				t.Fatalf("translation download requests = %d, want download: %v", requests.Load(), shouldDownload)
			}

			expected := map[string]string{
				translated: "official\n",
				unrelated:  "other language",
				gregtech:   "official GregTech",
			}
			if !shouldDownload || tc.status != http.StatusOK || tc.invalidZIP {
				expected[translated] = "local edit\nlocal key\n"
				expected[gregtech] = "local GregTech"
			}
			for name, want := range expected {
				got, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil || string(got) != want {
					t.Errorf("%s = %q, %v; want %q", name, got, err, want)
				}
			}
		})
	}
}

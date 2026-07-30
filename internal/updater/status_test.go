package updater

import (
	"testing"

	"github.com/caedis/gtnh-daily-updater/internal/assets"
	"github.com/caedis/gtnh-daily-updater/internal/config"
	"github.com/caedis/gtnh-daily-updater/internal/manifest"
)

func TestStatusVersions(t *testing.T) {
	m := &manifest.DailyManifest{
		LastUpdated: "2026-07-28T13:58:48.371055+00:00",
		Config:      "2.9.0-nightly-2026-07-28",
	}
	db := &assets.AssetsDB{LatestDaily: 648, LatestExperimental: 141}

	tests := []struct {
		name        string
		state       *config.LocalState
		db          *assets.AssetsDB
		mode        string
		wantCurrent string
		wantLatest  string
	}{
		{
			name:        "stored display version",
			state:       &config.LocalState{ManifestDate: "2026-07-27", DisplayVersion: "2.9.x (Daily 647) - 2026-07-27"},
			db:          db,
			mode:        manifest.ModeDaily,
			wantCurrent: "2.9.x (Daily 647) - 2026-07-27",
			wantLatest:  "2.9.x (Daily 648) - 2026-07-28",
		},
		{
			name: "no stored display version falls back to config version",
			state: &config.LocalState{
				ManifestDate:  "2026-07-27T11:04:12.331+00:00",
				ConfigVersion: "2.9.0-nightly-2026-07-27",
			},
			db:          db,
			mode:        manifest.ModeDaily,
			wantCurrent: "2.9.0-nightly-2026-07-27",
			wantLatest:  "2.9.x (Daily 648) - 2026-07-28",
		},
		{
			name:        "experimental uses its own counter",
			state:       &config.LocalState{ManifestDate: "2026-07-27", DisplayVersion: "2.9.x (Experimental 140) - 2026-07-27"},
			db:          db,
			mode:        manifest.ModeExperimental,
			wantCurrent: "2.9.x (Experimental 140) - 2026-07-27",
			wantLatest:  "2.9.x (Experimental 141) - 2026-07-28",
		},
		{
			name:        "up to date needs no assets db",
			state:       &config.LocalState{ManifestDate: "2026-07-28", DisplayVersion: "2.9.x (Daily 648) - 2026-07-28"},
			db:          nil,
			mode:        manifest.ModeDaily,
			wantCurrent: "2.9.x (Daily 648) - 2026-07-28",
			wantLatest:  "2.9.x (Daily 648) - 2026-07-28",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			current, latest := statusVersions(tc.state, m, tc.db, tc.mode)
			if current != tc.wantCurrent {
				t.Errorf("current = %q, want %q", current, tc.wantCurrent)
			}
			if latest != tc.wantLatest {
				t.Errorf("latest = %q, want %q", latest, tc.wantLatest)
			}
		})
	}
}

// TestFinalizeUpToDate covers a manifest regenerated with no real change: the
// cheap timestamp check misses it (LastUpdated advanced), but Current and
// Latest come out identical, so the instance must still read as up to date.
func TestFinalizeUpToDate(t *testing.T) {
	tests := []struct {
		name          string
		cheapUpToDate bool
		current       string
		latest        string
		want          bool
	}{
		{
			name:          "cheap check already true",
			cheapUpToDate: true,
			current:       "2.9.x (Daily 648) - 2026-07-28",
			latest:        "2.9.x (Daily 649) - 2026-07-29",
			want:          true,
		},
		{
			name:          "regenerated manifest but identical display strings",
			cheapUpToDate: false,
			current:       "2.9.x (Daily 648) - 2026-07-28",
			latest:        "2.9.x (Daily 648) - 2026-07-28",
			want:          true,
		},
		{
			name:          "real difference",
			cheapUpToDate: false,
			current:       "2.9.x (Daily 647) - 2026-07-27",
			latest:        "2.9.x (Daily 648) - 2026-07-28",
			want:          false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := finalizeUpToDate(tc.cheapUpToDate, tc.current, tc.latest); got != tc.want {
				t.Errorf("finalizeUpToDate(%v, %q, %q) = %v, want %v", tc.cheapUpToDate, tc.current, tc.latest, got, tc.want)
			}
		})
	}
}

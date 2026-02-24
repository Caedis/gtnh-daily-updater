package updater

import (
	"testing"

	"github.com/caedis/gtnh-daily-updater/internal/config"
	"github.com/caedis/gtnh-daily-updater/internal/manifest"
)

func TestResolveMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state *config.LocalState
		want  string
	}{
		{
			name:  "nil state defaults daily",
			state: nil,
			want:  manifest.ModeDaily,
		},
		{
			name: "explicit mode wins",
			state: &config.LocalState{
				Mode:          manifest.ModeExperimental,
				ConfigVersion: "2.8.0-nightly-2026-02-24",
			},
			want: manifest.ModeExperimental,
		},
		{
			name: "config version experimental inferred",
			state: &config.LocalState{
				ConfigVersion: "2.8.0-experimental-2026-02-24",
			},
			want: manifest.ModeExperimental,
		},
		{
			name: "invalid mode falls back to config inference",
			state: &config.LocalState{
				Mode:          "unknown",
				ConfigVersion: "2.8.0-experimental-2026-02-24",
			},
			want: manifest.ModeExperimental,
		},
		{
			name: "default daily",
			state: &config.LocalState{
				ConfigVersion: "2.8.0-nightly-2026-02-24",
			},
			want: manifest.ModeDaily,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveMode(tt.state); got != tt.want {
				t.Fatalf("resolveMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveInitMode(t *testing.T) {
	t.Parallel()

	track, err := resolveInitMode("2.8.0-nightly-2026-02-24", manifest.ModeExperimental)
	if err != nil {
		t.Fatalf("resolveInitMode returned unexpected error: %v", err)
	}
	if track != manifest.ModeExperimental {
		t.Fatalf("track = %q, want %q", track, manifest.ModeExperimental)
	}

	track, err = resolveInitMode("2.8.0-experimental-2026-02-24", "")
	if err != nil {
		t.Fatalf("resolveInitMode returned unexpected error: %v", err)
	}
	if track != manifest.ModeExperimental {
		t.Fatalf("track = %q, want %q", track, manifest.ModeExperimental)
	}

	if _, err := resolveInitMode("", "unknown"); err == nil {
		t.Fatalf("expected error for invalid mode")
	}
}

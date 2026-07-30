package cmd

import "testing"

func TestVersionTransition(t *testing.T) {
	tests := []struct {
		name string
		old  string
		new  string
		want string
	}{
		{
			name: "version moved",
			old:  "2.9.x (Daily 647) - 2026-07-27",
			new:  "2.9.x (Daily 648) - 2026-07-28",
			want: "2.9.x (Daily 647) - 2026-07-27 → 2.9.x (Daily 648) - 2026-07-28",
		},
		{
			name: "version held still",
			old:  "2.9.x (Daily 653+) - 2026-07-30",
			new:  "2.9.x (Daily 653+) - 2026-07-30",
			want: "2.9.x (Daily 653+) - 2026-07-30 (pack version unchanged)",
		},
		{
			// Instances updated before display versions were tracked report the
			// config tag on the left; the two sides differ, so it still reads
			// as a transition.
			name: "migration from a config tag",
			old:  "2.9.0-nightly-2026-07-29-02",
			new:  "2.9.x (Daily 653) - 2026-07-30",
			want: "2.9.0-nightly-2026-07-29-02 → 2.9.x (Daily 653) - 2026-07-30",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := versionTransition(tc.old, tc.new); got != tc.want {
				t.Errorf("versionTransition(%q, %q) = %q, want %q", tc.old, tc.new, got, tc.want)
			}
		})
	}
}

func TestVersionCell(t *testing.T) {
	tests := []struct {
		name string
		old  string
		new  string
		want string
	}{
		{
			name: "version moved",
			old:  "2.9.x (Daily 647) - 2026-07-27",
			new:  "2.9.x (Daily 648) - 2026-07-28",
			want: "2.9.x (Daily 647) - 2026-07-27 → 2.9.x (Daily 648) - 2026-07-28",
		},
		{
			name: "version held still carries no note",
			old:  "2.9.x (Daily 653+) - 2026-07-30",
			new:  "2.9.x (Daily 653+) - 2026-07-30",
			want: "2.9.x (Daily 653+) - 2026-07-30",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := versionCell(tc.old, tc.new); got != tc.want {
				t.Errorf("versionCell(%q, %q) = %q, want %q", tc.old, tc.new, got, tc.want)
			}
		})
	}
}

package versionstamp

import "testing"

func TestDevCycle(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"2.9.0-nightly-2026-07-28", "2.9.x"},
		{"2.10.3", "2.10.x"},
		{"2.9.0", "2.9.x"},
		{"nightly", "nightly"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := DevCycle(tc.in); got != tc.want {
			t.Errorf("DevCycle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuild(t *testing.T) {
	tests := []struct {
		name        string
		configVer   string
		mode        string
		count       int
		lastUpdated string
		beyond      bool
		wantShort   string
		wantLong    string
		wantDate    string
	}{
		{
			name:        "daily",
			configVer:   "2.9.0-nightly-2026-07-28",
			mode:        "daily",
			count:       648,
			lastUpdated: "2026-07-28T13:58:48.371055+00:00",
			beyond:      false,
			wantShort:   "2.9.x (Daily 648)",
			wantLong:    "2.9.x (Daily 648) - 2026-07-28",
			wantDate:    "2026-07-28",
		},
		{
			name:        "experimental",
			configVer:   "2.9.0-nightly-2026-07-28",
			mode:        "experimental",
			count:       141,
			lastUpdated: "2026-07-28T13:58:48.371055+00:00",
			beyond:      false,
			wantShort:   "2.9.x (Experimental 141)",
			wantLong:    "2.9.x (Experimental 141) - 2026-07-28",
			wantDate:    "2026-07-28",
		},
		{
			name:        "latest marks the count with a plus",
			configVer:   "2.9.0-nightly-2026-07-28",
			mode:        "daily",
			count:       648,
			lastUpdated: "2026-07-28T13:58:48.371055+00:00",
			beyond:      true,
			wantShort:   "2.9.x (Daily 648+)",
			wantLong:    "2.9.x (Daily 648+) - 2026-07-28",
			wantDate:    "2026-07-28",
		},
		{
			name:        "latest on a release tag keeps the cycle form",
			configVer:   "2.9.0",
			mode:        "experimental",
			count:       141,
			lastUpdated: "2026-07-28T13:58:48.371055+00:00",
			beyond:      true,
			wantShort:   "2.9.x (Experimental 141+)",
			wantLong:    "2.9.x (Experimental 141+) - 2026-07-28",
			wantDate:    "2026-07-28",
		},
		{
			name:        "missing date drops the suffix",
			configVer:   "2.9.0-nightly-2026-07-28",
			mode:        "daily",
			count:       648,
			lastUpdated: "",
			beyond:      false,
			wantShort:   "2.9.x (Daily 648)",
			wantLong:    "2.9.x (Daily 648)",
			wantDate:    "",
		},
		{
			name:        "unparseable cycle falls back to raw version",
			configVer:   "weird-tag",
			mode:        "daily",
			count:       1,
			lastUpdated: "2026-07-28",
			beyond:      false,
			wantShort:   "weird-tag (Daily 1)",
			wantLong:    "weird-tag (Daily 1) - 2026-07-28",
			wantDate:    "2026-07-28",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Build(tc.configVer, tc.mode, tc.count, tc.lastUpdated, tc.beyond)
			if got.Short != tc.wantShort {
				t.Errorf("Short = %q, want %q", got.Short, tc.wantShort)
			}
			if got.Long != tc.wantLong {
				t.Errorf("Long = %q, want %q", got.Long, tc.wantLong)
			}
			if got.Date != tc.wantDate {
				t.Errorf("Date = %q, want %q", got.Date, tc.wantDate)
			}
		})
	}
}

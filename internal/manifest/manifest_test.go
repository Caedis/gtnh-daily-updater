package manifest

import "testing"

func TestParseMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "empty defaults daily", input: "", want: ModeDaily},
		{name: "daily", input: "daily", want: ModeDaily},
		{name: "experimental", input: "experimental", want: ModeExperimental},
		{name: "case and whitespace normalized", input: "  ExPeRiMeNtAl ", want: ModeExperimental},
		{name: "invalid", input: "beta", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseMode(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseMode(%q) expected error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseMode(%q) returned error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("ParseMode(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestURLForMode(t *testing.T) {
	t.Parallel()

	url, err := URLForMode(ModeDaily)
	if err != nil {
		t.Fatalf("URLForMode(daily) returned error: %v", err)
	}
	if url != DailyManifestURL {
		t.Fatalf("URLForMode(daily) = %q, want %q", url, DailyManifestURL)
	}

	url, err = URLForMode(ModeExperimental)
	if err != nil {
		t.Fatalf("URLForMode(experimental) returned error: %v", err)
	}
	if url != ExperimentalManifestURL {
		t.Fatalf("URLForMode(experimental) = %q, want %q", url, ExperimentalManifestURL)
	}
}

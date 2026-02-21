package cmd

import "testing"

func TestNormalizeExtraSide(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "default empty", input: "", want: "BOTH"},
		{name: "default whitespace", input: "   ", want: "BOTH"},
		{name: "client lowercase", input: "client", want: "CLIENT"},
		{name: "server", input: "SERVER", want: "SERVER"},
		{name: "both java9", input: "both_java9", want: "BOTH_JAVA9"},
		{name: "invalid", input: "invalid", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeExtraSide(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("normalizeExtraSide(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeExtraSide(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("normalizeExtraSide(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

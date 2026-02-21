package semver

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantParts []int
		wantPre   string
	}{
		{
			name:      "plain",
			in:        "1.2.3",
			wantParts: []int{1, 2, 3},
		},
		{
			name:      "v prefix",
			in:        "v2.0.1",
			wantParts: []int{2, 0, 1},
		},
		{
			name:      "pre release suffix",
			in:        "1.2.3-beta4",
			wantParts: []int{1, 2, 3},
			wantPre:   "-beta4",
		},
		{
			name:    "non semver",
			in:      "some-mod-name",
			wantPre: "",
		},
		{
			name:    "mixed numeric non numeric",
			in:      "1.2.a",
			wantPre: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotParts, gotPre, gotRaw := Parse(tt.in)
			if !reflect.DeepEqual(gotParts, tt.wantParts) {
				t.Fatalf("parts mismatch: got=%v want=%v", gotParts, tt.wantParts)
			}
			if gotPre != tt.wantPre {
				t.Fatalf("pre mismatch: got=%q want=%q", gotPre, tt.wantPre)
			}
			if gotRaw != tt.in {
				t.Fatalf("raw mismatch: got=%q want=%q", gotRaw, tt.in)
			}
		})
	}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want int
	}{
		{
			name: "numeric less than",
			a:    "1.2.3",
			b:    "1.2.4",
			want: -1,
		},
		{
			name: "numeric greater than",
			a:    "1.10.0",
			b:    "1.2.99",
			want: 1,
		},
		{
			name: "missing patch equals zero patch",
			a:    "1.2.3",
			b:    "1.2.3.0",
			want: 0,
		},
		{
			name: "pre release is before release",
			a:    "1.2.3-rc1",
			b:    "1.2.3",
			want: -1,
		},
		{
			name: "pre release numeric suffix ordering",
			a:    "1.2.3-alpha10",
			b:    "1.2.3-alpha2",
			want: 1,
		},
		{
			name: "non semver lexical fallback",
			a:    "abc",
			b:    "def",
			want: -1,
		},
		{
			name: "non semver natural numeric ordering",
			a:    "rv3-beta-834-GTNH",
			b:    "rv3-beta-99-GTNH",
			want: 1,
		},
		{
			name: "non semver natural numeric ordering reverse",
			a:    "rv3-beta-99-GTNH",
			b:    "rv3-beta-835-GTNH",
			want: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Compare(tt.a, tt.b); got != tt.want {
				t.Fatalf("Compare(%q,%q)=%d want=%d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bare tilde", "~", home},
		{"tilde slash", "~/", home},
		{"tilde path", "~/.local/share", filepath.Join(home, ".local", "share")},
		{"empty", "", ""},
		{"absolute unchanged", "/home/ethan/.local", "/home/ethan/.local"},
		{"relative unchanged", ".local/share", ".local/share"},
		{"tilde mid-path unchanged", "foo/~/bar", "foo/~/bar"},
		{"tilde user unchanged", "~ethan/.local", "~ethan/.local"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpandTilde(tt.in)
			if got != tt.want {
				t.Errorf("ExpandTilde(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

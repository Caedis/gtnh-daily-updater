package paths

import (
	"os"
	"path/filepath"
)

// ExpandTilde expands a leading "~" (alone or followed by a path separator)
// to the current user's home directory. Shells only expand an unquoted "~",
// so a quoted argument like "~/.local/share" reaches the program literally;
// this restores the expected behavior.
//
// "~user" syntax is not supported and is returned unchanged, as is any path
// where "~" is not the leading element.
func ExpandTilde(p string) string {
	if p == "" || p[0] != '~' {
		return p
	}
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return p
	}
	if p[1] == '/' || p[1] == os.PathSeparator {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

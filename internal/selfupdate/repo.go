// Package selfupdate implements the self-update workflow: checking GitHub for
// a newer release of this tool, downloading the matching binary, verifying its
// SHA256, and atomically replacing the running executable.
package selfupdate

import (
	"fmt"
	"runtime"
)

// Repo is the GitHub repository hosting releases of this tool.
const Repo = "Caedis/gtnh-daily-updater"

// BinaryName is the platform binary name inside the release zip.
func BinaryName() string {
	if runtime.GOOS == "windows" {
		return "gtnh-daily-updater.exe"
	}
	return "gtnh-daily-updater"
}

// AssetName returns the release zip asset name for the current platform.
func AssetName(version string) string {
	return fmt.Sprintf("gtnh-daily-updater-%s-%s-%s.zip", version, runtime.GOOS, runtime.GOARCH)
}

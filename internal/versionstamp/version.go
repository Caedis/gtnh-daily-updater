// Package versionstamp writes the pack's display version into the same files
// DreamAssemblerXXL stamps when it assembles a release.
package versionstamp

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// DisplayVersion holds the version strings DAXXL stamps into pack files.
type DisplayVersion struct {
	Short string // "2.9.x (Daily 648)"
	Long  string // "2.9.x (Daily 648) - 2026-07-28"
	Date  string // "2026-07-28", empty when unknown
}

var cyclePattern = regexp.MustCompile(`^(\d+)\.(\d+)\.`)

// DevCycle turns a config version like "2.9.0-nightly-2026-07-28" into the
// cycle string DAXXL displays ("2.9.x"). Unparseable input is returned as-is.
func DevCycle(configVersion string) string {
	m := cyclePattern.FindStringSubmatch(configVersion)
	if m == nil {
		return configVersion
	}
	return m[1] + "." + m[2] + ".x"
}

// Build assembles the display version. beyondCounter marks a --latest run,
// whose mods are picked past the counted build; its count gets a "+" suffix.
func Build(configVersion, mode string, count int, lastUpdated string, beyondCounter bool) DisplayVersion {
	kind := "Daily"
	if strings.EqualFold(mode, "experimental") {
		kind = "Experimental"
	}
	countStr := strconv.Itoa(count)
	if beyondCounter {
		countStr += "+"
	}
	short := fmt.Sprintf("%s (%s %s)", DevCycle(configVersion), kind, countStr)

	date := ""
	if len(lastUpdated) >= 10 {
		date = lastUpdated[:10]
	}
	long := short
	if date != "" {
		long = short + " - " + date
	}
	return DisplayVersion{Short: short, Long: long, Date: date}
}

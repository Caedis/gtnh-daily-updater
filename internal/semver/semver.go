package semver

import (
	"strconv"
	"strings"
)

// Parse extracts numeric components from a version string.
// Handles formats like "1.2.3", "v1.2.3", "1.2.3-beta", "1.2.3.4".
// For non-semver strings, returns the raw string as a pre-release suffix
// so they sort stably via fallback comparison.
func Parse(v string) (parts []int, pre string, raw string) {
	raw = v
	v = strings.TrimPrefix(v, "v")

	// Split off pre-release suffix at first hyphen that follows a numeric segment.
	// This avoids treating "some-mod-name" as having a pre-release suffix.
	dotParts := strings.Split(v, ".")
	if len(dotParts) > 0 {
		// Check if the first dot-segment contains a hyphen after digits (e.g. "3-beta")
		// or the version looks numeric before the hyphen
		firstDot := dotParts[0]
		if idx := strings.IndexByte(firstDot, '-'); idx > 0 {
			if _, err := strconv.Atoi(firstDot[:idx]); err == nil {
				fullIdx := strings.IndexByte(v, '-')
				if fullIdx > 0 {
					pre = v[fullIdx:]
					v = v[:fullIdx]
				}
			}
		} else {
			if _, err := strconv.Atoi(firstDot); err == nil {
				if idx := strings.IndexByte(v, '-'); idx >= 0 {
					pre = v[idx:]
					v = v[:idx]
				}
			}
		}
	}

	// Parse dot-separated numeric parts
	allNumeric := true
	for _, s := range strings.Split(v, ".") {
		n, err := strconv.Atoi(s)
		if err != nil {
			allNumeric = false
			break
		}
		parts = append(parts, n)
	}

	// If the version isn't numeric at all, return no parts — it will be
	// compared by the raw string fallback in Compare.
	if !allNumeric {
		parts = nil
	}
	return
}

// Compare compares two version strings semantically.
// Returns -1 if a < b, 0 if a == b, +1 if a > b.
// Pre-release versions (with a hyphen suffix) sort before the corresponding release.
// Non-semver strings are compared with natural (numeric-aware) ordering and
// sort after all semver versions.
func Compare(a, b string) int {
	aParts, aPre, aRaw := Parse(a)
	bParts, bPre, bRaw := Parse(b)

	// If neither is a valid semver, compare with numeric-aware ordering.
	if aParts == nil && bParts == nil {
		return compareNatural(aRaw, bRaw)
	}
	// Semver sorts above non-semver
	if aParts == nil {
		return -1
	}
	if bParts == nil {
		return 1
	}

	// Compare numeric parts
	maxLen := max(len(aParts), len(bParts))
	for i := range maxLen {
		av, bv := 0, 0
		if i < len(aParts) {
			av = aParts[i]
		}
		if i < len(bParts) {
			bv = bParts[i]
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}

	// Equal numeric parts — pre-release sorts before release
	switch {
	case aPre == "" && bPre == "":
		return 0
	case aPre != "" && bPre == "":
		return -1 // a is pre-release, b is release → a < b
	case aPre == "" && bPre != "":
		return 1 // a is release, b is pre-release → a > b
	default:
		return comparePreRelease(aPre, bPre)
	}
}

// compareNatural compares strings with numeric-aware ordering.
// Example: "rv3-beta-834-GTNH" > "rv3-beta-99-GTNH".
func compareNatural(a, b string) int {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		aDigit := a[i] >= '0' && a[i] <= '9'
		bDigit := b[j] >= '0' && b[j] <= '9'
		if aDigit && bDigit {
			i2 := i
			for i2 < len(a) && a[i2] >= '0' && a[i2] <= '9' {
				i2++
			}
			j2 := j
			for j2 < len(b) && b[j2] >= '0' && b[j2] <= '9' {
				j2++
			}
			if cmp := compareNumericRuns(a[i:i2], b[j:j2]); cmp != 0 {
				return cmp
			}
			i, j = i2, j2
			continue
		}
		if a[i] != b[j] {
			if a[i] < b[j] {
				return -1
			}
			return 1
		}
		i++
		j++
	}
	switch {
	case i == len(a) && j == len(b):
		return 0
	case i == len(a):
		return -1
	default:
		return 1
	}
}

func compareNumericRuns(a, b string) int {
	a = strings.TrimLeft(a, "0")
	b = strings.TrimLeft(b, "0")
	if a == "" {
		a = "0"
	}
	if b == "" {
		b = "0"
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// comparePreRelease compares pre-release suffixes like "-alpha21" vs "-alpha3"
// by splitting each into a text prefix and an optional trailing number, so that
// numeric ordering is used when the text prefixes match.
func comparePreRelease(a, b string) int {
	aText, aNum := splitTrailingNumber(a)
	bText, bNum := splitTrailingNumber(b)

	if cmp := strings.Compare(aText, bText); cmp != 0 {
		return cmp
	}
	switch {
	case aNum < bNum:
		return -1
	case aNum > bNum:
		return 1
	default:
		return 0
	}
}

// splitTrailingNumber splits a string into a text prefix and a trailing integer.
// e.g. "-alpha21" → ("-alpha", 21), "-pre" → ("-pre", 0), "-rc1" → ("-rc", 1)
func splitTrailingNumber(s string) (string, int) {
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	if i == len(s) {
		return s, 0
	}
	n, err := strconv.Atoi(s[i:])
	if err != nil {
		return s, 0
	}
	return s[:i], n
}

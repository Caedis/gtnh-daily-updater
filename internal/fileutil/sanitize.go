package fileutil

import (
	"regexp"
	"strconv"
	"strings"
)

// percentEncoded matches a single percent-encoded byte (e.g. "%2B").
var percentEncoded = regexp.MustCompile(`%[0-9A-Fa-f]{2}`)

// winReservedNames are device names reserved on Windows in every directory,
// even with an extension (e.g. "NUL.jar" resolves to the NUL device).
var winReservedNames = func() map[string]bool {
	m := map[string]bool{"con": true, "prn": true, "aux": true, "nul": true}
	for i := 1; i <= 9; i++ {
		m["com"+strconv.Itoa(i)] = true
		m["lpt"+strconv.Itoa(i)] = true
	}
	return m
}()

// isForbiddenRune reports whether r may not appear in a filename on Windows
// (the strictest of the three target platforms). This is the union of the
// reserved punctuation and all control characters; designing to it keeps names
// valid on Linux and macOS too. See internal/fileutil docs / Microsoft's
// "Naming Files, Paths, and Namespaces".
func isForbiddenRune(r rune) bool {
	if r < 0x20 {
		return true
	}
	switch r {
	case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
		return true
	}
	return false
}

// SanitizeFilename makes a filename safe to write on Windows, macOS, and Linux
// while preserving the characters those platforms actually allow. It:
//
//  1. decodes percent-encoded bytes (e.g. "%2B" -> "+"), since an undecoded
//     sequence means the name was never URL-decoded upstream;
//  2. replaces Windows-forbidden characters (< > : " / \ | ? * and control
//     chars) with "_";
//  3. trims surrounding whitespace and strips trailing dots/spaces, which
//     Windows disallows;
//  4. prefixes "_" to Windows reserved device names (CON, NUL, COM1-9, ...).
//
// Characters that are legal on all three platforms — including spaces,
// brackets, parentheses, apostrophes, +, -, ., _ — are kept as-is.
//
// Changing these rules is safe: the updater records each mod's canonical
// (raw) filename and re-sanitizes on scan, renaming an existing jar to the new
// form instead of re-downloading a duplicate. See FilenameVariants.
func SanitizeFilename(s string) string {
	// 1. Decode percent-encoded bytes before sanitizing, so a decoded byte that
	// happens to be forbidden (e.g. %2F -> '/') is still caught below.
	s = percentEncoded.ReplaceAllStringFunc(s, func(m string) string {
		b, err := strconv.ParseUint(m[1:], 16, 8)
		if err != nil {
			return m
		}
		return string(rune(b))
	})

	// 2. Replace forbidden characters.
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isForbiddenRune(r) {
			b.WriteByte('_')
		} else {
			b.WriteRune(r)
		}
	}

	// 3. Trim surrounding whitespace, then any trailing dots/spaces.
	out := strings.TrimSpace(b.String())
	out = strings.TrimRight(out, " .")

	if out == "" {
		return "_"
	}

	// 4. Guard Windows reserved device names (matched on the base name,
	// case-insensitively).
	base := out
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	if winReservedNames[strings.ToLower(base)] {
		out = "_" + out
	}

	return out
}

// legacySanitizeFilename reproduces the original, stricter sanitization rule
// (allow only [A-Za-z0-9._+-], spaces -> hyphens, drop everything else). It is
// retained solely so FilenameVariants can recognize jars written to disk by
// older releases and migrate them, rather than re-downloading duplicates.
func legacySanitizeFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		case r == '-' || r == '.' || r == '_' || r == '+':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		default:
			// drop
		}
	}
	return b.String()
}

// FilenameVariants returns every on-disk name a canonical (raw) filename may
// have been written as: the raw name itself, the current sanitized form, and
// known historical sanitized forms. The reverse filename index keys on all of
// them so a jar written by any past sanitization rule still resolves to its
// mod. Duplicates and empty results are omitted; the raw name is always first.
func FilenameVariants(raw string) []string {
	out := []string{raw}
	seen := map[string]bool{raw: true}
	for _, v := range []string{SanitizeFilename(raw), legacySanitizeFilename(raw)} {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

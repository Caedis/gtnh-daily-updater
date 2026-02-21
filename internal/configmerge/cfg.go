package configmerge

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"
)

// CfgFile represents a parsed Forge .cfg file.
type CfgFile struct {
	// Preamble holds lines before the first category (comments, blank lines)
	Preamble []string
	// Categories in order of appearance
	Categories []CfgCategory
}

type CfgCategory struct {
	// Header comment lines immediately before the category
	Comments []string
	// Full category name path (e.g. "general" or "category.subcategory")
	Name string
	// Entries in order of appearance
	Entries []CfgEntry
}

type CfgEntry struct {
	// Comment lines immediately before this entry
	Comments []string
	// The raw line (e.g. "S:key=value" or "I:count=42")
	Key   string // type prefix + key name (e.g. "S:key")
	Value string
}

// ParseCfg parses a Forge .cfg file from a reader.
func ParseCfg(r io.Reader) (*CfgFile, error) {
	scanner := bufio.NewScanner(r)
	cfg := &CfgFile{}

	var currentCategory *CfgCategory
	var pendingComments []string
	depth := 0

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if currentCategory == nil {
			// Before first category
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				if depth == 0 {
					// Could be preamble or start of category comments
					pendingComments = append(pendingComments, line)
				}
				continue
			}

			if strings.HasSuffix(trimmed, "{") {
				name := strings.TrimSpace(strings.TrimSuffix(trimmed, "{"))
				name = strings.Trim(name, "\"")
				cat := CfgCategory{
					Name:     name,
					Comments: pendingComments,
				}
				pendingComments = nil
				if depth == 0 {
					cfg.Categories = append(cfg.Categories, cat)
					currentCategory = &cfg.Categories[len(cfg.Categories)-1]
				}
				depth++
				continue
			}

			// Anything else in preamble
			cfg.Preamble = append(cfg.Preamble, pendingComments...)
			cfg.Preamble = append(cfg.Preamble, line)
			pendingComments = nil
			continue
		}

		// Inside a category
		if trimmed == "}" {
			depth--
			if depth == 0 {
				currentCategory = nil
			}
			continue
		}

		if strings.HasSuffix(trimmed, "{") {
			// Nested category - track depth but flatten entries
			depth++
			continue
		}

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			pendingComments = append(pendingComments, line)
			continue
		}

		// Parse key=value
		if eqIdx := strings.Index(trimmed, "="); eqIdx >= 0 {
			key := trimmed[:eqIdx]
			value := trimmed[eqIdx+1:]
			entry := CfgEntry{
				Comments: pendingComments,
				Key:      key,
				Value:    value,
			}
			pendingComments = nil
			currentCategory.Entries = append(currentCategory.Entries, entry)
		} else {
			// Non key=value line inside category, treat as comment
			pendingComments = append(pendingComments, line)
		}
	}

	return cfg, scanner.Err()
}

// ToMap converts a CfgFile into a flat map of category.key → value.
func (cfg *CfgFile) ToMap() map[string]string {
	m := make(map[string]string)
	for _, cat := range cfg.Categories {
		for _, e := range cat.Entries {
			m[cat.Name+"."+e.Key] = e.Value
		}
	}
	return m
}

// MergeCfg performs a 3-way merge of Forge .cfg files.
// base is the original pack version, theirs is the new pack version, ours is the user's version.
// Returns the merged content and a list of conflicts.
func MergeCfg(base, theirs, ours []byte) ([]byte, []string) {
	baseCfg, err1 := ParseCfg(strings.NewReader(string(base)))
	theirsCfg, err2 := ParseCfg(strings.NewReader(string(theirs)))
	oursCfg, err3 := ParseCfg(strings.NewReader(string(ours)))

	if err1 != nil || err2 != nil || err3 != nil {
		// Fall back to text merge if parsing fails
		return MergeText(base, theirs, ours)
	}

	baseMap := baseCfg.ToMap()
	theirsMap := theirsCfg.ToMap()
	oursMap := oursCfg.ToMap()

	var conflicts []string

	// Build the merged result based on the "theirs" structure (new pack layout)
	// but applying user changes where appropriate
	result := *theirsCfg

	for ci := range result.Categories {
		cat := &result.Categories[ci]
		for ei := range cat.Entries {
			entry := &cat.Entries[ei]
			fullKey := cat.Name + "." + entry.Key

			baseVal, inBase := baseMap[fullKey]
			theirsVal := theirsMap[fullKey]
			oursVal, inOurs := oursMap[fullKey]

			if !inBase {
				// New key in pack, keep pack value
				continue
			}

			if !inOurs {
				// User deleted this key? Keep pack value
				continue
			}

			userChanged := oursVal != baseVal
			packChanged := theirsVal != baseVal

			if userChanged && packChanged && oursVal != theirsVal {
				// Both changed - user wins, but log conflict
				entry.Value = oursVal
				conflicts = append(conflicts, fmt.Sprintf("conflict: %s (user: %s, pack: %s)", fullKey, oursVal, theirsVal))
			} else if userChanged {
				// Only user changed - user wins
				entry.Value = oursVal
			}
			// If only pack changed or neither changed, keep theirs value (already set)
		}
	}

	// Write the merged result
	var buf strings.Builder
	writeCfg(&buf, &result)

	return []byte(buf.String()), conflicts
}

func writeCfg(w *strings.Builder, cfg *CfgFile) {
	for _, line := range cfg.Preamble {
		w.WriteString(line)
		w.WriteString("\n")
	}

	// Sort categories for consistent output
	cats := make([]CfgCategory, len(cfg.Categories))
	copy(cats, cfg.Categories)
	sort.Slice(cats, func(i, j int) bool {
		return cats[i].Name < cats[j].Name
	})

	for _, cat := range cats {
		for _, line := range cat.Comments {
			w.WriteString(line)
			w.WriteString("\n")
		}
		w.WriteString(cat.Name)
		w.WriteString(" {\n")
		for _, e := range cat.Entries {
			for _, line := range e.Comments {
				w.WriteString(line)
				w.WriteString("\n")
			}
			w.WriteString("    ")
			w.WriteString(e.Key)
			w.WriteString("=")
			w.WriteString(e.Value)
			w.WriteString("\n")
		}
		w.WriteString("}\n\n")
	}
}

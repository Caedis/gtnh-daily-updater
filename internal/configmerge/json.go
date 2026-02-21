package configmerge

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// MergeJSON performs a 3-way merge of JSON config files.
// base is the original pack version, theirs is the new pack version, ours is the user's version.
// Returns the merged JSON and a list of conflicts.
func MergeJSON(base, theirs, ours []byte) ([]byte, []string) {
	var baseObj, theirsObj, oursObj interface{}

	if err := json.Unmarshal(base, &baseObj); err != nil {
		return MergeText(base, theirs, ours)
	}
	if err := json.Unmarshal(theirs, &theirsObj); err != nil {
		return MergeText(base, theirs, ours)
	}
	if err := json.Unmarshal(ours, &oursObj); err != nil {
		return MergeText(base, theirs, ours)
	}

	var conflicts []string
	merged := mergeValue("", baseObj, theirsObj, oursObj, &conflicts)

	result, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return MergeText(base, theirs, ours)
	}

	result = append(result, '\n')
	return result, conflicts
}

func mergeValue(path string, base, theirs, ours interface{}, conflicts *[]string) interface{} {
	// If base, theirs, ours are all maps, do deep merge
	baseMap, baseIsMap := base.(map[string]interface{})
	theirsMap, theirsIsMap := theirs.(map[string]interface{})
	oursMap, oursIsMap := ours.(map[string]interface{})

	if baseIsMap && theirsIsMap && oursIsMap {
		return mergeObjects(path, baseMap, theirsMap, oursMap, conflicts)
	}

	// For non-object values, use 3-way merge logic
	userChanged := !reflect.DeepEqual(base, ours)
	packChanged := !reflect.DeepEqual(base, theirs)

	if userChanged && packChanged && !reflect.DeepEqual(theirs, ours) {
		// Both changed differently - user wins
		*conflicts = append(*conflicts, fmt.Sprintf("conflict at %s: user and pack both changed", path))
		return ours
	}
	if userChanged {
		return ours
	}
	return theirs
}

func mergeObjects(path string, base, theirs, ours map[string]interface{}, conflicts *[]string) map[string]interface{} {
	result := make(map[string]interface{})

	// Collect all keys
	allKeys := make(map[string]bool)
	for k := range base {
		allKeys[k] = true
	}
	for k := range theirs {
		allKeys[k] = true
	}
	for k := range ours {
		allKeys[k] = true
	}

	for k := range allKeys {
		keyPath := path + "." + k
		if path == "" {
			keyPath = k
		}

		baseVal, inBase := base[k]
		theirsVal, inTheirs := theirs[k]
		oursVal, inOurs := ours[k]

		switch {
		case inBase && inTheirs && inOurs:
			// Key exists in all three - merge recursively
			result[k] = mergeValue(keyPath, baseVal, theirsVal, oursVal, conflicts)

		case !inBase && inTheirs && !inOurs:
			// New key added by pack only
			result[k] = theirsVal

		case !inBase && !inTheirs && inOurs:
			// New key added by user only
			result[k] = oursVal

		case !inBase && inTheirs && inOurs:
			// Both added - merge
			result[k] = mergeValue(keyPath, nil, theirsVal, oursVal, conflicts)

		case inBase && !inTheirs && inOurs:
			// Pack removed, user kept - keep user's (user wins)
			if !reflect.DeepEqual(baseVal, oursVal) {
				result[k] = oursVal
			}
			// If user didn't change it and pack removed it, remove it

		case inBase && inTheirs && !inOurs:
			// User removed, pack kept - respect user's removal
			// unless pack changed the value
			if !reflect.DeepEqual(baseVal, theirsVal) {
				result[k] = theirsVal
				*conflicts = append(*conflicts, fmt.Sprintf("conflict at %s: user removed but pack changed", keyPath))
			}

		case inBase && !inTheirs && !inOurs:
			// Both removed - omit

		default:
			if inTheirs {
				result[k] = theirsVal
			} else if inOurs {
				result[k] = oursVal
			}
		}
	}

	return result
}

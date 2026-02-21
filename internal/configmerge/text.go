package configmerge

import (
	"fmt"
	"strings"
)

// MergeText performs a simple line-based 3-way merge.
// When both sides change the same region, user's version wins and the conflict is reported.
func MergeText(base, theirs, ours []byte) ([]byte, []string) {
	baseLines := splitLines(string(base))
	theirsLines := splitLines(string(theirs))
	oursLines := splitLines(string(ours))

	// Simple approach: if ours == base, take theirs; if theirs == base, take ours;
	// if both changed, try line-by-line merge
	if linesEqual(oursLines, baseLines) {
		return theirs, nil
	}
	if linesEqual(theirsLines, baseLines) {
		return ours, nil
	}
	if linesEqual(oursLines, theirsLines) {
		return ours, nil
	}

	// Both changed - attempt line-level merge using LCS
	merged, conflicts := mergeLines(baseLines, theirsLines, oursLines)
	return []byte(strings.Join(merged, "\n")), conflicts
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	// Remove trailing empty string from final newline
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func linesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// mergeLines does a simple line-level 3-way merge.
// It walks through the base and applies non-conflicting changes from theirs and ours.
func mergeLines(base, theirs, ours []string) ([]string, []string) {
	// Compute edit scripts from base to theirs and base to ours
	theirsDiff := diffLines(base, theirs)
	oursDiff := diffLines(base, ours)

	var result []string
	var conflicts []string

	bi := 0 // base index
	ti := 0 // theirs diff index
	oi := 0 // ours diff index

	for bi < len(base) || ti < len(theirsDiff) || oi < len(oursDiff) {
		// Find if there's a change at current base position
		var theirsEdit *edit
		var oursEdit *edit

		if ti < len(theirsDiff) && theirsDiff[ti].baseStart <= bi {
			theirsEdit = &theirsDiff[ti]
		}
		if oi < len(oursDiff) && oursDiff[oi].baseStart <= bi {
			oursEdit = &oursDiff[oi]
		}

		if theirsEdit != nil && oursEdit != nil {
			// Both have edits at this point
			if linesEqual(theirsEdit.newLines, oursEdit.newLines) {
				// Same change - apply once
				result = append(result, oursEdit.newLines...)
			} else {
				// Conflict - user wins
				result = append(result, oursEdit.newLines...)
				conflicts = append(conflicts, "line-level conflict near line "+fmt.Sprintf("%d", bi+1))
			}
			bi = max(theirsEdit.baseEnd, oursEdit.baseEnd)
			ti++
			oi++
		} else if theirsEdit != nil {
			result = append(result, theirsEdit.newLines...)
			bi = theirsEdit.baseEnd
			ti++
		} else if oursEdit != nil {
			result = append(result, oursEdit.newLines...)
			bi = oursEdit.baseEnd
			oi++
		} else {
			if bi < len(base) {
				result = append(result, base[bi])
				bi++
			} else {
				break
			}
		}
	}

	return result, conflicts
}

type edit struct {
	baseStart int
	baseEnd   int
	newLines  []string
}

// diffLines computes a simple list of edits from a to b using a basic approach.
// This is a simplified diff that groups consecutive changes.
func diffLines(a, b []string) []edit {
	// Use LCS to find matching lines
	lcs := computeLCS(a, b)

	var edits []edit
	ai, bi, li := 0, 0, 0

	for li < len(lcs) {
		// Find where the next LCS match is in a and b
		matchA := -1
		for i := ai; i < len(a); i++ {
			if a[i] == lcs[li] {
				matchA = i
				break
			}
		}
		matchB := -1
		for i := bi; i < len(b); i++ {
			if b[i] == lcs[li] {
				matchB = i
				break
			}
		}

		if matchA < 0 || matchB < 0 {
			break
		}

		// If there are changes before this match, record them
		if ai < matchA || bi < matchB {
			e := edit{
				baseStart: ai,
				baseEnd:   matchA,
				newLines:  b[bi:matchB],
			}
			edits = append(edits, e)
		}

		ai = matchA + 1
		bi = matchB + 1
		li++
	}

	// Handle remaining lines after last LCS match
	if ai < len(a) || bi < len(b) {
		e := edit{
			baseStart: ai,
			baseEnd:   len(a),
			newLines:  b[bi:],
		}
		edits = append(edits, e)
	}

	return edits
}

// computeLCS returns the longest common subsequence of a and b.
func computeLCS(a, b []string) []string {
	m, n := len(a), len(b)
	if m == 0 || n == 0 {
		return nil
	}

	// DP table
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}

	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else {
				dp[i][j] = max(dp[i-1][j], dp[i][j-1])
			}
		}
	}

	// Backtrack to find LCS
	lcs := make([]string, 0, dp[m][n])
	i, j := m, n
	for i > 0 && j > 0 {
		if a[i-1] == b[j-1] {
			lcs = append(lcs, a[i-1])
			i--
			j--
		} else if dp[i-1][j] > dp[i][j-1] {
			i--
		} else {
			j--
		}
	}

	// Reverse
	for left, right := 0, len(lcs)-1; left < right; left, right = left+1, right-1 {
		lcs[left], lcs[right] = lcs[right], lcs[left]
	}

	return lcs
}


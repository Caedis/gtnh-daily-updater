package versionstamp

import "strings"

// lineFile is a text file split into lines, remembering each line's own
// terminator so a rewrite leaves everything it does not touch byte-identical
// even when a file mixes CRLF and bare-LF endings.
//
// eols[i] is the terminator that follows lines[i]: "\n", "\r\n", or "" if
// lines[i] is the last line and the file has no trailing newline.
type lineFile struct {
	lines []string
	eols  []string
}

func parseLines(content string) lineFile {
	if content == "" {
		return lineFile{}
	}
	chunks := strings.SplitAfter(content, "\n")
	// content ending in "\n" produces a trailing empty chunk; drop it.
	if n := len(chunks); n > 0 && chunks[n-1] == "" {
		chunks = chunks[:n-1]
	}
	f := lineFile{lines: make([]string, len(chunks)), eols: make([]string, len(chunks))}
	for i, c := range chunks {
		switch {
		case strings.HasSuffix(c, "\r\n"):
			f.lines[i] = c[:len(c)-2]
			f.eols[i] = "\r\n"
		case strings.HasSuffix(c, "\n"):
			f.lines[i] = c[:len(c)-1]
			f.eols[i] = "\n"
		default:
			f.lines[i] = c
			f.eols[i] = ""
		}
	}
	return f
}

func (f lineFile) render() string {
	var b strings.Builder
	for i, l := range f.lines {
		b.WriteString(l)
		b.WriteString(f.eols[i])
	}
	return b.String()
}

// findKey returns the index of the first line starting with key (ignoring
// leading whitespace) and the value after it. Index is -1 when absent.
func (f lineFile) findKey(key string) (int, string) {
	for i, l := range f.lines {
		t := strings.TrimLeft(l, " \t")
		if strings.HasPrefix(t, key) {
			return i, strings.TrimPrefix(t, key)
		}
	}
	return -1, ""
}

// setKey rewrites line i as key+value, keeping the line's original indent and
// terminator. i must be a valid index into f.lines (i.e. not the -1 sentinel
// findKey returns for "not found"); callers are expected to check that first.
func (f *lineFile) setKey(i int, key, value string) {
	line := f.lines[i]
	indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	f.lines[i] = indent + key + value
}

// dominantEOL returns the file's most common line terminator, defaulting to
// "\n" when there is no majority (including empty/single-line files).
func (f lineFile) dominantEOL() string {
	crlf, lf := 0, 0
	for _, e := range f.eols {
		switch e {
		case "\r\n":
			crlf++
		case "\n":
			lf++
		}
	}
	if crlf > lf {
		return "\r\n"
	}
	return "\n"
}

// appendLine adds a new line at the end of the file, terminated with a
// newline in the file's dominant style. If the previous last line had no
// trailing newline, one is added so the new line starts on its own line.
func (f *lineFile) appendLine(line string) {
	eol := f.dominantEOL()
	if n := len(f.eols); n > 0 && f.eols[n-1] == "" {
		f.eols[n-1] = eol
	}
	f.lines = append(f.lines, line)
	f.eols = append(f.eols, eol)
}

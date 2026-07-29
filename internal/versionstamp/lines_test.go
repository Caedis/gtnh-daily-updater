package versionstamp

import "testing"

func TestParseLinesRoundTrip(t *testing.T) {
	tests := []string{
		"",
		"a",
		"a\n",
		"a\nb\n",
		"a\r\nb\r\n",
		"a\r\nb",
		"# comment\n\nkey=value\n",
		"a\r\nb\n",
		"a\r",
		"\n",
	}
	for _, in := range tests {
		if got := parseLines(in).render(); got != in {
			t.Errorf("round trip of %q = %q", in, got)
		}
	}
}

func TestFindKey(t *testing.T) {
	f := parseLines("# c\nfoo=1\n    S:ModPackVersion=2.9.0\nbar=2\n")

	i, val := f.findKey("foo=")
	if i != 1 || val != "1" {
		t.Errorf("findKey(foo=) = (%d, %q), want (1, \"1\")", i, val)
	}

	i, val = f.findKey("S:ModPackVersion=")
	if i != 2 || val != "2.9.0" {
		t.Errorf("findKey(S:ModPackVersion=) = (%d, %q), want (2, \"2.9.0\")", i, val)
	}

	if i, _ = f.findKey("missing="); i != -1 {
		t.Errorf("findKey(missing=) = %d, want -1", i)
	}
}

func TestSetKeyPreservesIndentAndOtherLines(t *testing.T) {
	f := parseLines("# c\n    S:ModPackVersion=old\nbar=2\n")
	i, _ := f.findKey("S:ModPackVersion=")
	f.setKey(i, "S:ModPackVersion=", "new")

	want := "# c\n    S:ModPackVersion=new\nbar=2\n"
	if got := f.render(); got != want {
		t.Errorf("render() = %q, want %q", got, want)
	}
}

func TestSetKeyOnCRLFFilePreservesCRLF(t *testing.T) {
	f := parseLines("motd=old\r\nport=25565\r\n")
	i, _ := f.findKey("motd=")
	f.setKey(i, "motd=", "new")

	want := "motd=new\r\nport=25565\r\n"
	if got := f.render(); got != want {
		t.Errorf("render() = %q, want %q", got, want)
	}
}

func TestSetKeyOnMixedEndingsPreservesEachLine(t *testing.T) {
	f := parseLines("motd=old\r\nport=25565\n")
	i, _ := f.findKey("motd=")
	f.setKey(i, "motd=", "new")

	want := "motd=new\r\nport=25565\n"
	if got := f.render(); got != want {
		t.Errorf("render() = %q, want %q", got, want)
	}
}

func TestAppendLineMatchesDominantStyle(t *testing.T) {
	f := parseLines("a\r\nb\r\n")
	f.appendLine("c")

	want := "a\r\nb\r\nc\r\n"
	if got := f.render(); got != want {
		t.Errorf("render() = %q, want %q", got, want)
	}
}

func TestAppendLineOnNoTrailingNewlineAddsOne(t *testing.T) {
	f := parseLines("a")
	f.appendLine("b")

	want := "a\nb\n"
	if got := f.render(); got != want {
		t.Errorf("render() = %q, want %q", got, want)
	}
}

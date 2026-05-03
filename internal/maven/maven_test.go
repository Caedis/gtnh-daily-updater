package maven

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLatestStableVersion(t *testing.T) {
	tests := []struct {
		name     string
		versions []string
		release  string
		want     string
	}{
		{
			name:     "numeric aware ordering for non semver tags",
			versions: []string{"rv3-beta-99-GTNH", "rv3-beta-834-GTNH", "rv3-beta-835-GTNH"},
			want:     "rv3-beta-835-GTNH",
		},
		{
			name:     "filters pre release suffix",
			versions: []string{"2.0.0-pre", "1.9.9", "2.0.0"},
			want:     "2.0.0",
		},
		{
			name:     "filters pre release suffix case insensitive",
			versions: []string{"2.0.0-PRE", "2.0.0-rc1", "1.0.0"},
			want:     "2.0.0-rc1",
		},
		{
			name:     "release tag considered when newer",
			versions: []string{"1.0.0", "1.1.0"},
			release:  "1.2.0",
			want:     "1.2.0",
		},
		{
			name:     "release pre tag ignored",
			versions: []string{"1.0.0"},
			release:  "2.0.0-pre",
			want:     "1.0.0",
		},
		{
			name:     "no stable versions",
			versions: []string{"1.0.0-pre", "2.0.0-pre"},
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := latestStableVersion(tt.versions, tt.release)
			if got != tt.want {
				t.Fatalf("latestStableVersion()=%q want=%q", got, tt.want)
			}
		})
	}
}

func TestMetadataURL(t *testing.T) {
	t.Run("escapes spaces", func(t *testing.T) {
		got := MetadataURL("My Mod")
		if !strings.Contains(got, "/My%20Mod/maven-metadata.xml") {
			t.Fatalf("unexpected metadata URL: %s", got)
		}
	})

	t.Run("escapes slashes", func(t *testing.T) {
		got := MetadataURL("mod/name")
		if !strings.Contains(got, "/mod%2Fname/maven-metadata.xml") {
			t.Fatalf("unexpected metadata URL: %s", got)
		}
	})
}

func TestFetchMetadata(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
<metadata>
  <versioning>
    <release>1.2.0</release>
    <versions>
      <version>1.0.0</version>
      <version>1.2.0</version>
    </versions>
  </versioning>
</metadata>`))
	}))
	defer server.Close()

	oldClient := HTTPClient
	HTTPClient = server.Client()
	t.Cleanup(func() { HTTPClient = oldClient })

	md, err := fetchMetadata(context.Background(), server.URL+"/maven-metadata.xml")
	if err != nil {
		t.Fatalf("fetchMetadata failed: %v", err)
	}
	if md.Versioning.Release != "1.2.0" {
		t.Fatalf("release=%q want=1.2.0", md.Versioning.Release)
	}
	if len(md.Versioning.Versions.Version) != 2 {
		t.Fatalf("versions=%d want=2", len(md.Versioning.Versions.Version))
	}
}

func TestDownloadURL(t *testing.T) {
	url, filename := DownloadURL("My Mod", "1.2.3")
	if filename != "My-Mod-1.2.3.jar" {
		t.Fatalf("filename=%q want=My-Mod-1.2.3.jar", filename)
	}
	if !strings.Contains(url, "/My%20Mod/1.2.3/My-Mod-1.2.3.jar") {
		t.Fatalf("unexpected url: %s", url)
	}
}

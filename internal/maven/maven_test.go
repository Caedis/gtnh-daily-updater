package maven

import (
	"context"
	"fmt"
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

func TestSelectGroup(t *testing.T) {
	tests := []struct {
		name    string
		items   []searchItem
		want    string
		wantErr bool
	}{
		{
			name:  "single gtnh group",
			items: []searchItem{{Group: "com.github.GTNewHorizons", Version: "1.0.0"}},
			want:  "com.github.GTNewHorizons",
		},
		{
			name:  "single non-gtnh group",
			items: []searchItem{{Group: "tuhljin.automagy", Version: "0.29.7-GTNH"}},
			want:  "tuhljin.automagy",
		},
		{
			name: "prefers gtnh when multiple groups present",
			items: []searchItem{
				{Group: "tuhljin.automagy", Version: "9.9.9"},
				{Group: "com.github.GTNewHorizons", Version: "0.0.1"},
			},
			want: "com.github.GTNewHorizons",
		},
		{
			name: "no gtnh picks newest version group",
			items: []searchItem{
				{Group: "old.group", Version: "1.0.0"},
				{Group: "new.group", Version: "2.0.0"},
			},
			want: "new.group",
		},
		{
			name:    "empty items errors",
			items:   nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectGroup(tt.items, "Mod")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got group=%q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("selectGroup error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("selectGroup=%q want=%q", got, tt.want)
			}
		})
	}
}

func TestMetadataURL(t *testing.T) {
	t.Run("gtnh group path and escaping", func(t *testing.T) {
		got := metadataURL("com.github.GTNewHorizons", "My Mod")
		if !strings.Contains(got, "/com/github/GTNewHorizons/My%20Mod/maven-metadata.xml") {
			t.Fatalf("unexpected metadata URL: %s", got)
		}
	})
	t.Run("non-gtnh group path", func(t *testing.T) {
		got := metadataURL("tuhljin.automagy", "Automagy-GTNH")
		if !strings.Contains(got, "/tuhljin/automagy/Automagy-GTNH/maven-metadata.xml") {
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
	url, filename := downloadURL("tuhljin.automagy", "My Mod", "1.2.3")
	if filename != "My Mod-1.2.3.jar" {
		t.Fatalf("filename=%q want=%q", filename, "My Mod-1.2.3.jar")
	}
	if !strings.Contains(url, "/tuhljin/automagy/My%20Mod/1.2.3/My%20Mod-1.2.3.jar") {
		t.Fatalf("unexpected url: %s", url)
	}
}

func TestResolveGroup(t *testing.T) {
	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Query().Get("maven.artifactId") != "Automagy-GTNH" {
			t.Fatalf("unexpected artifactId: %s", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items":[{"group":"tuhljin.automagy","version":"0.29.7-GTNH"}]}`))
	}))
	defer server.Close()

	oldClient, oldBase := HTTPClient, searchBase
	HTTPClient = server.Client()
	searchBase = server.URL
	groupCacheMu.Lock()
	groupCache = map[string]string{}
	groupCacheMu.Unlock()
	t.Cleanup(func() { HTTPClient, searchBase = oldClient, oldBase })

	got, err := ResolveGroup(context.Background(), "Automagy-GTNH")
	if err != nil {
		t.Fatalf("ResolveGroup error: %v", err)
	}
	if got != "tuhljin.automagy" {
		t.Fatalf("group=%q want=tuhljin.automagy", got)
	}
	// second call served from cache, no extra HTTP hit
	if _, err := ResolveGroup(context.Background(), "Automagy-GTNH"); err != nil {
		t.Fatalf("second ResolveGroup error: %v", err)
	}
	if hits != 1 {
		t.Fatalf("server hits=%d want=1 (cache miss)", hits)
	}
}

func TestFetchSHA256(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok.jar.sha256":
			fmt.Fprint(w, "ABCDEF0123  ok.jar\n")
		case "/bare.jar.sha256":
			fmt.Fprint(w, "deadbeef\n")
		case "/missing.jar.sha256":
			http.NotFound(w, r)
		default:
			http.Error(w, "unexpected", 500)
		}
	}))
	defer srv.Close()
	oldClient := HTTPClient
	HTTPClient = srv.Client()
	defer func() { HTTPClient = oldClient }()

	got, err := FetchSHA256(context.Background(), srv.URL+"/ok.jar")
	if err != nil {
		t.Fatal(err)
	}
	if got != "abcdef0123" {
		t.Fatalf("ok = %q, want abcdef0123", got)
	}

	got, err = FetchSHA256(context.Background(), srv.URL+"/bare.jar")
	if err != nil || got != "deadbeef" {
		t.Fatalf("bare = %q err=%v", got, err)
	}

	got, err = FetchSHA256(context.Background(), srv.URL+"/missing.jar")
	if err != nil {
		t.Fatalf("missing should not error, got %v", err)
	}
	if got != "" {
		t.Fatalf("missing = %q, want \"\"", got)
	}
}

package modrinth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseSource(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantProject string
		wantVersion string
		wantErr     bool
	}{
		{name: "slug only", input: "journeymap", wantProject: "journeymap"},
		{name: "id only", input: "AANobbMI", wantProject: "AANobbMI"},
		{name: "slug and version", input: "journeymap/abc123", wantProject: "journeymap", wantVersion: "abc123"},
		{name: "empty", input: "", wantErr: true},
		{name: "trailing slash", input: "journeymap/", wantErr: true},
		{name: "leading slash", input: "/abc", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proj, ver, err := ParseSource(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseSource(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSource(%q) unexpected error: %v", tt.input, err)
			}
			if proj != tt.wantProject || ver != tt.wantVersion {
				t.Fatalf("ParseSource(%q) = (%q, %q), want (%q, %q)", tt.input, proj, ver, tt.wantProject, tt.wantVersion)
			}
		})
	}
}

func TestFetchLatestVersionPrefersRelease(t *testing.T) {
	versions := []Version{
		{ID: "v1", VersionType: "beta", Files: []File{{URL: "https://example.com/b.jar", Filename: "b.jar", Primary: true}}},
		{ID: "v2", VersionType: "release", Files: []File{{URL: "https://example.com/r.jar", Filename: "r.jar", Primary: true}}},
		{ID: "v3", VersionType: "release", Files: []File{{URL: "https://example.com/r2.jar", Filename: "r2.jar", Primary: true}}},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/v2/project/") {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		if r.Header.Get("User-Agent") == "" {
			http.Error(w, "missing UA", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(versions); err != nil {
			http.Error(w, "encode error", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	origBase := baseURL
	origClient := httpClient
	baseURL = srv.URL
	httpClient = srv.Client()
	defer func() {
		baseURL = origBase
		httpClient = origClient
	}()

	got, err := FetchLatestVersion(context.Background(), "journeymap", "1.7.10", "forge")
	if err != nil {
		t.Fatalf("FetchLatestVersion: %v", err)
	}
	if got.ID != "v2" {
		t.Errorf("FetchLatestVersion picked %q, want v2 (first release)", got.ID)
	}
}

func TestFetchLatestVersionFallsBackWhenNoRelease(t *testing.T) {
	versions := []Version{
		{ID: "v1", VersionType: "beta", Files: []File{{URL: "https://example.com/b.jar", Filename: "b.jar", Primary: true}}},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(versions)
	}))
	defer srv.Close()

	origBase := baseURL
	origClient := httpClient
	baseURL = srv.URL
	httpClient = srv.Client()
	defer func() {
		baseURL = origBase
		httpClient = origClient
	}()

	got, err := FetchLatestVersion(context.Background(), "journeymap", "1.7.10", "forge")
	if err != nil {
		t.Fatalf("FetchLatestVersion: %v", err)
	}
	if got.ID != "v1" {
		t.Errorf("FetchLatestVersion got %q, want v1", got.ID)
	}
}

func TestFetchLatestVersionErrorsOnEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]Version{})
	}))
	defer srv.Close()

	origBase := baseURL
	origClient := httpClient
	baseURL = srv.URL
	httpClient = srv.Client()
	defer func() {
		baseURL = origBase
		httpClient = origClient
	}()

	if _, err := FetchLatestVersion(context.Background(), "journeymap", "1.7.10", "forge"); err == nil {
		t.Fatal("expected error for empty versions, got nil")
	}
}

func TestFetchVersionReturnsPinned(t *testing.T) {
	want := Version{
		ID:            "pin123",
		VersionNumber: "1.2.3",
		VersionType:   "release",
		Files:         []File{{URL: "https://example.com/p.jar", Filename: "p.jar", Primary: true}},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/v2/version/pin123") {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	origBase := baseURL
	origClient := httpClient
	baseURL = srv.URL
	httpClient = srv.Client()
	defer func() {
		baseURL = origBase
		httpClient = origClient
	}()

	got, err := FetchVersion(context.Background(), "pin123")
	if err != nil {
		t.Fatalf("FetchVersion: %v", err)
	}
	if got.ID != want.ID || got.VersionNumber != want.VersionNumber {
		t.Errorf("FetchVersion got {ID:%q Ver:%q}, want {ID:%q Ver:%q}", got.ID, got.VersionNumber, want.ID, want.VersionNumber)
	}
}

func TestPrimaryFile(t *testing.T) {
	v := Version{ID: "x", Files: []File{
		{URL: "a", Filename: "a.jar"},
		{URL: "b", Filename: "b.jar", Primary: true},
	}}
	f, err := PrimaryFile(v)
	if err != nil {
		t.Fatalf("PrimaryFile: %v", err)
	}
	if f.Filename != "b.jar" {
		t.Errorf("PrimaryFile picked %q, want b.jar", f.Filename)
	}

	// No primary flag — first file
	v2 := Version{ID: "x", Files: []File{{URL: "a", Filename: "a.jar"}}}
	f, err = PrimaryFile(v2)
	if err != nil {
		t.Fatalf("PrimaryFile: %v", err)
	}
	if f.Filename != "a.jar" {
		t.Errorf("PrimaryFile got %q, want a.jar", f.Filename)
	}

	// Empty files
	if _, err := PrimaryFile(Version{ID: "x"}); err == nil {
		t.Error("PrimaryFile expected error for no files")
	}
}

func TestProjectExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/found") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	origBase := baseURL
	origClient := httpClient
	baseURL = srv.URL
	httpClient = srv.Client()
	defer func() {
		baseURL = origBase
		httpClient = origClient
	}()

	if ok, err := ProjectExists(context.Background(), "found"); err != nil || !ok {
		t.Errorf("ProjectExists(found) = (%v, %v), want (true, nil)", ok, err)
	}
	if ok, err := ProjectExists(context.Background(), "missing"); err != nil || ok {
		t.Errorf("ProjectExists(missing) = (%v, %v), want (false, nil)", ok, err)
	}
}

func TestVersion_FilesHashes(t *testing.T) {
	body := `{"id":"v1","project_id":"p","version_number":"1.0","version_type":"release","loaders":["forge"],"game_versions":["1.12.2"],"files":[{"url":"http://x/x.jar","filename":"x.jar","primary":true,"hashes":{"sha1":"aa","sha512":"bb"}}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv.Close()
	oldBase := baseURL
	oldClient := httpClient
	baseURL = srv.URL
	httpClient = srv.Client()
	defer func() { baseURL = oldBase; httpClient = oldClient }()

	v, err := FetchVersion(context.Background(), "v1")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Files) == 0 || v.Files[0].Hashes.SHA512 != "bb" {
		t.Fatalf("SHA512 = %q, want bb", v.Files[0].Hashes.SHA512)
	}
	if v.Files[0].Hashes.SHA1 != "aa" {
		t.Fatalf("SHA1 = %q, want aa", v.Files[0].Hashes.SHA1)
	}
}

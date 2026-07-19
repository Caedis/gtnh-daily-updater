package curseforge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseChannel(t *testing.T) {
	tests := []struct {
		input   string
		want    int
		wantErr bool
	}{
		{input: "", want: 1},
		{input: "release", want: 1},
		{input: "Beta", want: 2},
		{input: "ALPHA", want: 3},
		{input: "stable", wantErr: true},
	}
	for _, tt := range tests {
		got, err := ParseChannel(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("ParseChannel(%q) expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParseChannel(%q) unexpected error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("ParseChannel(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParseSource(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantProject int
		wantFile    int
		wantChannel string
		wantErr     bool
	}{
		{name: "project only", input: "238222", wantProject: 238222},
		{name: "project and file", input: "238222/4586932", wantProject: 238222, wantFile: 4586932},
		{name: "project with channel", input: "238222@beta", wantProject: 238222, wantChannel: "beta"},
		{name: "channel case insensitive", input: "238222@ALPHA", wantProject: 238222, wantChannel: "ALPHA"},
		{name: "invalid project letters", input: "abc", wantErr: true},
		{name: "invalid file letters", input: "238222/abc", wantErr: true},
		{name: "zero project", input: "0", wantErr: true},
		{name: "negative file", input: "238222/-5", wantErr: true},
		{name: "unknown channel", input: "238222@stable", wantErr: true},
		{name: "channel with file", input: "238222/4586932@beta", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proj, file, channel, err := ParseSource(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseSource(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSource(%q) unexpected error: %v", tt.input, err)
			}
			if proj != tt.wantProject || file != tt.wantFile || channel != tt.wantChannel {
				t.Fatalf("ParseSource(%q) = (%d, %d, %q), want (%d, %d, %q)", tt.input, proj, file, channel, tt.wantProject, tt.wantFile, tt.wantChannel)
			}
		})
	}
}

func TestFetchLatestFilePrefersNewestRelease(t *testing.T) {
	files := []File{
		{ID: 100, ReleaseType: 3, FileName: "alpha.jar", DownloadURL: "https://example.com/alpha.jar"},
		{ID: 200, ReleaseType: 1, FileName: "old-release.jar", DownloadURL: "https://example.com/old.jar"},
		{ID: 300, ReleaseType: 1, FileName: "latest-release.jar", DownloadURL: "https://example.com/latest.jar"},
		{ID: 250, ReleaseType: 2, FileName: "beta.jar", DownloadURL: "https://example.com/beta.jar"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") == "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(filesResponse{Data: files}); err != nil {
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

	got, err := FetchLatestFile(context.Background(), 12345, "", "test-key", 1)
	if err != nil {
		t.Fatalf("FetchLatestFile: %v", err)
	}
	if got.ID != 300 {
		t.Errorf("FetchLatestFile picked file ID %d, want 300", got.ID)
	}
	if got.FileName != "latest-release.jar" {
		t.Errorf("FetchLatestFile picked filename %q, want latest-release.jar", got.FileName)
	}
}

func TestFetchLatestFileChannel(t *testing.T) {
	files := []File{
		{ID: 100, ReleaseType: 3, FileName: "alpha.jar", DownloadURL: "https://example.com/alpha.jar"},
		{ID: 200, ReleaseType: 2, FileName: "old-beta.jar", DownloadURL: "https://example.com/old-beta.jar"},
		{ID: 300, ReleaseType: 1, FileName: "release.jar", DownloadURL: "https://example.com/release.jar"},
		{ID: 400, ReleaseType: 2, FileName: "new-beta.jar", DownloadURL: "https://example.com/new-beta.jar"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") == "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(filesResponse{Data: files})
	}))
	defer srv.Close()

	oldBase, oldClient := baseURL, httpClient
	baseURL, httpClient = srv.URL, srv.Client()
	defer func() { baseURL, httpClient = oldBase, oldClient }()

	tests := []struct {
		name   string
		maxTyp int
		wantID int
	}{
		{name: "release excludes betas", maxTyp: 1, wantID: 300},
		{name: "beta picks newest beta over older release", maxTyp: 2, wantID: 400},
		{name: "alpha still picks newest beta", maxTyp: 3, wantID: 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := FetchLatestFile(context.Background(), 238222, "", "key", tt.maxTyp)
			if err != nil {
				t.Fatalf("FetchLatestFile error: %v", err)
			}
			if f.ID != tt.wantID {
				t.Fatalf("FetchLatestFile(maxTyp=%d) = ID %d, want %d", tt.maxTyp, f.ID, tt.wantID)
			}
		})
	}
}

func TestFetchLatestFileErrorsOnNoReleases(t *testing.T) {
	files := []File{
		{ID: 100, ReleaseType: 3, FileName: "alpha.jar"},
		{ID: 200, ReleaseType: 2, FileName: "beta.jar"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(filesResponse{Data: files}); err != nil {
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

	_, err := FetchLatestFile(context.Background(), 12345, "", "test-key", 1)
	if err == nil {
		t.Fatal("FetchLatestFile: expected error for no releases, got nil")
	}
}

func TestFetchFileReturnsPinnedFile(t *testing.T) {
	want := File{
		ID:          67890,
		ModID:       12345,
		FileName:    "pinned-mod.jar",
		DownloadURL: "https://example.com/pinned-mod.jar",
		ReleaseType: 1,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(fileResponse{Data: want}); err != nil {
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

	got, err := FetchFile(context.Background(), 12345, 67890, "test-key")
	if err != nil {
		t.Fatalf("FetchFile: %v", err)
	}
	if got.ID != want.ID || got.FileName != want.FileName {
		t.Errorf("FetchFile got {ID:%d FileName:%q}, want {ID:%d FileName:%q}", got.ID, got.FileName, want.ID, want.FileName)
	}
}

func TestResolveDownloadURLUsesFieldWhenPresent(t *testing.T) {
	file := File{ID: 100, DownloadURL: "https://example.com/mod.jar"}
	got, err := ResolveDownloadURL(context.Background(), 12345, file, "key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != file.DownloadURL {
		t.Errorf("got %q, want %q", got, file.DownloadURL)
	}
}

func TestResolveDownloadURLFetchesWhenEmpty(t *testing.T) {
	wantURL := "https://edge.forgecdn.net/files/100/mod.jar"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(downloadURLResponse{Data: wantURL}); err != nil {
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

	file := File{ID: 100, DownloadURL: ""}
	got, err := ResolveDownloadURL(context.Background(), 12345, file, "test-key")
	if err != nil {
		t.Fatalf("ResolveDownloadURL: %v", err)
	}
	if got != wantURL {
		t.Errorf("got %q, want %q", got, wantURL)
	}
}

func TestCheckStatusRejectsMissingKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
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

	_, err := FetchLatestFile(context.Background(), 12345, "", "bad-key", 1)
	if err == nil {
		t.Fatal("expected error for 403, got nil")
	}
}

func TestFileVersion(t *testing.T) {
	f := File{ID: 4586932}
	if got := FileVersion(f); got != "4586932" {
		t.Errorf("FileVersion = %q, want %q", got, "4586932")
	}
}

func TestFile_SHA1(t *testing.T) {
	f := File{Hashes: []FileHash{
		{Algo: 2, Value: "deadbeef"},
		{Algo: 1, Value: "abc123"},
	}}
	if got := f.SHA1(); got != "abc123" {
		t.Fatalf("SHA1 = %q, want abc123", got)
	}
	empty := File{}
	if got := empty.SHA1(); got != "" {
		t.Fatalf("SHA1 on empty = %q, want \"\"", got)
	}
}

func TestFetchFile_ParsesHashes(t *testing.T) {
	body := `{"data":{"id":42,"modId":1,"fileName":"x.jar","hashes":[{"value":"AABB","algo":1},{"value":"CCDD","algo":2}]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv.Close()
	oldBase := baseURL
	oldClient := httpClient
	baseURL = srv.URL
	httpClient = srv.Client()
	defer func() { baseURL = oldBase; httpClient = oldClient }()

	f, err := FetchFile(context.Background(), 1, 42, "key")
	if err != nil {
		t.Fatal(err)
	}
	if f.SHA1() != "AABB" {
		t.Fatalf("SHA1 = %q, want AABB", f.SHA1())
	}
}

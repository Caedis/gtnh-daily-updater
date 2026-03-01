package curseforge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseSource(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantProject int
		wantFile    int
		wantErr     bool
	}{
		{name: "project only", input: "238222", wantProject: 238222, wantFile: 0},
		{name: "project and file", input: "238222/4586932", wantProject: 238222, wantFile: 4586932},
		{name: "invalid project letters", input: "abc", wantErr: true},
		{name: "invalid file letters", input: "238222/abc", wantErr: true},
		{name: "zero project", input: "0", wantErr: true},
		{name: "negative project", input: "-1", wantErr: true},
		{name: "zero file", input: "238222/0", wantErr: true},
		{name: "negative file", input: "238222/-5", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proj, file, err := ParseSource(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseSource(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSource(%q) unexpected error: %v", tt.input, err)
			}
			if proj != tt.wantProject || file != tt.wantFile {
				t.Fatalf("ParseSource(%q) = (%d, %d), want (%d, %d)", tt.input, proj, file, tt.wantProject, tt.wantFile)
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

	got, err := FetchLatestFile(context.Background(), 12345, "", "test-key")
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

	_, err := FetchLatestFile(context.Background(), 12345, "", "test-key")
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

	_, err := FetchLatestFile(context.Background(), 12345, "", "bad-key")
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

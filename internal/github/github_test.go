package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestPickPrimaryJar(t *testing.T) {
	t.Run("matches exact version suffix", func(t *testing.T) {
		assets := []ReleaseAsset{
			{Name: "Mod-1.2.3-dev.jar"},
			{Name: "Mod-1.2.3.jar"},
		}
		got := PickPrimaryJar(assets, "1.2.3")
		if got == nil || got.Name != "Mod-1.2.3.jar" {
			t.Fatalf("PickPrimaryJar=%v, want Mod-1.2.3.jar", got)
		}
	})

	t.Run("supports v-prefixed tag", func(t *testing.T) {
		assets := []ReleaseAsset{
			{Name: "Mod-1.4.7.jar"},
		}
		got := PickPrimaryJar(assets, "v1.4.7")
		if got == nil || got.Name != "Mod-1.4.7.jar" {
			t.Fatalf("PickPrimaryJar=%v, want Mod-1.4.7.jar", got)
		}
	})

	t.Run("single jar fallback", func(t *testing.T) {
		assets := []ReleaseAsset{
			{Name: "anything.JAR"},
		}
		got := PickPrimaryJar(assets, "1.0.0")
		if got == nil || got.Name != "anything.JAR" {
			t.Fatalf("PickPrimaryJar=%v, want anything.JAR", got)
		}
	})

	t.Run("ambiguous multiple jars returns nil", func(t *testing.T) {
		assets := []ReleaseAsset{
			{Name: "Mod-dev.jar"},
			{Name: "Mod-api.jar"},
		}
		if got := PickPrimaryJar(assets, "1.0.0"); got != nil {
			t.Fatalf("PickPrimaryJar should be nil for ambiguous jars, got %v", got)
		}
	})
}

func TestSelectLatestResult(t *testing.T) {
	releases := []Release{
		{
			TagName:    "1.0.0",
			Prerelease: false,
			Assets: []ReleaseAsset{
				{Name: "Mod-1.0.0.jar", BrowserDownloadURL: "https://example.test/mod-1.0.0.jar"},
			},
		},
		{
			TagName:    "1.2.0-PRE",
			Prerelease: false,
			Assets: []ReleaseAsset{
				{Name: "Mod-1.2.0-PRE.jar", BrowserDownloadURL: "https://example.test/mod-1.2.0-pre.jar"},
			},
		},
		{
			TagName:    "1.1.0",
			Prerelease: false,
			Assets: []ReleaseAsset{
				{Name: "Mod-1.1.0.jar", BrowserDownloadURL: "https://example.test/mod-1.1.0.jar"},
			},
		},
	}

	got, err := selectLatestResult(releases, "")
	if err != nil {
		t.Fatalf("selectLatestResult failed: %v", err)
	}
	if got.Version != "1.1.0" {
		t.Fatalf("version=%q want=1.1.0", got.Version)
	}
	if got.URL != "https://example.test/mod-1.1.0.jar" {
		t.Fatalf("url=%q want browser download URL", got.URL)
	}
	if got.IsAPI {
		t.Fatalf("IsAPI=true want=false")
	}
}

func TestSelectLatestResultUsesAPIURLWithToken(t *testing.T) {
	releases := []Release{
		{
			TagName: "1.0.0",
			Assets: []ReleaseAsset{
				{
					Name:               "Mod-1.0.0.jar",
					BrowserDownloadURL: "https://example.test/browser.jar",
					URL:                "https://api.github.com/assets/123",
				},
			},
		},
	}

	got, err := selectLatestResult(releases, "token")
	if err != nil {
		t.Fatalf("selectLatestResult failed: %v", err)
	}
	if !got.IsAPI {
		t.Fatalf("IsAPI=false want=true")
	}
	if got.URL != "https://api.github.com/assets/123" {
		t.Fatalf("url=%q want API url", got.URL)
	}
}

func TestFetchLatestRelease(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/releases" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("per_page"); got != "25" {
			t.Fatalf("unexpected per_page query: %s", got)
		}
		if got := r.Header.Get("Authorization"); got != "token test-token" {
			t.Fatalf("unexpected Authorization header: %q", got)
		}

		releases := []Release{
			{
				TagName: "1.2.0",
				Assets: []ReleaseAsset{
					{
						Name:               "Mod-1.2.0.jar",
						BrowserDownloadURL: "https://example.test/browser.jar",
						URL:                "https://api.github.com/assets/999",
					},
				},
			},
		}
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(releases); err != nil {
			t.Fatalf("encoding response: %v", err)
		}
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse failed: %v", err)
	}

	oldClient := githubHTTPClient
	githubHTTPClient = &http.Client{
		Transport: &rewriteHostTransport{
			host: parsed.Host,
			rt:   server.Client().Transport,
		},
	}
	t.Cleanup(func() { githubHTTPClient = oldClient })

	got, err := FetchLatestRelease(context.Background(), "owner/repo", "test-token")
	if err != nil {
		t.Fatalf("FetchLatestRelease failed: %v", err)
	}
	if got.Version != "1.2.0" {
		t.Fatalf("version=%q want=1.2.0", got.Version)
	}
	if got.URL != "https://api.github.com/assets/999" {
		t.Fatalf("url=%q want api url", got.URL)
	}
	if !got.IsAPI {
		t.Fatalf("IsAPI=false want=true")
	}
}

type rewriteHostTransport struct {
	host string
	rt   http.RoundTripper
}

func (t *rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = "http"
	cloned.URL.Host = t.host
	return t.rt.RoundTrip(cloned)
}

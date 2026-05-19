package selfupdate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"testing"

	"github.com/caedis/gtnh-daily-updater/internal/github"
)

type rewriteHostTransport struct {
	host string
	rt   http.RoundTripper
}

func (t *rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = t.host
	if t.rt != nil {
		return t.rt.RoundTrip(req)
	}
	return http.DefaultTransport.RoundTrip(req)
}

func overrideGitHubClient(t *testing.T, server *httptest.Server) {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	c := &http.Client{Transport: &rewriteHostTransport{host: parsed.Host, rt: server.Client().Transport}}
	old := github.SetHTTPClient(c)
	t.Cleanup(func() { github.SetHTTPClient(old) })
}

func TestCheckLatestPicksHighestSemver(t *testing.T) {
	assetName := AssetName("1.2.3")
	releases := []github.Release{
		{
			TagName: "1.2.3",
			Assets: []github.ReleaseAsset{
				{
					Name:               assetName,
					BrowserDownloadURL: "https://example.test/" + assetName,
					Digest:             "sha256:abcdef0123456789",
				},
			},
		},
		{
			TagName: "1.1.0",
			Assets: []github.ReleaseAsset{
				{
					Name:               AssetName("1.1.0"),
					BrowserDownloadURL: "https://example.test/old.zip",
					Digest:             "sha256:000000",
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(releases)
	}))
	defer server.Close()
	overrideGitHubClient(t, server)

	info, newer, err := CheckLatest(context.Background(), "1.0.0", false)
	if err != nil {
		t.Fatalf("CheckLatest: %v", err)
	}
	if !newer {
		t.Fatal("expected newer=true")
	}
	if info.Tag != "1.2.3" {
		t.Fatalf("tag=%q want 1.2.3", info.Tag)
	}
	if info.SHA256 != "abcdef0123456789" {
		t.Fatalf("sha256=%q", info.SHA256)
	}
	if info.AssetName != assetName {
		t.Fatalf("asset=%q want %q", info.AssetName, assetName)
	}
}

func TestCheckLatestDevSuppressesNewer(t *testing.T) {
	assetName := AssetName("2.0.0")
	releases := []github.Release{{
		TagName: "2.0.0",
		Assets: []github.ReleaseAsset{{
			Name:               assetName,
			BrowserDownloadURL: "https://example.test/x.zip",
			Digest:             "sha256:deadbeef",
		}},
	}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(releases)
	}))
	defer server.Close()
	overrideGitHubClient(t, server)

	info, newer, err := CheckLatest(context.Background(), "dev", false)
	if err != nil {
		t.Fatalf("CheckLatest: %v", err)
	}
	if newer {
		t.Fatal("dev should report newer=false")
	}
	if info == nil || info.Tag != "2.0.0" {
		t.Fatalf("info=%+v", info)
	}
}

func TestCheckLatestSkipsPrereleases(t *testing.T) {
	releases := []github.Release{
		{
			TagName:    "1.5.0-pre",
			Prerelease: true,
			Assets: []github.ReleaseAsset{{
				Name:   AssetName("1.5.0-pre"),
				Digest: "sha256:aa",
			}},
		},
		{
			TagName: "1.0.0",
			Assets: []github.ReleaseAsset{{
				Name:               AssetName("1.0.0"),
				BrowserDownloadURL: "https://example.test/1.0.0.zip",
				Digest:             "sha256:bb",
			}},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(releases)
	}))
	defer server.Close()
	overrideGitHubClient(t, server)

	info, _, err := CheckLatest(context.Background(), "0.9.0", false)
	if err != nil {
		t.Fatalf("CheckLatest: %v", err)
	}
	if info.Tag != "1.0.0" {
		t.Fatalf("tag=%q want 1.0.0 (prerelease should be skipped)", info.Tag)
	}
}

func TestAssetNameMatchesPlatform(t *testing.T) {
	got := AssetName("9.9.9")
	want := "gtnh-daily-updater-9.9.9-" + runtime.GOOS + "-" + runtime.GOARCH + ".zip"
	if got != want {
		t.Fatalf("AssetName=%q want %q", got, want)
	}
}

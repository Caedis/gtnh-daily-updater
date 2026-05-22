package downloader

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestNewHasher_Algos(t *testing.T) {
	cases := []struct {
		algo string
		want string // hex digest of "hello"
	}{
		{"sha256", "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"},
		{"sha1", "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d"},
		{"sha512", "9b71d224bd62f3785d96d46ad3ea3d73319bfbc2890caadae2dff72519673ca72323c3d99ba5c11d7c7acc6e14b8c5da0c4663475c2e5c3adef46f73bcdec043"},
	}
	for _, c := range cases {
		h, err := newHasher(c.algo)
		if err != nil {
			t.Fatalf("%s: %v", c.algo, err)
		}
		h.Write([]byte("hello"))
		if got := h.Hex(); got != c.want {
			t.Fatalf("%s: got %s want %s", c.algo, got, c.want)
		}
	}
	if _, err := newHasher(""); err == nil {
		t.Fatal("empty algo: want error")
	}
	if _, err := newHasher("md5"); err == nil {
		t.Fatal("md5: want error")
	}
}

// sha256 of "good-bytes"
const goodBytes = "good-bytes"
const goodSHA256 = "d79d9fd44ad034758fbdc3b2fa305894e98f111aae32efeb2c70a3b97cd0a456"

func TestRun_HashMatchSucceeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, goodBytes)
	}))
	defer srv.Close()

	dest := t.TempDir()
	results := Run(context.Background(),
		[]Download{{URL: srv.URL, Filename: "x.jar", ModName: "m", ExpectedHash: goodSHA256, HashAlgo: "sha256"}},
		dest, 1, "", "", nil,
	)
	if results[0].Err != nil {
		t.Fatalf("err = %v", results[0].Err)
	}
	got, _ := os.ReadFile(filepath.Join(dest, "x.jar"))
	if string(got) != goodBytes {
		t.Fatalf("file = %q", got)
	}
}

func TestRun_HashMismatchRetriesAndFails(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, "wrong-bytes")
	}))
	defer srv.Close()

	dest := t.TempDir()
	results := Run(context.Background(),
		[]Download{{URL: srv.URL, Filename: "x.jar", ModName: "m", ExpectedHash: goodSHA256, HashAlgo: "sha256"}},
		dest, 1, "", "", nil,
	)
	if !errors.Is(results[0].Err, ErrHashMismatch) {
		t.Fatalf("err = %v, want ErrHashMismatch", results[0].Err)
	}
	if hits.Load() != int32(maxRetries) {
		t.Fatalf("hits = %d, want %d", hits.Load(), maxRetries)
	}
	if _, err := os.Stat(filepath.Join(dest, "x.jar")); !os.IsNotExist(err) {
		t.Fatal("dest file should not exist after mismatch")
	}
}

func TestRun_EmptyAlgoSkipsValidation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "anything")
	}))
	defer srv.Close()
	dest := t.TempDir()
	results := Run(context.Background(),
		[]Download{{URL: srv.URL, Filename: "x.jar", ModName: "m"}},
		dest, 1, "", "", nil,
	)
	if results[0].Err != nil {
		t.Fatalf("err = %v", results[0].Err)
	}
}

func TestRun_CacheHit_ValidatesAndReplaces(t *testing.T) {
	var serverHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverHits.Add(1)
		fmt.Fprint(w, goodBytes)
	}))
	defer srv.Close()

	cache := t.TempDir()
	dest := t.TempDir()

	modDir := filepath.Join(cache, "m")
	if err := os.MkdirAll(modDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modDir, "x.jar"), []byte("corrupt"), 0644); err != nil {
		t.Fatal(err)
	}

	results := Run(context.Background(),
		[]Download{{URL: srv.URL, Filename: "x.jar", ModName: "m", ExpectedHash: goodSHA256, HashAlgo: "sha256"}},
		dest, 1, "", cache, nil,
	)
	if results[0].Err != nil {
		t.Fatalf("err = %v", results[0].Err)
	}
	if serverHits.Load() == 0 {
		t.Fatal("expected server to be contacted after cache invalidation")
	}
	got, _ := os.ReadFile(filepath.Join(dest, "x.jar"))
	if string(got) != goodBytes {
		t.Fatalf("dest = %q", got)
	}
	cached, _ := os.ReadFile(filepath.Join(modDir, "x.jar"))
	if string(cached) != goodBytes {
		t.Fatalf("cache not replaced: %q", cached)
	}
}

func TestRun_CacheHit_ValidMatches(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be contacted")
	}))
	defer srv.Close()

	cache := t.TempDir()
	dest := t.TempDir()
	modDir := filepath.Join(cache, "m")
	os.MkdirAll(modDir, 0755)
	os.WriteFile(filepath.Join(modDir, "x.jar"), []byte(goodBytes), 0644)

	results := Run(context.Background(),
		[]Download{{URL: srv.URL, Filename: "x.jar", ModName: "m", ExpectedHash: goodSHA256, HashAlgo: "sha256"}},
		dest, 1, "", cache, nil,
	)
	if results[0].Err != nil {
		t.Fatalf("err = %v", results[0].Err)
	}
}

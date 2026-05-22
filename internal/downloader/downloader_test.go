package downloader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

// Keep imports satisfied for later tasks that will add tests using these.
var _ = bytes.NewBuffer
var _ = context.Background
var _ = errors.Is
var _ = fmt.Sprint
var _ = http.MethodGet
var _ = httptest.NewServer
var _ = os.Open
var _ = filepath.Join
var _ = strings.TrimSpace
var _ atomic.Int32

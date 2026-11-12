package breach

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBreachedKAnonymity(t *testing.T) {
	password := "correct horse battery staple"
	sum := sha1.Sum([]byte(password))
	hash := strings.ToUpper(hex.EncodeToString(sum[:]))
	var sawPrefix string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPrefix = strings.TrimPrefix(r.URL.Path, "/")
		if r.Header.Get("Add-Padding") != "true" {
			t.Error("missing Add-Padding header")
		}
		_, _ = w.Write([]byte("0000000000000000000000000000000000A:1\r\n" + hash[5:] + ":42\r\n"))
	}))
	defer srv.Close()

	checker := New(srv.URL, srv.Client())
	breached, err := checker.Breached(context.Background(), password)
	if err != nil {
		t.Fatal(err)
	}
	if !breached {
		t.Fatal("expected breached password")
	}
	if sawPrefix != hash[:5] {
		t.Fatalf("prefix = %q, want %q", sawPrefix, hash[:5])
	}
}

func TestNotBreachedAndZeroCount(t *testing.T) {
	password := "a unique phrase that is not in the corpus"
	sum := sha1.Sum([]byte(password))
	hash := strings.ToUpper(hex.EncodeToString(sum[:]))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(hash[5:] + ":0\r\n"))
	}))
	defer srv.Close()
	breached, err := New(srv.URL, srv.Client()).Breached(context.Background(), password)
	if err != nil {
		t.Fatal(err)
	}
	if breached {
		t.Fatal("zero count should not count as breached")
	}
}

func TestRangeAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	if _, err := New(srv.URL, srv.Client()).Breached(context.Background(), "x"); err == nil {
		t.Fatal("expected error")
	}
}

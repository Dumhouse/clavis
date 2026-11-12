package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dumhouse/clavis/pkg/problem"
)

func TestGetSuccess(t *testing.T) {
	var gotAuth, gotUA, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]string{"name": "clavis"})
	}))
	defer srv.Close()

	c, err := New(srv.URL, WithToken("clv_mt_test"), WithUserAgent("test-agent"))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]string
	if err := c.Get(context.Background(), "/api/v1/version", &out); err != nil {
		t.Fatal(err)
	}
	if out["name"] != "clavis" {
		t.Fatalf("out = %v", out)
	}
	if gotAuth != "Bearer clv_mt_test" || gotUA != "test-agent" || gotPath != "/api/v1/version" {
		t.Fatalf("request: auth=%q ua=%q path=%q", gotAuth, gotUA, gotPath)
	}
}

func TestProblemError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", problem.ContentType)
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(problem.Forbidden("nope"))
	}))
	defer srv.Close()

	c, _ := New(srv.URL)
	err := c.Get(context.Background(), "/api/v1/x", nil)
	var p *problem.Problem
	if !errors.As(err, &p) {
		t.Fatalf("err = %T %v", err, err)
	}
	if p.Status != http.StatusForbidden || p.Code != "forbidden" {
		t.Fatalf("problem = %+v", p)
	}
}

func TestNonJSONErrorFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer srv.Close()

	c, _ := New(srv.URL, WithRetries(0))
	err := c.Get(context.Background(), "/x", nil)
	var p *problem.Problem
	if !errors.As(err, &p) {
		t.Fatalf("err = %T %v", err, err)
	}
	if p.Code != "http_error" || p.Status != http.StatusBadGateway {
		t.Fatalf("problem = %+v", p)
	}
}

func TestRetriesTransientFailures(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer srv.Close()

	c, _ := New(srv.URL, WithRetries(2))
	c.sleep = func(context.Context, time.Duration) error { return nil }
	var out map[string]bool
	if err := c.Get(context.Background(), "/x", &out); err != nil {
		t.Fatal(err)
	}
	if attempts != 3 || !out["ok"] {
		t.Fatalf("attempts = %d, out = %v", attempts, out)
	}
}

func TestRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c, _ := New(srv.URL, WithRetries(1))
	var slept time.Duration
	c.sleep = func(_ context.Context, d time.Duration) error { slept = d; return nil }
	if err := c.Get(context.Background(), "/x", nil); err == nil {
		t.Fatal("expected rate limit error")
	}
	if slept != 0 {
		t.Fatalf("slept = %v", slept)
	}
}

func TestNoRetryOnBadRequest(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		http.Error(w, "bad", http.StatusBadRequest)
	}))
	defer srv.Close()
	c, _ := New(srv.URL, WithRetries(3))
	c.sleep = func(context.Context, time.Duration) error { return nil }
	_ = c.Get(context.Background(), "/x", nil)
	if attempts != 1 {
		t.Fatalf("attempts = %d", attempts)
	}
}

func TestNoRetryOnPost(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c, _ := New(srv.URL, WithRetries(3))
	c.sleep = func(context.Context, time.Duration) error { return nil }
	_ = c.Post(context.Background(), "/x", map[string]string{"a": "b"}, nil)
	if attempts != 1 {
		t.Fatalf("POST retried %d times", attempts)
	}
}

func TestContextCancelledDuringRetry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c, _ := New(srv.URL, WithRetries(3))
	c.sleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Get(ctx, "/x", nil); err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestDeleteNoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c, _ := New(srv.URL)
	if err := c.Delete(context.Background(), "/x"); err != nil {
		t.Fatal(err)
	}
}

func TestNewValidation(t *testing.T) {
	for _, u := range []string{"", "not a url", "ftp://example.com", "/relative"} {
		if _, err := New(u); err == nil {
			t.Errorf("New(%q) succeeded", u)
		}
	}
	c, _ := New("https://example.com")
	if c.BaseURL() != "https://example.com" {
		t.Fatalf("base = %q", c.BaseURL())
	}
}

func TestAbsoluteURLRejected(t *testing.T) {
	c, _ := New("https://example.com")
	if err := c.Get(context.Background(), "https://evil.example.com/x", nil); err == nil {
		t.Fatal("absolute URL accepted")
	}
	if err := c.Get(context.Background(), "%zz", nil); err == nil {
		t.Fatal("invalid path accepted")
	}
}

func TestDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()
	c, _ := New(srv.URL)
	var out map[string]string
	if err := c.Get(context.Background(), "/x", &out); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}

func TestBackoff(t *testing.T) {
	if backoff(0) != 100*time.Millisecond {
		t.Fatalf("backoff(0) = %v", backoff(0))
	}
	if backoff(10) != 2*time.Second {
		t.Fatalf("backoff(10) = %v", backoff(10))
	}
}

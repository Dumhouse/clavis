package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dumhouse/clavis/internal/config"
)

func TestLocalReadyURL(t *testing.T) {
	plain := config.Default()
	plain.Server.Addr = ":8080"
	if got := localReadyURL(plain); got != "http://127.0.0.1:8080/readyz" {
		t.Fatalf("plain: %s", got)
	}
	bound := config.Default()
	bound.Server.Addr = "10.0.0.5:9000"
	if got := localReadyURL(bound); got != "http://10.0.0.5:9000/readyz" {
		t.Fatalf("bound: %s", got)
	}
	secure := config.Default()
	secure.Server.Addr = ":8080"
	secure.Server.TLS.Mode = config.TLSSelfSigned
	secure.Server.TLS.Addr = "0.0.0.0:8443"
	if got := localReadyURL(secure); got != "https://127.0.0.1:8443/readyz" {
		t.Fatalf("tls: %s", got)
	}
}

func TestProbeReady(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ok := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ok.Close()
	if err := probeReady(ctx, ok.URL); err != nil {
		t.Fatalf("self-signed 200 should be ready: %v", err)
	}

	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer down.Close()
	if err := probeReady(ctx, down.URL); err == nil {
		t.Fatal("503 should not be ready")
	}

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://elsewhere.invalid/", http.StatusPermanentRedirect)
	}))
	defer redirect.Close()
	if err := probeReady(ctx, redirect.URL); err == nil {
		t.Fatal("a redirect is not a ready answer")
	}

	closed := httptest.NewServer(http.NotFoundHandler())
	url := closed.URL
	closed.Close()
	if err := probeReady(ctx, url); err == nil {
		t.Fatal("a closed port should not be ready")
	}
}

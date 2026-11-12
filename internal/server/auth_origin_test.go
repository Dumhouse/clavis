package server_test

import (
	"net/http"
	"testing"
)

// The Vite dev server proxies /api calls to the Go server and rewrites Host to
// the API address, so the browser's Origin (localhost:5173) never matches
// r.Host. Dev mode must still accept it, otherwise every login/write from the
// dev UI fails the CSRF origin check with 403.
func TestDevViteOriginAccepted(t *testing.T) {
	f := newFixture(t)
	c := &apiClient{t: t, h: f.srv.Handler()}

	for _, origin := range []string{"http://localhost:5173", "http://127.0.0.1:5173"} {
		rec := c.requestHeaders(http.MethodPost, "/api/v1/auth/login",
			map[string]string{"email": "nobody@example.com", "password": "not-the-password"},
			map[string]string{"Origin": origin})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("login with origin %s = %d body=%s, want 401", origin, rec.Code, rec.Body)
		}
	}
}

func TestForeignOriginRejected(t *testing.T) {
	f := newFixture(t)
	c := &apiClient{t: t, h: f.srv.Handler()}

	rec := c.requestHeaders(http.MethodPost, "/api/v1/auth/login",
		map[string]string{"email": "nobody@example.com", "password": "not-the-password"},
		map[string]string{"Origin": "https://evil.example"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("login with foreign origin = %d body=%s, want 403", rec.Code, rec.Body)
	}
}

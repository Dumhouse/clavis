package server_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dumhouse/clavis/internal/config"
	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/server"
	"github.com/dumhouse/clavis/internal/store/storetest"
)

func testWebFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte(`<!doctype html><html><body><div id="root"></div></body></html>`)},
		"assets/app.js": &fstest.MapFile{Data: []byte(`console.log("app")`)},
		"favicon.svg":   &fstest.MapFile{Data: []byte(`<svg/>`)},
	}
}

func newTestServer(t *testing.T) *server.Server {
	t.Helper()
	st := storetest.Open(t)
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Database.Path = filepath.Join(t.TempDir(), "test.db")
	cfg.Server.Addr = "127.0.0.1:0"
	cfg.Server.BaseURL = "http://localhost:8080"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	srv, err := server.New(cfg, st, logger, key, server.WithWebFS(testWebFS()))
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestRouteCoverage(t *testing.T) {
	srv := newTestServer(t)
	if missing := srv.RouteCoverage(); len(missing) != 0 {
		t.Fatalf("routes without declarations: %v", missing)
	}
}

func TestHealthAndReady(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Handler()

	rec := get(t, h, "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d", rec.Code)
	}
	var health map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if health["status"] != "ok" || health["version"] == "" {
		t.Fatalf("health = %v", health)
	}

	rec = get(t, h, "/readyz")
	if rec.Code != http.StatusOK {
		t.Fatalf("readyz status = %d body=%s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"database":"ok"`) {
		t.Fatalf("ready body = %s", rec.Body)
	}
}

func TestReadyFailsWhenDatabaseDown(t *testing.T) {
	st := storetest.Open(t)
	key, _ := crypto.GenerateKey()
	cfg := config.Default()
	cfg.Database.Path = filepath.Join(t.TempDir(), "test.db")
	cfg.Server.BaseURL = "http://localhost:8080"
	srv, err := server.New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), key)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	rec := get(t, srv.Handler(), "/readyz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz status = %d body=%s", rec.Code, rec.Body)
	}
}

func TestSecurityHeaders(t *testing.T) {
	srv := newTestServer(t)
	rec := get(t, srv.Handler(), "/healthz")
	if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "script-src 'self'") {
		t.Fatalf("CSP = %q", got)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing nosniff")
	}
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("missing frame options")
	}
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Fatal("HSTS set for plain http")
	}
}

func TestHSTSOnHTTPS(t *testing.T) {
	st := storetest.Open(t)
	key, _ := crypto.GenerateKey()
	cfg := config.Default()
	cfg.Database.Path = filepath.Join(t.TempDir(), "test.db")
	cfg.Server.BaseURL = "https://clavis.example.com"
	srv, err := server.New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), key)
	if err != nil {
		t.Fatal(err)
	}
	rec := get(t, srv.Handler(), "/healthz")
	if rec.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("missing HSTS")
	}
}

func TestVersionEndpoint(t *testing.T) {
	srv := newTestServer(t)
	rec := get(t, srv.Handler(), "/api/v1/version")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body)
	}
	var body server.VersionBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Name != "clavis" || body.APIVersion != "v1" || body.Version == "" {
		t.Fatalf("body = %+v", body)
	}
}

func TestOpenAPI(t *testing.T) {
	srv := newTestServer(t)
	rec := get(t, srv.Handler(), "/api/v1/openapi.json")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "openapi") {
		t.Fatalf("content type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), `"/api/v1/version"`) {
		t.Fatal("spec is missing the version route")
	}
}

func TestSPAFallback(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Handler()

	for _, path := range []string{"/", "/projects", "/orgs/acme/settings"} {
		rec := get(t, h, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "<div id=\"root\">") {
			t.Fatalf("%s did not serve the SPA", path)
		}
	}

	rec := get(t, h, "/assets/missing.js")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing asset status = %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "problem+json") {
		t.Fatalf("missing asset content type = %q", rec.Header().Get("Content-Type"))
	}
}

func TestUnknownAPIPathIsJSON(t *testing.T) {
	srv := newTestServer(t)
	rec := get(t, srv.Handler(), "/api/v1/does-not-exist")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "problem+json") {
		t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
	}
}

func TestUndeclaredRouteReported(t *testing.T) {
	srv := newTestServer(t)
	router, ok := srv.Handler().(interface {
		Get(string, http.HandlerFunc)
	})
	if !ok {
		t.Fatal("handler does not expose chi routing")
	}
	router.Get("/api/v1/undeclared", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	missing := srv.RouteCoverage()
	if len(missing) != 1 || missing[0] != "GET /api/v1/undeclared" {
		t.Fatalf("coverage = %v", missing)
	}
}

func TestMetrics(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Handler()
	get(t, h, "/healthz")
	rec := get(t, h, "/metrics")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "clavis_http_requests_total") {
		t.Fatalf("metrics body = %s", rec.Body)
	}
}

func TestSPANotBuilt(t *testing.T) {
	st := storetest.Open(t)
	key, _ := crypto.GenerateKey()
	cfg := config.Default()
	cfg.Database.Path = filepath.Join(t.TempDir(), "test.db")
	cfg.Server.BaseURL = "http://localhost:8080"
	srv, err := server.New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), key, server.WithWebFS(fstest.MapFS{}))
	if err != nil {
		t.Fatal(err)
	}
	rec := get(t, srv.Handler(), "/")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Web UI not built") {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestMethodNotAllowedOnSPA(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/projects", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}

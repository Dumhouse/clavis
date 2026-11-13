package server

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dumhouse/clavis/internal/config"
)

func TestEnsureSelfSigned(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "c.crt"), filepath.Join(dir, "c.key")
	now := time.Now()
	hosts := selfSignedHosts("192.168.100.234")

	cert, fp1, err := ensureSelfSigned(certFile, keyFile, hosts, now)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"192.168.100.234", "localhost", "127.0.0.1"} {
		if err := leaf.VerifyHostname(h); err != nil {
			t.Errorf("certificate does not cover %s: %v", h, err)
		}
	}
	if info, _ := os.Stat(keyFile); info.Mode().Perm() != 0o600 {
		t.Errorf("key mode = %v, want 0600", info.Mode().Perm())
	}

	// Same hosts: reused.
	if _, fp2, err := ensureSelfSigned(certFile, keyFile, hosts, now); err != nil || fp2 != fp1 {
		t.Fatalf("expected reuse, fp1=%s fp2=%s err=%v", fp1, fp2, err)
	}
	// New host: regenerated.
	if _, fp3, err := ensureSelfSigned(certFile, keyFile, selfSignedHosts("clavis.lan"), now); err != nil || fp3 == fp1 {
		t.Fatalf("expected new certificate for a new host, err=%v", err)
	}
	// Close to expiry: regenerated.
	_, fp4, _ := ensureSelfSigned(certFile, keyFile, selfSignedHosts("clavis.lan"), now)
	if _, fp5, err := ensureSelfSigned(certFile, keyFile, selfSignedHosts("clavis.lan"), now.Add(selfSignedValidity-24*time.Hour)); err != nil || fp5 == fp4 {
		t.Fatalf("expected renewal near expiry, err=%v", err)
	}
}

func TestCertReloader(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "c.crt"), filepath.Join(dir, "c.key")
	if _, _, err := ensureSelfSigned(certFile, keyFile, selfSignedHosts("a.test"), time.Now()); err != nil {
		t.Fatal(err)
	}
	r, err := newCertReloader(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := r.GetCertificate(&tls.ClientHelloInfo{})
	if _, _, err := ensureSelfSigned(certFile, keyFile, selfSignedHosts("b.test"), time.Now()); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(certFile, future, future); err != nil {
		t.Fatal(err)
	}
	r.checked = time.Time{} // skip the 30 s throttle
	second, _ := r.GetCertificate(&tls.ClientHelloInfo{})
	if string(first.Certificate[0]) == string(second.Certificate[0]) {
		t.Fatal("reloader kept the old certificate after the file changed")
	}
	if _, err := newCertReloader(filepath.Join(dir, "missing"), keyFile); err == nil {
		t.Fatal("expected an error for a missing certificate")
	}
}

func TestHTTPSRedirectKeepsHealthChecks(t *testing.T) {
	router := chi.NewRouter()
	router.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	cfg := config.Default()
	cfg.Server.BaseURL = "https://clavis.example.com"
	srv := &Server{cfg: cfg, router: router}
	h := srv.httpsRedirect()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://clavis.example.com/projects?x=1", nil))
	if rec.Code != http.StatusPermanentRedirect || rec.Header().Get("Location") != "https://clavis.example.com/projects?x=1" {
		t.Fatalf("redirect = %d %q", rec.Code, rec.Header().Get("Location"))
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://clavis.example.com/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz over http = %d, want 200", rec.Code)
	}
}

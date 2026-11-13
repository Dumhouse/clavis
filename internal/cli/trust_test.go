package cli

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func trustTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(server.Close)
	t.Setenv("CLAVIS_TRUST_FILE", filepath.Join(t.TempDir(), "trusted.json"))
	t.Setenv("CLAVIS_SERVER_FINGERPRINT", "")
	t.Setenv("CLAVIS_CA_CERT", "")
	return server
}

func get(t *testing.T, server string) error {
	t.Helper()
	hc, err := httpClientFor(server, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	response, err := hc.Get(server + "/healthz")
	if err == nil {
		_ = response.Body.Close()
	}
	return err
}

func TestTrustOnFirstUse(t *testing.T) {
	server := trustTestServer(t)

	var untrusted *untrustedCertError
	if err := get(t, server.URL); !errors.As(err, &untrusted) || untrusted.Pinned != nil {
		t.Fatalf("want an untrusted certificate error, got %v", err)
	}
	if probe := probeServerTLS(t.Context(), server.URL); probe == nil {
		t.Fatal("probe should report the untrusted certificate")
	}

	if err := pinCertificate(trustKey(server.URL), server.Certificate()); err != nil {
		t.Fatal(err)
	}
	if err := get(t, server.URL); err != nil {
		t.Fatalf("pinned server should be trusted: %v", err)
	}

	// Another server on the same address with a different key: refused.
	pins, _ := loadTrust()
	entry := pins[trustKey(server.URL)]
	entry.PublicKey, entry.Certificate = "00", "00"
	pins[trustKey(server.URL)] = entry
	if err := saveTrust(pins); err != nil {
		t.Fatal(err)
	}
	if err := get(t, server.URL); !errors.As(err, &untrusted) || untrusted.Pinned == nil {
		t.Fatalf("want a changed-certificate error, got %v", err)
	}
}

func TestTrustFingerprintEnv(t *testing.T) {
	server := trustTestServer(t)
	t.Setenv("CLAVIS_SERVER_FINGERPRINT", colonHex(certHash(server.Certificate())))
	if err := get(t, server.URL); err != nil {
		t.Fatalf("certificate fingerprint: %v", err)
	}
	t.Setenv("CLAVIS_SERVER_FINGERPRINT", "sha256:"+publicKeyHash(server.Certificate()))
	if err := get(t, server.URL); err != nil {
		t.Fatalf("public key fingerprint: %v", err)
	}
	t.Setenv("CLAVIS_SERVER_FINGERPRINT", "AB:CD")
	var untrusted *untrustedCertError
	if err := get(t, server.URL); !errors.As(err, &untrusted) || !untrusted.EnvPin {
		t.Fatalf("want a fingerprint mismatch, got %v", err)
	}
}

func TestTrustCAFile(t *testing.T) {
	server := trustTestServer(t)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(ca, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAVIS_CA_CERT", ca)
	if err := get(t, server.URL); err != nil {
		t.Fatalf("CA file should be trusted: %v", err)
	}
	t.Setenv("CLAVIS_CA_CERT", filepath.Join(t.TempDir(), "missing.pem"))
	if _, err := httpClientFor(server.URL, time.Second); err == nil {
		t.Fatal("missing CA file should fail")
	}
}

func TestTrustHelpers(t *testing.T) {
	cases := map[string]string{
		"https://Example.com":        "example.com:443",
		"https://192.168.1.20:8443/": "192.168.1.20:8443",
		"http://localhost:8080":      "",
		"https://[::1]:9443":         "[::1]:9443",
	}
	for in, want := range cases {
		if got := trustKey(in); got != want {
			t.Errorf("trustKey(%q) = %q, want %q", in, got, want)
		}
	}
	for _, in := range []string{"AB:CD", "abcd", "sha256:ABCD", "SHA256 Fingerprint=AB:CD"} {
		if got := normalizeFingerprint(in); got != "abcd" {
			t.Errorf("normalizeFingerprint(%q) = %q", in, got)
		}
	}
	if got := normalizeServerURL("192.168.1.20/"); got != "https://192.168.1.20" {
		t.Errorf("normalizeServerURL = %q", got)
	}
}

func TestTemporaryCertIsNotPinned(t *testing.T) {
	leaf := &x509.Certificate{Subject: pkix.Name{CommonName: "c.example.com", Organization: []string{"Clavis temporary"}}}
	leaf.Issuer = leaf.Subject
	if !isTemporaryCert(leaf) {
		t.Fatal("want temporary")
	}
	err := confirmTrust(&Output{}, &untrustedCertError{Server: "https://c.example.com", Leaf: leaf})
	if err == nil || !strings.Contains(err.Error(), "still getting") {
		t.Fatalf("got %v", err)
	}
	leaf.Subject.Organization = []string{"Clavis self-signed"}
	leaf.Issuer = leaf.Subject
	if isTemporaryCert(leaf) {
		t.Fatal("regular self-signed is not temporary")
	}
}

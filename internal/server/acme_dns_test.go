package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dumhouse/clavis/internal/config"
)

func TestSelfSignedKeepsKeyOnRenewal(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "c.crt"), filepath.Join(dir, "c.key")
	first, _, err := ensureSelfSigned(certFile, keyFile, selfSignedHosts("192.168.1.20"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// A new host forces a new certificate; the key must stay.
	second, _, err := ensureSelfSigned(certFile, keyFile, selfSignedHosts("clavis.lan"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first.Leaf.Raw, second.Leaf.Raw) {
		t.Fatal("expected a new certificate")
	}
	if !bytes.Equal(first.Leaf.RawSubjectPublicKeyInfo, second.Leaf.RawSubjectPublicKeyInfo) {
		t.Fatal("renewal changed the key; pinned clients would be asked again")
	}
}

func TestSelfSignedManagerRenewsWhileRunning(t *testing.T) {
	dir := t.TempDir()
	m, err := newSelfSignedManager(dir, "192.168.1.20", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	// Pretend the server has been running for a year.
	old, _, err := ensureSelfSigned(filepath.Join(t.TempDir(), "o.crt"), m.keyFile, m.hosts,
		time.Now().Add(-selfSignedValidity+10*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	m.cert, m.checked = old, time.Time{}
	cert, err := m.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if time.Until(cert.Leaf.NotAfter) < 300*24*time.Hour {
		t.Fatalf("not renewed: expires %s", cert.Leaf.NotAfter)
	}
	if !bytes.Equal(cert.Leaf.RawSubjectPublicKeyInfo, old.Leaf.RawSubjectPublicKeyInfo) {
		t.Fatal("renewal changed the key")
	}
}

func TestCloudflareDNS(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer cf-token" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":9109}]}`))
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Query().Get("name") == "example.com":
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"zone1"}]}`))
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"success":true,"result":[]}`))
		case r.Method == http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["name"] != "_acme-challenge.clavis.home.example.com" || body["type"] != "TXT" {
				t.Errorf("bad record %v", body)
			}
			_, _ = w.Write([]byte(`{"success":true,"result":{"id":"rec1"}}`))
		case r.Method == http.MethodDelete:
			_, _ = w.Write([]byte(`{"success":true,"result":{}}`))
		}
	}))
	defer api.Close()

	cf := &cloudflareDNS{token: "cf-token", base: api.URL, http: api.Client(), records: map[string]string{}}
	ctx := context.Background()
	fqdn := "_acme-challenge.clavis.home.example.com."
	if err := cf.Present(ctx, fqdn, "v1"); err != nil {
		t.Fatal(err)
	}
	if err := cf.CleanUp(ctx, fqdn, "v1"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, "\n")
	for _, want := range []string{"name=clavis.home.example.com", "name=home.example.com", "name=example.com",
		"POST /zones/zone1/dns_records", "DELETE /zones/zone1/dns_records/rec1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing call %q in:\n%s", want, joined)
		}
	}

	bad := &cloudflareDNS{token: "wrong", base: api.URL, http: api.Client(), records: map[string]string{}}
	if err := bad.Present(ctx, fqdn, "v1"); err == nil || !strings.Contains(err.Error(), "DNS → Edit") {
		t.Fatalf("want a permission hint, got %v", err)
	}
}

func TestDuckDNS(t *testing.T) {
	var got []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.RawQuery)
		if r.URL.Query().Get("token") != "duck" {
			_, _ = w.Write([]byte("KO"))
			return
		}
		_, _ = w.Write([]byte("OK\n"))
	}))
	defer api.Close()
	d := &duckDNS{token: "duck", base: api.URL, http: api.Client()}
	ctx := context.Background()
	if err := d.Present(ctx, "_acme-challenge.clavis.myhome.duckdns.org.", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := d.CleanUp(ctx, "_acme-challenge.clavis.myhome.duckdns.org.", "v1"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got[0], "domains=myhome") || !strings.Contains(got[0], "txt=v1") || !strings.Contains(got[1], "clear=true") {
		t.Fatalf("queries: %v", got)
	}
	d.token = "bad"
	if err := d.Present(ctx, "_acme-challenge.myhome.duckdns.org.", "v1"); err == nil {
		t.Fatal("want an error for KO")
	}
}

func TestWaitForTXT(t *testing.T) {
	seen := 0
	m := &dnsCertManager{
		resolvers: []string{"ns1:53", "ns2:53"},
		wait:      20 * time.Second,
		lookupTXT: func(_ context.Context, server, _ string) ([]string, error) {
			seen++
			if server == "ns2:53" && seen < 4 {
				return nil, nil // not propagated yet
			}
			return []string{"other", "want"}, nil
		},
	}
	if err := m.waitForTXT(context.Background(), "_acme-challenge.a.example.com.", "want"); err != nil {
		t.Fatal(err)
	}
	m.wait = 0
	m.lookupTXT = func(context.Context, string, string) ([]string, error) { return nil, nil }
	if err := m.waitForTXT(context.Background(), "_acme-challenge.a.example.com.", "want"); err == nil {
		t.Fatal("want a timeout")
	}
}

func TestACMEDNSServesFallbackUntilIssued(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.BaseURL = "https://clavis.example.com"
	cfg.Server.TLS = config.TLSConfig{Mode: config.TLSACME, Dir: t.TempDir(), ACMEDNS: "cloudflare", ACMEDNSToken: "x"}
	s := &Server{cfg: cfg, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	setup, err := s.prepareTLS()
	if err != nil {
		t.Fatal(err)
	}
	if setup.start == nil {
		t.Fatal("expected a background issuer")
	}
	cert, err := setup.config.GetCertificate(&tls.ClientHelloInfo{ServerName: "clavis.example.com"})
	if err != nil || cert == nil || cert.Leaf.Issuer.String() != cert.Leaf.Subject.String() {
		t.Fatalf("want the temporary self-signed certificate, got %v %v", cert, err)
	}
	info, err := DescribeTLS(cfg)
	if err != nil || info.Leaf == nil || !strings.Contains(info.Note, "not issued") || info.Leaf.Subject.Organization[0] != TemporaryCertOrganization {
		t.Fatalf("tls info: %+v %v", info, err)
	}
}

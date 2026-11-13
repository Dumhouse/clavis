package server

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme"
)

/*
 * Let's Encrypt with the DNS-01 challenge.
 *
 * The HTTP-01 challenge (autocert) needs Let's Encrypt to reach port 80 from
 * the internet. DNS-01 proves control of the domain with a TXT record instead,
 * so a server that is only reachable on a home or office network still gets a
 * certificate every browser trusts, and wildcard names work.
 *
 * Edge cases handled here:
 *   - HTTPS works immediately: a temporary self-signed certificate is served
 *     until the first issuance succeeds, and failures retry with backoff
 *     (1m, 5m, 15m, then hourly) to stay inside Let's Encrypt rate limits.
 *   - Issued certificates are stored on disk and reused after restarts.
 *   - Renewal starts 30 days before expiry; the old certificate keeps being
 *     served if renewal fails.
 *   - Before asking Let's Encrypt to check, Clavis waits until the zone's
 *     authoritative nameservers return the TXT value (not the local resolver,
 *     which may be a Pi-hole with split DNS or may cache a negative answer).
 *   - Authorizations are solved one at a time and each TXT record is removed
 *     right after, so "example.com" + "*.example.com" (same TXT name) and
 *     DuckDNS (one TXT value per name) both work.
 */

type dnsProvider interface {
	Present(ctx context.Context, fqdn, value string) error
	CleanUp(ctx context.Context, fqdn, value string) error
}

type dnsCertManager struct {
	directory string
	email     string
	domains   []string
	dir       string
	provider  dnsProvider
	resolvers []string
	wait      time.Duration
	logger    *slog.Logger
	// lookupTXT is replaceable in tests.
	lookupTXT func(ctx context.Context, server, name string) ([]string, error)

	mu       sync.Mutex
	cert     *tls.Certificate
	fallback *tls.Certificate
	lastErr  error
}

func (m *dnsCertManager) certPath() string { return filepath.Join(m.dir, "cert.pem") }
func (m *dnsCertManager) keyPath() string  { return filepath.Join(m.dir, "key.pem") }

// GetCertificate serves the issued certificate, or the temporary one while
// the first issuance is still running.
func (m *dnsCertManager) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cert != nil {
		return m.cert, nil
	}
	return m.fallback, nil
}

func (m *dnsCertManager) current() *tls.Certificate {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cert
}

func (m *dnsCertManager) loadFromDisk() {
	cert, err := tls.LoadX509KeyPair(m.certPath(), m.keyPath())
	if err != nil || cert.Leaf == nil || !coversHosts(cert.Leaf, m.domains) {
		return
	}
	m.mu.Lock()
	m.cert = &cert
	m.mu.Unlock()
}

func (m *dnsCertManager) needsRenewal(now time.Time) bool {
	cert := m.current()
	return cert == nil || cert.Leaf == nil || cert.Leaf.NotAfter.Sub(now) < 30*24*time.Hour
}

// run issues and renews until ctx ends.
func (m *dnsCertManager) run(ctx context.Context) {
	backoff := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}
	failures := 0
	for {
		next := 12 * time.Hour
		if m.needsRenewal(time.Now()) {
			m.logger.Info("requesting a Let's Encrypt certificate with the DNS challenge", "domains", m.domains)
			if err := m.obtain(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				m.mu.Lock()
				m.lastErr = err
				m.mu.Unlock()
				next = backoff[min(failures, len(backoff)-1)]
				failures++
				serving := "the temporary self-signed certificate"
				if m.current() != nil {
					serving = "the current certificate"
				}
				m.logger.Error("Let's Encrypt certificate request failed; serving "+serving, "error", err, "retry_in", next.String())
			} else {
				failures = 0
				leaf := m.current().Leaf
				m.logger.Info("Let's Encrypt certificate issued", "domains", m.domains, "expires", leaf.NotAfter.Format(time.RFC3339))
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(next):
		}
	}
}

func (m *dnsCertManager) accountKey() (crypto.Signer, error) {
	path := filepath.Join(m.dir, "account.key")
	if key, err := readECKey(path); err == nil {
		return key, nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	return key, writePEM(path, "EC PRIVATE KEY", der, 0o600)
}

func (m *dnsCertManager) obtain(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return err
	}
	accountKey, err := m.accountKey()
	if err != nil {
		return fmt.Errorf("account key: %w", err)
	}
	client := &acme.Client{Key: accountKey, DirectoryURL: m.directory}
	account := &acme.Account{}
	if m.email != "" {
		account.Contact = []string{"mailto:" + m.email}
	}
	if _, err := client.Register(ctx, account, acme.AcceptTOS); err != nil && !errors.Is(err, acme.ErrAccountAlreadyExists) {
		return fmt.Errorf("register ACME account: %w", err)
	}
	order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(m.domains...))
	if err != nil {
		return fmt.Errorf("create order: %w", err)
	}
	for _, authzURL := range order.AuthzURLs {
		if err := m.solve(ctx, client, authzURL); err != nil {
			return err
		}
	}
	if _, err := client.WaitOrder(ctx, order.URI); err != nil {
		return fmt.Errorf("wait for order: %w", err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{DNSNames: m.domains}, key)
	if err != nil {
		return err
	}
	chain, _, err := client.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return fmt.Errorf("finalize order: %w", err)
	}
	var certPEM []byte
	for _, der := range chain {
		certPEM = append(certPEM, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("issued certificate: %w", err)
	}
	if err := writeFileAtomic(m.keyPath(), keyPEM, 0o600); err != nil {
		return err
	}
	if err := writeFileAtomic(m.certPath(), certPEM, 0o644); err != nil {
		return err
	}
	m.mu.Lock()
	m.cert, m.lastErr = &cert, nil
	m.mu.Unlock()
	return nil
}

func (m *dnsCertManager) solve(ctx context.Context, client *acme.Client, authzURL string) error {
	authz, err := client.GetAuthorization(ctx, authzURL)
	if err != nil {
		return fmt.Errorf("get authorization: %w", err)
	}
	if authz.Status == acme.StatusValid {
		return nil
	}
	var challenge *acme.Challenge
	for _, c := range authz.Challenges {
		if c.Type == "dns-01" {
			challenge = c
		}
	}
	if challenge == nil {
		return fmt.Errorf("%s: the CA offered no dns-01 challenge", authz.Identifier.Value)
	}
	value, err := client.DNS01ChallengeRecord(challenge.Token)
	if err != nil {
		return err
	}
	fqdn := "_acme-challenge." + strings.TrimPrefix(authz.Identifier.Value, "*.") + "."
	if err := m.provider.Present(ctx, fqdn, value); err != nil {
		return fmt.Errorf("create TXT record %s: %w", fqdn, err)
	}
	defer func() {
		cleanCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err := m.provider.CleanUp(cleanCtx, fqdn, value); err != nil {
			m.logger.Warn("could not remove the ACME TXT record; delete it by hand", "name", fqdn, "error", err)
		}
	}()
	if err := m.waitForTXT(ctx, fqdn, value); err != nil {
		// Let's Encrypt may still see it; a failed check costs one attempt.
		m.logger.Warn("TXT record not visible on the nameservers yet; asking Let's Encrypt anyway", "name", fqdn, "error", err)
	}
	if _, err := client.Accept(ctx, challenge); err != nil {
		return fmt.Errorf("accept challenge: %w", err)
	}
	if _, err := client.WaitAuthorization(ctx, authz.URI); err != nil {
		return fmt.Errorf("%s: %w", authz.Identifier.Value, err)
	}
	return nil
}

// waitForTXT polls the zone's authoritative nameservers (or the configured
// resolvers) until every one of them returns value.
func (m *dnsCertManager) waitForTXT(ctx context.Context, fqdn, value string) error {
	servers := m.resolvers
	if len(servers) == 0 {
		servers = authoritativeServers(ctx, strings.TrimSuffix(fqdn, "."))
	}
	if len(servers) == 0 {
		servers = []string{"1.1.1.1:53", "8.8.8.8:53"}
	}
	deadline := time.Now().Add(m.wait)
	for {
		pending := ""
		for _, server := range servers {
			values, err := m.lookupTXT(ctx, server, fqdn)
			if err != nil || !containsString(values, value) {
				pending = server
				break
			}
		}
		if pending == "" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("still missing on %s after %s", pending, m.wait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// authoritativeServers finds the nameservers of the zone that holds name by
// walking up the labels.
func authoritativeServers(ctx context.Context, name string) []string {
	labels := strings.Split(name, ".")
	for i := range labels {
		zone := strings.Join(labels[i:], ".")
		if !strings.Contains(zone, ".") {
			break
		}
		nss, err := net.DefaultResolver.LookupNS(ctx, zone)
		if err != nil || len(nss) == 0 {
			continue
		}
		var out []string
		for _, ns := range nss {
			out = append(out, net.JoinHostPort(strings.TrimSuffix(ns.Host, "."), "53"))
		}
		return out
	}
	return nil
}

func lookupTXTAt(ctx context.Context, server, name string) ([]string, error) {
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, server)
		},
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return resolver.LookupTXT(lookupCtx, name)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Providers

func newDNSProvider(name, token string) (dnsProvider, error) {
	hc := &http.Client{Timeout: 30 * time.Second}
	switch name {
	case "cloudflare":
		return &cloudflareDNS{token: token, base: "https://api.cloudflare.com/client/v4", http: hc, records: map[string]string{}}, nil
	case "duckdns":
		return &duckDNS{token: token, base: "https://www.duckdns.org/update", http: hc}, nil
	}
	return nil, fmt.Errorf("unknown DNS provider %q", name)
}

// cloudflareDNS needs an API token with Zone:DNS:Edit for the zone.
type cloudflareDNS struct {
	token string
	base  string
	http  *http.Client

	mu      sync.Mutex
	records map[string]string // fqdn|value -> record id
}

type cloudflareResponse struct {
	Success bool              `json:"success"`
	Errors  []json.RawMessage `json:"errors"`
	Result  json.RawMessage   `json:"result"`
}

func (c *cloudflareDNS) call(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = strings.NewReader(string(data))
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	var parsed cloudflareResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&parsed); err != nil {
		return fmt.Errorf("cloudflare: %s %s: HTTP %d", method, path, resp.StatusCode)
	}
	if !parsed.Success {
		hint := ""
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
			hint = " (the API token needs Zone → DNS → Edit for this zone)"
		}
		return fmt.Errorf("cloudflare: %s %s: HTTP %d %s%s", method, path, resp.StatusCode, joinRaw(parsed.Errors), hint)
	}
	if out != nil {
		return json.Unmarshal(parsed.Result, out)
	}
	return nil
}

func joinRaw(items []json.RawMessage) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, string(item))
	}
	return strings.Join(parts, ", ")
}

// zoneID finds the zone by trying the name and each parent.
func (c *cloudflareDNS) zoneID(ctx context.Context, fqdn string) (string, error) {
	labels := strings.Split(strings.TrimSuffix(fqdn, "."), ".")
	for i := 1; i < len(labels)-1; i++ {
		zone := strings.Join(labels[i:], ".")
		var zones []struct {
			ID string `json:"id"`
		}
		if err := c.call(ctx, http.MethodGet, "/zones?name="+url.QueryEscape(zone), nil, &zones); err != nil {
			return "", err
		}
		if len(zones) > 0 {
			return zones[0].ID, nil
		}
	}
	return "", fmt.Errorf("cloudflare: no zone for %s in this account (is the domain on Cloudflare, and can the token see it?)", fqdn)
}

func (c *cloudflareDNS) Present(ctx context.Context, fqdn, value string) error {
	zone, err := c.zoneID(ctx, fqdn)
	if err != nil {
		return err
	}
	var record struct {
		ID string `json:"id"`
	}
	body := map[string]any{"type": "TXT", "name": strings.TrimSuffix(fqdn, "."), "content": value, "ttl": 60}
	if err := c.call(ctx, http.MethodPost, "/zones/"+zone+"/dns_records", body, &record); err != nil {
		return err
	}
	c.mu.Lock()
	c.records[fqdn+"|"+value] = zone + "/" + record.ID
	c.mu.Unlock()
	return nil
}

func (c *cloudflareDNS) CleanUp(ctx context.Context, fqdn, value string) error {
	c.mu.Lock()
	ref, ok := c.records[fqdn+"|"+value]
	delete(c.records, fqdn+"|"+value)
	c.mu.Unlock()
	if !ok {
		return nil
	}
	zone, id, _ := strings.Cut(ref, "/")
	return c.call(ctx, http.MethodDelete, "/zones/"+zone+"/dns_records/"+id, nil, nil)
}

// duckDNS sets the single TXT value DuckDNS keeps per subdomain.
type duckDNS struct {
	token string
	base  string
	http  *http.Client
}

func (d *duckDNS) update(ctx context.Context, fqdn string, query url.Values) error {
	name := strings.TrimSuffix(strings.TrimPrefix(fqdn, "_acme-challenge."), ".")
	sub := strings.TrimSuffix(name, ".duckdns.org")
	if i := strings.LastIndex(sub, "."); i >= 0 {
		sub = sub[i+1:] // x.myname.duckdns.org -> myname
	}
	query.Set("domains", sub)
	query.Set("token", d.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.base+"?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if !strings.HasPrefix(strings.TrimSpace(string(body)), "OK") {
		return fmt.Errorf("duckdns: update %s failed (%q); check the token and subdomain", sub, strings.TrimSpace(string(body)))
	}
	return nil
}

func (d *duckDNS) Present(ctx context.Context, fqdn, value string) error {
	return d.update(ctx, fqdn, url.Values{"txt": {value}, "verbose": {"true"}})
}

func (d *duckDNS) CleanUp(ctx context.Context, fqdn, _ string) error {
	return d.update(ctx, fqdn, url.Values{"txt": {""}, "clear": {"true"}})
}

package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"

	"github.com/dumhouse/clavis/internal/config"
)

// tlsSetup is what Run needs to serve HTTPS: the TLS config for the HTTPS
// listener, the handler for the plain-HTTP listener (redirects, health checks,
// ACME challenges), and log fields describing the certificate.
type tlsSetup struct {
	config   *tls.Config
	httpSide http.Handler
	logAttrs []any
	// start runs background work (renewals) until ctx ends; may be nil.
	start func(ctx context.Context)
}

const (
	selfSignedValidity = 397 * 24 * time.Hour // within browser limits for user-trusted certs
	selfSignedRenewal  = 30 * 24 * time.Hour
)

func (s *Server) prepareTLS() (*tlsSetup, error) {
	t := s.cfg.Server.TLS
	base, err := url.Parse(s.cfg.Server.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("server: tls: parse base URL: %w", err)
	}
	redirect := s.httpsRedirect()
	out := &tlsSetup{
		config:   &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: []string{"h2", "http/1.1"}},
		httpSide: redirect,
	}
	switch t.Mode {
	case config.TLSSelfSigned:
		manager, err := newSelfSignedManager(t.Dir, base.Hostname(), s.logger)
		if err != nil {
			return nil, err
		}
		out.config.GetCertificate = manager.GetCertificate
		leaf := manager.leaf()
		out.logAttrs = []any{"tls", t.Mode, "cert_file", manager.certFile,
			"sha256_fingerprint", colonFingerprint(leaf.Raw),
			"public_key_sha256", colonFingerprint(leaf.RawSubjectPublicKeyInfo),
			"trust_on_clients", "clavis trust add " + strings.TrimRight(s.cfg.Server.BaseURL, "/")}
	case config.TLSFile:
		reloader, err := newCertReloader(t.CertFile, t.KeyFile)
		if err != nil {
			return nil, err
		}
		out.config.GetCertificate = reloader.GetCertificate
		out.logAttrs = []any{"tls", t.Mode, "cert_file", t.CertFile}
	case config.TLSACME:
		domains := t.ACMEDomains
		if len(domains) == 0 {
			domains = []string{base.Hostname()}
		}
		directory := t.ACMEDirectory
		if directory == "" {
			directory = acme.LetsEncryptURL
		}
		if t.ACMEDNS != "" {
			return s.prepareACMEDNS(out, domains, directory, base.Hostname())
		}
		if err := os.MkdirAll(filepath.Join(t.Dir, "acme"), 0o700); err != nil {
			return nil, fmt.Errorf("server: tls: create acme cache: %w", err)
		}
		m := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			Cache:      autocert.DirCache(filepath.Join(t.Dir, "acme")),
			HostPolicy: autocert.HostWhitelist(domains...),
			Email:      t.ACMEEmail,
		}
		if t.ACMEDirectory != "" {
			m.Client = &acme.Client{DirectoryURL: t.ACMEDirectory}
		}
		acmeConfig := m.TLSConfig()
		acmeConfig.MinVersion = tls.VersionTLS12
		out.config = acmeConfig
		// HTTP-01 challenges arrive on the plain-HTTP port; everything else redirects.
		out.httpSide = m.HTTPHandler(redirect)
		out.logAttrs = []any{"tls", t.Mode, "challenge", "http-01", "domains", domains, "acme_directory", directory}
	default:
		return nil, fmt.Errorf("server: tls: unsupported mode %q", t.Mode)
	}
	return out, nil
}

// prepareACMEDNS sets up Let's Encrypt with the DNS-01 challenge. Until the
// first certificate arrives, a self-signed one keeps HTTPS working.
func (s *Server) prepareACMEDNS(out *tlsSetup, domains []string, directory, baseHost string) (*tlsSetup, error) {
	t := s.cfg.Server.TLS
	provider, err := newDNSProvider(t.ACMEDNS, t.ACMEDNSToken)
	if err != nil {
		return nil, err
	}
	fallbackHost := strings.TrimPrefix(baseHost, "*.")
	// The stand-in certificate is marked as temporary, so the CLI refuses to
	// pin it: a pin would be useless minutes later.
	fallback, _, err := ensureSelfSignedOrg(filepath.Join(t.Dir, "acme-dns", "temporary.crt"),
		filepath.Join(t.Dir, "acme-dns", "temporary.key"), selfSignedHosts(fallbackHost), time.Now(), TemporaryCertOrganization)
	if err != nil {
		return nil, err
	}
	m := &dnsCertManager{
		directory: directory,
		email:     t.ACMEEmail,
		domains:   domains,
		dir:       filepath.Join(t.Dir, "acme-dns"),
		provider:  provider,
		resolvers: t.ACMEDNSResolvers,
		wait:      t.ACMEDNSWait.Duration(),
		logger:    s.logger,
		lookupTXT: lookupTXTAt,
		fallback:  &fallback,
	}
	if m.wait <= 0 {
		m.wait = 3 * time.Minute
	}
	m.loadFromDisk()
	out.config.GetCertificate = m.GetCertificate
	out.start = m.run
	state := "issued, loaded from disk"
	if m.current() == nil {
		state = "requesting now; a temporary self-signed certificate is served meanwhile"
	}
	out.logAttrs = []any{"tls", t.Mode, "challenge", "dns-01", "dns_provider", t.ACMEDNS, "domains", domains,
		"acme_directory", directory, "certificate", state}
	return out, nil
}

// httpsRedirect answers plain-HTTP requests when Clavis serves HTTPS itself.
// Health probes keep working over HTTP so container and load-balancer checks
// need no certificate; everything else moves to the HTTPS base URL.
func (s *Server) httpsRedirect() http.Handler {
	base := strings.TrimRight(s.cfg.Server.BaseURL, "/")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			s.router.ServeHTTP(w, r)
			return
		}
		// The host is the configured base URL; only the path and query come from the request.
		http.Redirect(w, r, base+r.URL.RequestURI(), http.StatusPermanentRedirect) //nolint:gosec // G710: fixed configured host
	})
}

// selfSignedHosts lists the names a generated certificate covers: the base URL
// host plus loopback names, so the same container works from the host itself.
func selfSignedHosts(baseHost string) []string {
	hosts := []string{"localhost", "127.0.0.1", "::1"}
	if baseHost != "" && !slices.Contains(hosts, baseHost) {
		hosts = append([]string{baseHost}, hosts...)
	}
	return hosts
}

// ensureSelfSigned loads the certificate at certFile, or creates a new one when
// it is missing, expires within 30 days, or does not cover hosts.
func ensureSelfSigned(certFile, keyFile string, hosts []string, now time.Time) (tls.Certificate, string, error) {
	return ensureSelfSignedOrg(certFile, keyFile, hosts, now, selfSignedOrganization)
}

const (
	selfSignedOrganization = "Clavis self-signed"
	// TemporaryCertOrganization marks the certificate served while a Let's
	// Encrypt certificate is being requested. Clients must not pin it.
	TemporaryCertOrganization = "Clavis temporary"
)

func ensureSelfSignedOrg(certFile, keyFile string, hosts []string, now time.Time, org string) (tls.Certificate, string, error) {
	if cert, err := tls.LoadX509KeyPair(certFile, keyFile); err == nil && cert.Leaf != nil {
		if cert.Leaf.NotAfter.Sub(now) > selfSignedRenewal && coversHosts(cert.Leaf, hosts) &&
			slices.Equal(cert.Leaf.Subject.Organization, []string{org}) {
			return cert, fingerprint(cert.Leaf.Raw), nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(certFile), 0o700); err != nil {
		return tls.Certificate{}, "", fmt.Errorf("server: tls: create dir: %w", err)
	}
	// Keep the existing key: clients pin the public key (`clavis trust`), so
	// a renewal or a new host name doesn't make every user trust it again.
	key, err := readECKey(keyFile)
	if err != nil {
		if key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader); err != nil {
			return tls.Certificate{}, "", fmt.Errorf("server: tls: generate key: %w", err)
		}
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("server: tls: serial: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: hosts[0], Organization: []string{org}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(selfSignedValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("server: tls: create certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("server: tls: marshal key: %w", err)
	}
	if err := writePEM(keyFile, "EC PRIVATE KEY", keyDER, 0o600); err != nil {
		return tls.Certificate{}, "", err
	}
	if err := writePEM(certFile, "CERTIFICATE", der, 0o644); err != nil {
		return tls.Certificate{}, "", err
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("server: tls: load generated certificate: %w", err)
	}
	return cert, fingerprint(der), nil
}

func coversHosts(leaf *x509.Certificate, hosts []string) bool {
	for _, h := range hosts {
		if leaf.VerifyHostname(h) != nil {
			return false
		}
	}
	return true
}

func fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// colonFingerprint is the SHA-256 the way browsers, openssl and
// `clavis trust` show it: AB:CD:…
func colonFingerprint(der []byte) string {
	hexDigest := fingerprint(der)
	parts := make([]string, 0, len(hexDigest)/2)
	for i := 0; i+1 < len(hexDigest); i += 2 {
		parts = append(parts, hexDigest[i:i+2])
	}
	return strings.Join(parts, ":")
}

// readECKey loads an existing self-signed key, so it can be reused.
func readECKey(path string) (*ecdsa.PrivateKey, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path is under the operator's TLS dir
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "EC PRIVATE KEY" {
		return nil, errors.New("server: tls: no EC key in " + path)
	}
	return x509.ParseECPrivateKey(block.Bytes)
}

// selfSignedManager serves the generated certificate and renews it while the
// server runs, so an instance that stays up for a year never serves an
// expired certificate.
type selfSignedManager struct {
	certFile, keyFile string
	hosts             []string
	logger            interface{ Info(string, ...any) }

	mu      sync.Mutex
	cert    tls.Certificate
	checked time.Time
}

func newSelfSignedManager(dir, baseHost string, logger interface{ Info(string, ...any) }) (*selfSignedManager, error) {
	m := &selfSignedManager{
		certFile: filepath.Join(dir, "self-signed.crt"),
		keyFile:  filepath.Join(dir, "self-signed.key"),
		hosts:    selfSignedHosts(baseHost),
		logger:   logger,
	}
	cert, _, err := ensureSelfSigned(m.certFile, m.keyFile, m.hosts, time.Now())
	if err != nil {
		return nil, err
	}
	m.cert, m.checked = cert, time.Now()
	return m, nil
}

func (m *selfSignedManager) leaf() *x509.Certificate {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cert.Leaf
}

func (m *selfSignedManager) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if now.Sub(m.checked) > time.Hour {
		m.checked = now
		if m.cert.Leaf != nil && m.cert.Leaf.NotAfter.Sub(now) <= selfSignedRenewal {
			if cert, fp, err := ensureSelfSigned(m.certFile, m.keyFile, m.hosts, now); err == nil {
				m.cert = cert
				m.logger.Info("self-signed certificate renewed (same key, clients keep trusting it)", "sha256_fingerprint", fp)
			}
		}
	}
	return &m.cert, nil
}

func writePEM(path, blockType string, der []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode) //nolint:gosec // G304: path is under the operator's TLS dir
	if err != nil {
		return fmt.Errorf("server: tls: write %s: %w", path, err)
	}
	if err := pem.Encode(f, &pem.Block{Type: blockType, Bytes: der}); err != nil {
		_ = f.Close()
		return fmt.Errorf("server: tls: write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("server: tls: write %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("server: tls: write %s: %w", path, err)
	}
	return nil
}

// certReloader serves a certificate from files and picks up renewals (for
// example from certbot) without a restart. It checks file times at most every
// 30 seconds.
type certReloader struct {
	certFile, keyFile string

	mu      sync.Mutex
	cert    *tls.Certificate
	modTime time.Time
	checked time.Time
}

func newCertReloader(certFile, keyFile string) (*certReloader, error) {
	r := &certReloader{certFile: certFile, keyFile: keyFile}
	if err := r.reload(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *certReloader) reload() error {
	cert, err := tls.LoadX509KeyPair(r.certFile, r.keyFile)
	if err != nil {
		return fmt.Errorf("server: tls: load %s and %s: %w", r.certFile, r.keyFile, err)
	}
	info, err := os.Stat(r.certFile)
	if err != nil {
		return fmt.Errorf("server: tls: stat %s: %w", r.certFile, err)
	}
	r.cert, r.modTime = &cert, info.ModTime()
	return nil
}

func (r *certReloader) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if time.Since(r.checked) > 30*time.Second {
		r.checked = time.Now()
		if info, err := os.Stat(r.certFile); err == nil && !info.ModTime().Equal(r.modTime) {
			// Keep serving the old certificate if the new pair is half-written.
			if err := r.reload(); err != nil && r.cert == nil {
				return nil, err
			}
		}
	}
	if r.cert == nil {
		return nil, errors.New("server: tls: no certificate loaded")
	}
	return r.cert, nil
}

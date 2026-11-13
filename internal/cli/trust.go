package cli

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/dumhouse/clavis/internal/server"
	"golang.org/x/term"
)

/*
 * Trusting self-signed servers.
 *
 * A home-lab server often runs with CLAVIS_TLS=self-signed. Instead of making
 * every user install the certificate in their OS, the CLI works like SSH: the
 * first time it meets a certificate the system doesn't trust, it shows the
 * fingerprint and asks. The answer is remembered per server as the hash of
 * the certificate's public key, which Clavis keeps when it renews a
 * self-signed certificate, so renewals and new IP addresses don't ask again.
 *
 * Non-interactive alternatives, for CI and fleets:
 *   CLAVIS_SERVER_FINGERPRINT  accept exactly this certificate or public key
 *   CLAVIS_CA_CERT             extra PEM roots (an internal CA), added to the system ones
 *   clavis trust add --fingerprint SHA256  save a pin without a prompt
 *
 * A system-trusted certificate always wins; pins only apply when normal
 * verification fails.
 */

type trustedServer struct {
	PublicKey   string    `json:"public_key_sha256"`
	Certificate string    `json:"certificate_sha256"`
	Subject     string    `json:"subject"`
	NotAfter    time.Time `json:"not_after"`
	AddedAt     time.Time `json:"added_at"`
}

func trustFilePath() (string, error) {
	if path := os.Getenv("CLAVIS_TRUST_FILE"); path != "" {
		return path, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "clavis", "trusted-servers.json"), nil
}

func loadTrust() (map[string]trustedServer, error) {
	path, err := trustFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: the user's own config file
	if errors.Is(err, os.ErrNotExist) {
		return map[string]trustedServer{}, nil
	}
	if err != nil {
		return nil, err
	}
	var file struct {
		Servers map[string]trustedServer `json:"servers"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if file.Servers == nil {
		file.Servers = map[string]trustedServer{}
	}
	return file.Servers, nil
}

func saveTrust(servers map[string]trustedServer) error {
	path, err := trustFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(map[string]any{"servers": servers}, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// trustKey is "host:port" for an https server URL, or "" for plain http.
func trustKey(server string) string {
	u, err := url.Parse(server)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return ""
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	return net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}

func certHash(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}

func publicKeyHash(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return hex.EncodeToString(sum[:])
}

// colonHex formats a hex digest the way browsers and openssl show it.
func colonHex(digest string) string {
	digest = strings.ToUpper(digest)
	parts := make([]string, 0, len(digest)/2)
	for i := 0; i+1 < len(digest); i += 2 {
		parts = append(parts, digest[i:i+2])
	}
	return strings.Join(parts, ":")
}

// normalizeFingerprint accepts "AB:CD:…", "abcd…", "sha256:…" or
// "SHA256 Fingerprint=AB:…" and returns lowercase hex.
func normalizeFingerprint(value string) string {
	value = strings.TrimSpace(value)
	if i := strings.LastIndex(value, "="); i >= 0 {
		value = value[i+1:]
	}
	value = strings.TrimPrefix(strings.ToLower(value), "sha256:")
	value = strings.NewReplacer(":", "", " ", "", "-", "").Replace(value)
	return value
}

func fingerprintMatches(cert *x509.Certificate, value string) bool {
	want := normalizeFingerprint(value)
	return len(want) == 64 && (want == certHash(cert) || want == publicKeyHash(cert))
}

// untrustedCertError is returned when a server's certificate is neither
// trusted by the system nor pinned. It carries the certificate so login can
// show it and ask.
type untrustedCertError struct {
	Server    string
	Key       string
	Leaf      *x509.Certificate
	VerifyErr error
	// Pinned is set when this server was trusted before with another key.
	Pinned *trustedServer
	// EnvPin is set when CLAVIS_SERVER_FINGERPRINT did not match.
	EnvPin bool
}

func (e *untrustedCertError) Error() string {
	now := colonHex(certHash(e.Leaf))
	switch {
	case e.EnvPin:
		return fmt.Sprintf("the certificate of %s does not match CLAVIS_SERVER_FINGERPRINT (the server presents SHA-256 %s)", e.Server, now)
	case e.Pinned != nil:
		return fmt.Sprintf("the certificate of %s CHANGED since you trusted it (was %s, now %s). "+
			"If the server got a new key on purpose, run `clavis trust add %s`; otherwise someone may be intercepting the connection",
			e.Server, colonHex(e.Pinned.Certificate), now, e.Server)
	default:
		return fmt.Sprintf("this computer does not trust the certificate of %s (%v). "+
			"Run `clavis trust add %s` to check its fingerprint and trust it, "+
			"or set CLAVIS_SERVER_FINGERPRINT (CI) or CLAVIS_CA_CERT (an internal CA)",
			e.Server, e.VerifyErr, e.Server)
	}
}

// extraRoots returns the system roots plus CLAVIS_CA_CERT files (separated by
// the OS path list separator), or nil to use the system roots alone.
func extraRoots() (*x509.CertPool, error) {
	files := os.Getenv("CLAVIS_CA_CERT")
	if strings.TrimSpace(files) == "" {
		return nil, nil
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	for _, file := range filepath.SplitList(files) {
		if file = strings.TrimSpace(file); file == "" {
			continue
		}
		data, err := os.ReadFile(file) //nolint:gosec // G304: a CA file the user points at
		if err != nil {
			return nil, fmt.Errorf("CLAVIS_CA_CERT: %w", err)
		}
		if !pool.AppendCertsFromPEM(data) {
			return nil, fmt.Errorf("CLAVIS_CA_CERT: %s has no PEM certificates", file)
		}
	}
	return pool, nil
}

func verifyServerCert(server string, state tls.ConnectionState, roots *x509.CertPool) error {
	if len(state.PeerCertificates) == 0 {
		return errors.New("server sent no certificate")
	}
	leaf := state.PeerCertificates[0]
	intermediates := x509.NewCertPool()
	for _, cert := range state.PeerCertificates[1:] {
		intermediates.AddCert(cert)
	}
	// The host comes from the URL: for IP addresses Go leaves
	// state.ServerName empty, which would skip the name check.
	host := state.ServerName
	if u, err := url.Parse(server); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	}
	_, verifyErr := leaf.Verify(x509.VerifyOptions{
		DNSName:       host,
		Roots:         roots,
		Intermediates: intermediates,
	})
	if verifyErr == nil {
		return nil
	}
	key := trustKey(server)
	if pin := os.Getenv("CLAVIS_SERVER_FINGERPRINT"); pin != "" {
		if fingerprintMatches(leaf, pin) {
			return nil
		}
		return &untrustedCertError{Server: server, Key: key, Leaf: leaf, VerifyErr: verifyErr, EnvPin: true}
	}
	pins, err := loadTrust()
	if err != nil {
		return fmt.Errorf("%w (and the trusted servers file could not be read: %v)", verifyErr, err)
	}
	if pinned, ok := pins[key]; ok {
		if pinned.PublicKey == publicKeyHash(leaf) || pinned.Certificate == certHash(leaf) {
			return nil
		}
		return &untrustedCertError{Server: server, Key: key, Leaf: leaf, VerifyErr: verifyErr, Pinned: &pinned}
	}
	return &untrustedCertError{Server: server, Key: key, Leaf: leaf, VerifyErr: verifyErr}
}

// httpClientFor returns an HTTP client for one Clavis server that accepts
// system-trusted certificates, CLAVIS_CA_CERT roots, and pinned self-signed
// certificates. A zero timeout means none (event streams).
func httpClientFor(server string, timeout time.Duration) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if trustKey(server) != "" {
		roots, err := extraRoots()
		if err != nil {
			return nil, err
		}
		transport.TLSClientConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			// Verification is not skipped: VerifyConnection does the standard
			// chain and hostname check first, then accepts only a pinned key.
			InsecureSkipVerify: true, //nolint:gosec // G402: replaced by VerifyConnection below
			VerifyConnection: func(state tls.ConnectionState) error {
				return verifyServerCert(server, state, roots)
			},
		}
	}
	return &http.Client{Transport: transport, Timeout: timeout}, nil
}

// probeServerTLS connects to the server and returns an *untrustedCertError if
// its certificate is not trusted. Other connection problems return nil: the
// command that follows reports them in context.
func probeServerTLS(ctx context.Context, server string) *untrustedCertError {
	if trustKey(server) == "" {
		return nil
	}
	hc, err := httpClientFor(server, 15*time.Second)
	if err != nil {
		return nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(server, "/")+"/healthz", nil)
	if err != nil {
		return nil
	}
	response, err := hc.Do(request)
	if err == nil {
		_ = response.Body.Close()
		return nil
	}
	var untrusted *untrustedCertError
	if errors.As(err, &untrusted) {
		return untrusted
	}
	return nil
}

// describeCert prints what a person needs to decide whether to trust it.
func describeCert(out *Output, e *untrustedCertError, host string) {
	leaf := e.Leaf
	names := append([]string{}, leaf.DNSNames...)
	for _, ip := range leaf.IPAddresses {
		names = append(names, ip.String())
	}
	issuer := leaf.Issuer.String()
	if leaf.Issuer.String() == leaf.Subject.String() {
		issuer = "itself (self-signed)"
	}
	out.Info("  Server        %s", e.Server)
	out.Info("  Issued to     %s", strings.Join(names, ", "))
	out.Info("  Issued by     %s", issuer)
	out.Info("  Valid until   %s", leaf.NotAfter.Format("2006-01-02"))
	out.Info("  SHA-256       %s", colonHex(certHash(leaf)))
	out.Info("")
	if time.Now().After(leaf.NotAfter) {
		out.Warn("This certificate EXPIRED on %s.", leaf.NotAfter.Format("2006-01-02"))
	}
	if leaf.VerifyHostname(host) != nil {
		out.Warn("This certificate is for %s, not for %s. If one of those names reaches the server, use it instead.", strings.Join(names, ", "), host)
	}
}

// confirmTrust explains an untrusted certificate and, on a terminal, asks
// whether to trust it. It saves the answer and returns nil when trusted.
func confirmTrust(out *Output, e *untrustedCertError) error {
	if e.EnvPin {
		return e
	}
	if isTemporaryCert(e.Leaf) {
		return fmt.Errorf("%s is still getting its Let's Encrypt certificate and serves a temporary one meanwhile; "+
			"try again in a minute. If it doesn't clear up, the server log or `clavis admin tls-info` says why", e.Server)
	}
	interactive := term.IsTerminal(int(os.Stdin.Fd()))
	host := e.Leaf.Subject.CommonName
	if u, err := url.Parse(e.Server); err == nil {
		host = u.Hostname()
	}
	if e.Pinned != nil {
		out.Warn("WARNING: the certificate of %s has CHANGED since you trusted it.", e.Server)
		out.Info("")
		out.Info("  Trusted       %s", colonHex(e.Pinned.Certificate))
		out.Info("  Now           %s", colonHex(certHash(e.Leaf)))
		out.Info("")
		out.Info("This is expected only if the server was reinstalled or its key was replaced.")
		out.Info("Otherwise someone may be intercepting the connection: stop and ask whoever runs the server.")
		out.Info("")
	} else {
		out.Info("This computer does not trust the certificate of %s.", e.Server)
		out.Info("That is normal for a Clavis server with a self-signed certificate.")
		out.Info("")
	}
	describeCert(out, e, host)
	out.Info("Compare the SHA-256 with the one the server shows: on the server run")
	out.Info("  docker exec clavis clavis admin tls-info")
	out.Info("or open the server in a browser and view the certificate details.")
	out.Info("")
	if !interactive {
		if e.Pinned != nil {
			return e
		}
		return fmt.Errorf("not trusted, and there is no terminal to ask: after comparing the fingerprint, run "+
			"`clavis trust add %s --fingerprint %s`, or set CLAVIS_SERVER_FINGERPRINT", e.Server, colonHex(certHash(e.Leaf)))
	}
	if e.Pinned != nil {
		line, err := promptLine(`Type "trust" to replace the old certificate, anything else to stop: `)
		if err != nil || line != "trust" {
			return errors.New("certificate not trusted")
		}
	} else {
		ok, err := confirm("Do the fingerprints match, and do you trust this server?")
		if err != nil || !ok {
			return errors.New("certificate not trusted. Nothing was saved")
		}
	}
	if err := pinCertificate(e.Key, e.Leaf); err != nil {
		return err
	}
	path, _ := trustFilePath()
	out.Success("Trusted %s (saved in %s).", e.Server, path)
	return nil
}

// isTemporaryCert reports the stand-in certificate a server serves while it
// waits for Let's Encrypt (server.TemporaryCertOrganization). Pinning it
// would be pointless: it is replaced within minutes.
func isTemporaryCert(leaf *x509.Certificate) bool {
	return leaf.Issuer.String() == leaf.Subject.String() && slices.Contains(leaf.Subject.Organization, server.TemporaryCertOrganization)
}

func pinCertificate(key string, leaf *x509.Certificate) error {
	pins, err := loadTrust()
	if err != nil {
		return err
	}
	pins[key] = trustedServer{
		PublicKey:   publicKeyHash(leaf),
		Certificate: certHash(leaf),
		Subject:     leaf.Subject.String(),
		NotAfter:    leaf.NotAfter,
		AddedAt:     time.Now().UTC(),
	}
	return saveTrust(pins)
}

// ensureServerTrusted runs before login: it asks about an untrusted
// certificate once, instead of failing with an x509 error.
func ensureServerTrusted(ctx context.Context, out *Output, server string) error {
	untrusted := probeServerTLS(ctx, server)
	if untrusted == nil {
		return nil
	}
	return confirmTrust(out, untrusted)
}

func sortedTrustKeys(pins map[string]trustedServer) []string {
	keys := make([]string, 0, len(pins))
	for key := range pins {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

package server

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/url"
	"os"
	"path/filepath"

	"github.com/dumhouse/clavis/internal/config"
)

// TLSInfo describes the certificate the server presents, for
// `clavis admin tls-info`.
type TLSInfo struct {
	Mode     string
	BaseURL  string
	CertFile string
	Leaf     *x509.Certificate
	// Note explains a special state (plain HTTP, not issued yet, ...).
	Note string
}

// Fingerprint is the certificate SHA-256 as browsers show it.
func (i *TLSInfo) Fingerprint() string { return colonFingerprint(i.Leaf.Raw) }

// PublicKeyFingerprint is the SHA-256 of the public key, which stays the same
// across self-signed renewals.
func (i *TLSInfo) PublicKeyFingerprint() string {
	return colonFingerprint(i.Leaf.RawSubjectPublicKeyInfo)
}

// DescribeTLS reads the certificate the server is configured to serve.
func DescribeTLS(cfg *config.Config) (*TLSInfo, error) {
	t := cfg.Server.TLS
	info := &TLSInfo{Mode: t.Mode, BaseURL: cfg.Server.BaseURL}
	selfSigned := filepath.Join(t.Dir, "self-signed.crt")
	switch t.Mode {
	case config.TLSOff, "":
		info.Note = "Clavis serves plain HTTP (CLAVIS_TLS=off). If a reverse proxy does HTTPS, clients see the proxy's certificate."
		return info, nil
	case config.TLSSelfSigned:
		info.CertFile = selfSigned
	case config.TLSFile:
		info.CertFile = t.CertFile
	case config.TLSACME:
		if t.ACMEDNS != "" {
			info.CertFile = filepath.Join(t.Dir, "acme-dns", "cert.pem")
			if _, err := os.Stat(info.CertFile); err != nil {
				info.CertFile = filepath.Join(t.Dir, "acme-dns", "temporary.crt")
				info.Note = "Let's Encrypt has not issued a certificate yet (see the server log); a temporary self-signed certificate is served. Clients wait for the real one instead of trusting it."
			}
			break
		}
		host := ""
		if u, err := url.Parse(cfg.Server.BaseURL); err == nil {
			host = u.Hostname()
		}
		if len(t.ACMEDomains) > 0 {
			host = t.ACMEDomains[0]
		}
		info.CertFile = filepath.Join(t.Dir, "acme", host)
	}
	data, err := os.ReadFile(info.CertFile) //nolint:gosec // G304: the configured certificate path
	if err != nil {
		if info.Mode == config.TLSACME && errors.Is(err, os.ErrNotExist) {
			info.Note = "Let's Encrypt issues the certificate on the first HTTPS visit; open the server once, then run this again."
			info.CertFile = ""
			return info, nil
		}
		return nil, err
	}
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			return nil, errors.New("no certificate in " + info.CertFile)
		}
		if block.Type == "CERTIFICATE" {
			if info.Leaf, err = x509.ParseCertificate(block.Bytes); err != nil {
				return nil, err
			}
			return info, nil
		}
	}
}

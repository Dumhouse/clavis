// Package netguard provides the outbound HTTP client used by webhooks, syncs,
// and OIDC discovery. It blocks private, loopback, link-local, CGNAT, and
// metadata addresses after DNS resolution, with an optional CIDR allowlist for
// deliberate internal targets.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// ErrBlocked is returned when a destination resolves only to blocked addresses.
var ErrBlocked = errors.New("netguard: destination address is blocked")

var alwaysBlocked = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),  // IETF protocol assignments
	netip.MustParsePrefix("198.18.0.0/15"), // benchmarking
	netip.MustParsePrefix("240.0.0.0/4"),   // reserved
	netip.MustParsePrefix("2001:db8::/32"), // documentation
	netip.MustParsePrefix("64:ff9b::/96"),  // NAT64
}

// Guardian builds SSRF-safe clients.
type Guardian struct {
	allow    []netip.Prefix
	resolver *net.Resolver
	dialer   net.Dialer
}

// New parses an allowlist of IPs/CIDRs. Entries permit otherwise-blocked
// destinations.
func New(allowlist []string) (*Guardian, error) {
	g := &Guardian{
		resolver: net.DefaultResolver,
		dialer:   net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second},
	}
	for _, entry := range allowlist {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			addr, addrErr := netip.ParseAddr(entry)
			if addrErr != nil {
				return nil, fmt.Errorf("netguard: invalid allowlist entry %q", entry)
			}
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		g.allow = append(g.allow, prefix.Masked())
	}
	return g, nil
}

// Client returns an HTTP client that enforces the policy on every connection,
// including redirects.
func (g *Guardian) Client() *http.Client {
	transport := &http.Transport{
		Proxy:                 nil, // a proxy would bypass address checks
		DialContext:           g.dialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          50,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   60 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("netguard: too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errors.New("netguard: redirect to unsupported scheme")
			}
			return nil
		},
	}
}

func (g *Guardian) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("netguard: invalid address %q: %w", address, err)
	}
	addresses, err := g.resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, addr := range addresses {
		if g.blocked(addr) {
			continue
		}
		conn, err := g.dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("%w: %s is a private or reserved address; to allow it, add it to security.outbound_allowlist (CLAVIS_OUTBOUND_ALLOWLIST)", ErrBlocked, host)
}

func (g *Guardian) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	if addr, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{addr.Unmap()}, nil
	}
	addrs, err := g.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("netguard: resolve %s: %w", host, err)
	}
	out := make([]netip.Addr, 0, len(addrs))
	for _, addr := range addrs {
		out = append(out, addr.Unmap())
	}
	return out, nil
}

func (g *Guardian) blocked(addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, prefix := range g.allow {
		if prefix.Contains(addr) {
			return false
		}
	}
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return true
	}
	for _, prefix := range alwaysBlocked {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// CheckURL resolves a URL and reports whether the policy would allow it.
func (g *Guardian) CheckURL(ctx context.Context, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return fmt.Errorf("netguard: invalid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("netguard: unsupported scheme %q", u.Scheme)
	}
	addresses, err := g.resolve(ctx, u.Hostname())
	if err != nil {
		return err
	}
	for _, addr := range addresses {
		if !g.blocked(addr) {
			return nil
		}
	}
	return ErrBlocked
}

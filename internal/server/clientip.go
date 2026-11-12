package server

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/dumhouse/clavis/internal/config"
)

// clientIPResolver rewrites r.RemoteAddr to the real client address, but only
// when the request arrives from a trusted proxy. Unlike chi's RealIP, a client
// that talks to the server directly cannot spoof its address with headers.
type clientIPResolver struct {
	trusted []netip.Prefix
}

func newClientIPResolver(entries []string) (*clientIPResolver, error) {
	r := &clientIPResolver{}
	for _, e := range entries {
		p, err := config.ParseProxyPrefix(e)
		if err != nil {
			return nil, err
		}
		r.trusted = append(r.trusted, p)
	}
	return r, nil
}

func (c *clientIPResolver) isTrusted(addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, p := range c.trusted {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func (c *clientIPResolver) middleware(next http.Handler) http.Handler {
	if len(c.trusted) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ip := c.resolve(r); ip != "" {
			r.RemoteAddr = ip
		}
		next.ServeHTTP(w, r)
	})
}

// resolve returns the client IP, or "" to keep the socket peer. It walks
// X-Forwarded-For from the right, skipping trusted proxies, and returns the
// first untrusted hop. Falls back to X-Real-IP.
func (c *clientIPResolver) resolve(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil || !c.isTrusted(peer) {
		return ""
	}
	if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
		hops := strings.Split(strings.Join(xff, ","), ",")
		for i := len(hops) - 1; i >= 0; i-- {
			hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
			if err != nil {
				return ""
			}
			if !c.isTrusted(hop) {
				return hop.Unmap().String()
			}
		}
	}
	if realIP, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("X-Real-IP"))); err == nil {
		return realIP.Unmap().String()
	}
	return ""
}

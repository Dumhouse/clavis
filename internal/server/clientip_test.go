package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIPResolver(t *testing.T) {
	cases := []struct {
		name, remote, xff, realIP, want string
	}{
		{"untrusted peer ignores headers", "203.0.113.9:1234", "1.2.3.4", "5.6.7.8", "203.0.113.9:1234"},
		{"trusted peer uses xff", "10.0.0.2:1234", "198.51.100.7", "", "198.51.100.7"},
		{"spoofed left hop ignored", "10.0.0.2:1234", "1.2.3.4, 198.51.100.7", "", "198.51.100.7"},
		{"trusted chain skipped", "10.0.0.2:1234", "198.51.100.7, 10.0.0.9", "", "198.51.100.7"},
		{"falls back to x-real-ip", "10.0.0.2:1234", "", "198.51.100.8", "198.51.100.8"},
		{"garbage xff keeps peer", "10.0.0.2:1234", "nonsense", "", "10.0.0.2:1234"},
		{"no headers keeps peer", "10.0.0.2:1234", "", "", "10.0.0.2:1234"},
	}
	resolver, err := newClientIPResolver([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			h := resolver.middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.RemoteAddr }))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.remote
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			if tc.realIP != "" {
				req.Header.Set("X-Real-IP", tc.realIP)
			}
			h.ServeHTTP(httptest.NewRecorder(), req)
			if got != tc.want {
				t.Fatalf("RemoteAddr = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClientIPResolverDisabledWithoutProxies(t *testing.T) {
	resolver, err := newClientIPResolver(nil)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	h := resolver.middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.RemoteAddr }))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.2:1"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if got != "10.0.0.2:1" {
		t.Fatalf("headers were honored without trusted proxies: %q", got)
	}
}

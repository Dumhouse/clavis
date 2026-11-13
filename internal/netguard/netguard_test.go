package netguard

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestBlockedAddresses(t *testing.T) {
	g, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	blocked := []string{
		"127.0.0.1", "10.0.0.1", "172.16.5.4", "192.168.1.1",
		"169.254.169.254", "100.64.0.1", "0.0.0.0", "192.0.0.1",
		"::1", "fd00::1", "fe80::1", "2001:db8::1",
	}
	for _, raw := range blocked {
		if !g.blocked(netip.MustParseAddr(raw)) {
			t.Errorf("%s should be blocked", raw)
		}
	}
	allowed := []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"}
	for _, raw := range allowed {
		if g.blocked(netip.MustParseAddr(raw)) {
			t.Errorf("%s should be allowed", raw)
		}
	}
}

func TestAllowlistOverridesBlocked(t *testing.T) {
	g, err := New([]string{"127.0.0.1/32", "10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	if g.blocked(netip.MustParseAddr("127.0.0.1")) {
		t.Fatal("allowlisted loopback blocked")
	}
	if g.blocked(netip.MustParseAddr("10.1.2.3")) {
		t.Fatal("allowlisted private address blocked")
	}
	if !g.blocked(netip.MustParseAddr("192.168.1.1")) {
		t.Fatal("non-allowlisted private address allowed")
	}
}

func TestClientBlocksLoopbackByDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("should not reach"))
	}))
	defer server.Close()

	g, _ := New(nil)
	client := g.Client()
	resp, err := client.Get(server.URL)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("loopback request succeeded")
	}
}

func TestClientAllowsExplicitLoopback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	g, _ := New([]string{"127.0.0.1/32"})
	resp, err := g.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestRedirectToBlockedAddressFails(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data", http.StatusFound)
	}))
	defer target.Close()

	g, _ := New([]string{"127.0.0.1/32"})
	resp, err := g.Client().Get(target.URL)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("redirect to metadata succeeded")
	}
}

func TestCheckURL(t *testing.T) {
	g, _ := New([]string{"127.0.0.1/32"})
	if err := g.CheckURL(context.Background(), "http://127.0.0.1:8080/hook"); err != nil {
		t.Fatal(err)
	}
	if err := g.CheckURL(context.Background(), "http://169.254.169.254/"); err == nil {
		t.Fatal("metadata URL accepted")
	}
	if err := g.CheckURL(context.Background(), "file:///etc/passwd"); err == nil {
		t.Fatal("file scheme accepted")
	}
	if err := g.CheckURL(context.Background(), "http://8.8.8.8/"); err != nil {
		t.Fatal(err)
	}
}

func TestNewRejectsInvalidAllowlist(t *testing.T) {
	if _, err := New([]string{"not-an-ip"}); err == nil {
		t.Fatal("invalid allowlist accepted")
	}
	if _, err := New([]string{""}); err != nil {
		t.Fatalf("empty entry: %v", err)
	}
}

func TestBlockedErrorExplainsTheFix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()
	g, _ := New(nil)
	_, err := g.Client().Get(server.URL)
	if !errors.Is(err, ErrBlocked) || !strings.Contains(err.Error(), "outbound_allowlist") {
		t.Fatalf("error = %v", err)
	}
}

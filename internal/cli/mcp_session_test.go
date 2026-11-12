package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dumhouse/clavis/pkg/client"
)

func TestMCPAgentSessionStartsAndRenews(t *testing.T) {
	var started atomic.Int32
	expires := time.Now().Add(time.Hour)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		auth := r.Header.Get("Authorization")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/orgs/acme/identities/claude-code/sessions":
			if auth != "Bearer human-login" {
				http.Error(w, `{"status":401}`, http.StatusUnauthorized)
				return
			}
			n := started.Add(1)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token": fmt.Sprintf("clv_at_session%d", n), "expires_at": expires,
			})
		case strings.HasSuffix(r.URL.Path, "/secrets:agent"):
			if !strings.HasPrefix(auth, "Bearer clv_at_session") {
				http.Error(w, `{"status":401}`, http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"secrets": []map[string]any{
				{"key": "AGENT_KEY", "value": "super-secret-value", "kind": "variable"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	human, err := client.New(server.URL, client.WithToken("human-login"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	source := newAgentSessionSource(human, server.URL, "acme", "claude-code")
	source.now = func() time.Time { return now }
	mcp := &mcpServer{
		resolved: &resolvedContext{Server: server.URL, Org: "acme", Project: "web-app", Env: "dev"},
		session:  source,
	}

	run := func() *toolResult {
		t.Helper()
		result, err := mcp.callTool(context.Background(), "run_with_secrets", json.RawMessage(
			`{"command":"sh","args":["-c","echo $AGENT_KEY"]}`))
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	if result := run(); result.IsError || strings.Contains(result.Content[0].Text, "super-secret-value") {
		t.Fatalf("run = %+v", result)
	}
	run()
	if got := started.Load(); got != 1 {
		t.Fatalf("sessions started = %d, want 1 (reuse while fresh)", got)
	}

	// Close to expiry, the next call renews the session with the person's login.
	now = expires.Add(-agentSessionRenewBefore + time.Second)
	expires = expires.Add(time.Hour)
	run()
	if got := started.Load(); got != 2 {
		t.Fatalf("sessions started = %d, want 2 after renewal", got)
	}
}

func TestMCPAgentSessionReportsLoginErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"status":403,"title":"Forbidden","detail":"you can only start an agent with access you hold yourself"}`))
	}))
	defer server.Close()
	human, err := client.New(server.URL, client.WithToken("human-login"), client.WithRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	_, err = newAgentSessionSource(human, server.URL, "acme", "claude-code").client(context.Background())
	if err == nil || !strings.Contains(err.Error(), `"claude-code"`) {
		t.Fatalf("err = %v", err)
	}
}

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dumhouse/clavis/pkg/client"
)

func TestMCPTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/orgs/acme/projects/web-app/schema":
			_ = json.NewEncoder(w).Encode(map[string]any{"schema": []map[string]any{
				{"key": "AGENT_KEY", "type": "string", "kind": "variable", "required": map[string]bool{"production": true}},
			}})
		case r.URL.Path == "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets":
			_ = json.NewEncoder(w).Encode(map[string]any{"secrets": []map[string]any{
				{"key": "AGENT_KEY", "value": "super-secret-value", "kind": "variable"},
			}})
		case r.URL.Path == "/api/v1/orgs/acme/projects/web-app":
			_ = json.NewEncoder(w).Encode(map[string]any{"slug": "web-app", "environments": []map[string]string{
				{"slug": "dev", "type": "development"},
			}})
		case strings.HasSuffix(r.URL.Path, "/secrets:agent"):
			_ = json.NewEncoder(w).Encode(map[string]any{"secrets": []map[string]any{
				{"key": "AGENT_KEY", "value": "super-secret-value", "kind": "variable"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c, err := client.New(server.URL, client.WithToken("clv_at_test"))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	mcp := &mcpServer{
		resolved: &resolvedContext{Server: server.URL, Org: "acme", Project: "web-app", Env: "dev", Token: "clv_at_test"},
		client:   c,
		encoder:  json.NewEncoder(&output),
	}
	call := func(name string, args any) *toolResult {
		raw, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		result, err := mcp.callTool(context.Background(), name, raw)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return result
	}
	listed := call("list_secrets", map[string]any{})
	if listed.IsError || !strings.Contains(listed.Content[0].Text, "AGENT_KEY") ||
		strings.Contains(listed.Content[0].Text, "super-secret-value") {
		t.Fatalf("list_secrets = %+v", listed)
	}
	checked := call("check_env", map[string]any{})
	if checked.IsError || !strings.Contains(checked.Content[0].Text, `"ok": true`) {
		t.Fatalf("check_env = %+v", checked)
	}
	ran := call("run_with_secrets", map[string]any{
		"command": "sh", "args": []string{"-c", "echo $AGENT_KEY"},
	})
	if ran.IsError || !strings.Contains(ran.Content[0].Text, "••••••") ||
		strings.Contains(ran.Content[0].Text, "super-secret-value") {
		t.Fatalf("run_with_secrets = %+v", ran)
	}

	// Protocol handshake and tools/list are valid JSON-RPC.
	input := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n" +
			`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")
	output.Reset()
	if err := mcp.serve(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"protocolVersion"`) ||
		!strings.Contains(lines[1], "run_with_secrets") {
		t.Fatalf("mcp protocol output = %q", output.String())
	}
}

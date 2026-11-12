package cli

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/dumhouse/clavis/internal/version"
	"github.com/dumhouse/clavis/pkg/client"
)

// agentSessionRenewBefore renews the agent session this long before it expires.
const agentSessionRenewBefore = 5 * time.Minute

// agentSessionSource holds a delegated agent session in memory. It is started
// and renewed with the person's own login, so no agent token is ever written
// to an MCP config file the agent could read.
type agentSessionSource struct {
	mu      sync.Mutex
	human   *client.Client
	server  string
	org     string
	agent   string
	now     func() time.Time
	current *client.Client
	expires time.Time
}

func newAgentSessionSource(human *client.Client, server, org, agent string) *agentSessionSource {
	return &agentSessionSource{human: human, server: server, org: org, agent: agent, now: time.Now}
}

// client returns a client for the current agent session, starting or renewing
// the session when it is missing or about to expire.
func (a *agentSessionSource) client(ctx context.Context) (*client.Client, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.current != nil && a.now().Add(agentSessionRenewBefore).Before(a.expires) {
		return a.current, nil
	}
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	path := fmt.Sprintf("/api/v1/orgs/%s/identities/%s/sessions", url.PathEscape(a.org), url.PathEscape(a.agent))
	if err := a.human.Post(ctx, path, nil, &out); err != nil {
		return nil, fmt.Errorf("start agent session for %q: %w", a.agent, err)
	}
	c, err := client.New(a.server,
		client.WithToken(out.Token),
		client.WithUserAgent("clavis-mcp/"+version.Version),
	)
	if err != nil {
		return nil, err
	}
	a.current, a.expires = c, out.ExpiresAt
	return c, nil
}

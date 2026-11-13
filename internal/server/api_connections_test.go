package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dumhouse/clavis/internal/config"
)

// fakeForgejo accepts the given tokens and records the token behind each
// secret write.
type fakeForgejo struct {
	mu        sync.Mutex
	valid     map[string]bool
	lastToken string
	secrets   map[string]string
}

func newFakeForgejo(t *testing.T, tokens ...string) (*fakeForgejo, *httptest.Server) {
	t.Helper()
	fake := &fakeForgejo{valid: map[string]bool{}, secrets: map[string]string{}}
	for _, token := range tokens {
		fake.valid[token] = true
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "token ")
		if !fake.valid[token] {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		const prefix = "/api/v1/repos/team/app/actions/"
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/user":
			_, _ = w.Write([]byte(`{"login":"sync-bot"}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, prefix):
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, prefix+"secrets/"):
			var body struct {
				Data string `json:"data"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			fake.secrets[strings.TrimPrefix(r.URL.Path, prefix+"secrets/")] = body.Data
			fake.lastToken = token
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return fake, srv
}

func (f *fakeForgejo) token() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastToken
}

type connectionResponse struct {
	ID        string `json:"id"`
	Provider  string `json:"provider"`
	Name      string `json:"name"`
	BaseURL   string `json:"base_url"`
	Auth      string `json:"auth"`
	SyncCount int    `json:"sync_count"`
	Syncs     []struct {
		SyncID      string `json:"sync_id"`
		Project     string `json:"project"`
		Environment string `json:"environment"`
	} `json:"syncs"`
}

func TestSyncConnections(t *testing.T) {
	fake, srv := newFakeForgejo(t, "fj-one", "fj-two")
	f := newFixture(t)
	owner, _ := f.setup(t)
	const connections = "/api/v1/orgs/acme/connections"
	const syncs = "/api/v1/orgs/acme/projects/web-app/syncs"

	created := owner.request(http.MethodPost, connections, map[string]any{
		"provider": "forgejo", "name": "Home Forgejo", "base_url": srv.URL + "/api/v1/", "token": "fj-one",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create connection = %d body=%s", created.Code, created.Body)
	}
	if strings.Contains(created.Body.String(), "fj-one") {
		t.Fatal("connection response leaked the token")
	}
	conn := decode[connectionResponse](t, created)
	if conn.BaseURL != srv.URL || conn.Auth != "token" || conn.Provider != "forgejo" {
		t.Fatalf("connection = %+v", conn)
	}
	if dup := owner.request(http.MethodPost, connections, map[string]any{
		"provider": "forgejo", "name": "Home Forgejo", "base_url": srv.URL, "token": "fj-one",
	}); dup.Code != http.StatusUnprocessableEntity {
		t.Fatalf("duplicate name = %d body=%s", dup.Code, dup.Body)
	}
	if hook := owner.request(http.MethodPost, connections, map[string]any{
		"provider": "http", "name": "Hook", "token": "x",
	}); hook.Code != http.StatusUnprocessableEntity && hook.Code != http.StatusBadRequest {
		t.Fatalf("http connection = %d body=%s", hook.Code, hook.Body)
	}

	test := owner.request(http.MethodPost, connections+"/"+conn.ID+"/test", nil)
	if test.Code != http.StatusOK || !strings.Contains(test.Body.String(), `"ok":true`) ||
		!strings.Contains(test.Body.String(), "sync-bot") {
		t.Fatalf("test connection = %d body=%s", test.Code, test.Body)
	}

	// A sync that uses the connection needs only the destination.
	if both := owner.request(http.MethodPost, syncs, map[string]any{
		"environment": "dev", "provider": "forgejo", "forgejo_repo": "team/app",
		"connection_id": conn.ID, "token": "fj-one",
	}); both.Code != http.StatusUnprocessableEntity {
		t.Fatalf("connection plus token = %d body=%s", both.Code, both.Body)
	}
	if wrong := owner.request(http.MethodPost, syncs, map[string]any{
		"environment": "dev", "provider": "gitlab", "gitlab_project": "team/app", "connection_id": conn.ID,
	}); wrong.Code != http.StatusUnprocessableEntity {
		t.Fatalf("provider mismatch = %d body=%s", wrong.Code, wrong.Body)
	}
	syncRec := owner.request(http.MethodPost, syncs, map[string]any{
		"environment": "dev", "provider": "forgejo", "forgejo_repo": "team/app", "connection_id": conn.ID,
	})
	if syncRec.Code != http.StatusCreated {
		t.Fatalf("create sync = %d body=%s", syncRec.Code, syncRec.Body)
	}
	shared := decode[struct {
		ID           string `json:"id"`
		ConnectionID string `json:"connection_id"`
	}](t, syncRec)
	if shared.ConnectionID != conn.ID {
		t.Fatalf("sync connection_id = %q", shared.ConnectionID)
	}
	if rec := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets",
		map[string]any{"key": "API_KEY", "value": "v1"}); rec.Code != http.StatusCreated {
		t.Fatalf("secret = %d", rec.Code)
	}
	run := func(id string) {
		t.Helper()
		rec := owner.request(http.MethodPost, syncs+"/"+id+"/run", map[string]any{"confirm": true})
		if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"status":"success"`) {
			t.Fatalf("run = %d body=%s", rec.Code, rec.Body)
		}
	}
	run(shared.ID)
	if got := fake.token(); got != "fj-one" {
		t.Fatalf("push used token %q", got)
	}

	// Rotating the connection reaches every sync that uses it.
	rotate := owner.request(http.MethodPatch, connections+"/"+conn.ID, map[string]any{"token": "fj-two"})
	if rotate.Code != http.StatusOK || strings.Contains(rotate.Body.String(), "fj-two") {
		t.Fatalf("rotate = %d body=%s", rotate.Code, rotate.Body)
	}
	run(shared.ID)
	if got := fake.token(); got != "fj-two" {
		t.Fatalf("push after rotation used token %q", got)
	}

	list := owner.request(http.MethodGet, connections+"?provider=forgejo", nil)
	listed := decode[struct {
		Connections []connectionResponse `json:"connections"`
	}](t, list)
	if len(listed.Connections) != 1 || listed.Connections[0].SyncCount != 1 ||
		len(listed.Connections[0].Syncs) != 1 || listed.Connections[0].Syncs[0].Project != "web-app" {
		t.Fatalf("list = %s", list.Body)
	}
	if other := owner.request(http.MethodGet, connections+"?provider=github", nil); strings.Contains(other.Body.String(), conn.ID) {
		t.Fatalf("provider filter = %s", other.Body)
	}

	// Deleting a connection in use is refused with the syncs listed.
	inUse := owner.request(http.MethodDelete, connections+"/"+conn.ID, nil)
	if inUse.Code != http.StatusConflict || !strings.Contains(inUse.Body.String(), "connection_in_use") ||
		!strings.Contains(inUse.Body.String(), shared.ID) {
		t.Fatalf("delete in use = %d body=%s", inUse.Code, inUse.Body)
	}

	// Migration: a sync with an inline token moves its credential into a new
	// connection.
	inline := owner.request(http.MethodPost, syncs, map[string]any{
		"environment": "staging", "provider": "forgejo", "base_url": srv.URL,
		"forgejo_repo": "team/app", "token": "fj-one",
	})
	if inline.Code != http.StatusCreated {
		t.Fatalf("inline sync = %d body=%s", inline.Code, inline.Body)
	}
	legacy := decode[struct {
		ID string `json:"id"`
	}](t, inline)
	moved := owner.request(http.MethodPut, syncs+"/"+legacy.ID+"/connection", map[string]any{"name": "Migrated"})
	if moved.Code != http.StatusOK {
		t.Fatalf("move to connection = %d body=%s", moved.Code, moved.Body)
	}
	movedBody := decode[struct {
		Sync struct {
			ConnectionID string `json:"connection_id"`
		} `json:"sync"`
		Connection connectionResponse `json:"connection"`
	}](t, moved)
	if movedBody.Connection.Name != "Migrated" || movedBody.Sync.ConnectionID != movedBody.Connection.ID ||
		movedBody.Connection.BaseURL != srv.URL {
		t.Fatalf("moved = %s", moved.Body)
	}
	if rec := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/environments/staging/secrets",
		map[string]any{"key": "API_KEY", "value": "v2"}); rec.Code != http.StatusCreated {
		t.Fatalf("staging secret = %d", rec.Code)
	}
	run(legacy.ID)
	if got := fake.token(); got != "fj-one" {
		t.Fatalf("migrated sync used token %q", got)
	}
	// And can then be pointed at the shared connection.
	if rec := owner.request(http.MethodPut, syncs+"/"+legacy.ID+"/connection",
		map[string]any{"connection_id": conn.ID}); rec.Code != http.StatusOK {
		t.Fatalf("switch connection = %d body=%s", rec.Code, rec.Body)
	}
	run(legacy.ID)
	if got := fake.token(); got != "fj-two" {
		t.Fatalf("switched sync used token %q", got)
	}
	if rec := owner.request(http.MethodDelete, connections+"/"+movedBody.Connection.ID, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete unused = %d body=%s", rec.Code, rec.Body)
	}

	// A rejected credential is a failed test, not an API error.
	bad := decode[connectionResponse](t, owner.request(http.MethodPost, connections, map[string]any{
		"provider": "forgejo", "name": "Bad", "base_url": srv.URL, "token": "nope",
	}))
	badTest := owner.request(http.MethodPost, connections+"/"+bad.ID+"/test", nil)
	if badTest.Code != http.StatusOK || !strings.Contains(badTest.Body.String(), `"ok":false`) ||
		!strings.Contains(badTest.Body.String(), "rejected") {
		t.Fatalf("bad test = %d body=%s", badTest.Code, badTest.Body)
	}

	events := owner.request(http.MethodGet, "/api/v1/orgs/acme/audit-events", nil).Body.String()
	for _, action := range []string{"sync_connection.create", "sync_connection.rotate", "sync.connection"} {
		if !strings.Contains(events, action) {
			t.Errorf("audit log has no %s", action)
		}
	}
	if strings.Contains(events, "fj-one") || strings.Contains(events, "fj-two") {
		t.Error("audit log leaked a token")
	}

	for _, id := range []string{shared.ID, legacy.ID} {
		if rec := owner.request(http.MethodDelete, syncs+"/"+id, nil); rec.Code != http.StatusNoContent {
			t.Fatalf("delete sync = %d", rec.Code)
		}
	}
	if rec := owner.request(http.MethodDelete, connections+"/"+conn.ID, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete connection = %d body=%s", rec.Code, rec.Body)
	}
	if !strings.Contains(owner.request(http.MethodGet, "/api/v1/orgs/acme/audit-events", nil).Body.String(), "sync_connection.delete") {
		t.Error("audit log has no sync_connection.delete")
	}
}

func TestSyncConnectionPermissions(t *testing.T) {
	_, srv := newFakeForgejo(t, "fj-one")
	f := newFixture(t)
	owner, _ := f.setup(t)
	conn := decode[connectionResponse](t, owner.request(http.MethodPost, "/api/v1/orgs/acme/connections", map[string]any{
		"provider": "forgejo", "name": "Shared", "base_url": srv.URL, "token": "fj-one",
	}))

	invite := decode[struct {
		URL string `json:"url"`
	}](t, owner.request(http.MethodPost, "/api/v1/orgs/acme/invites", map[string]any{"role": "Viewer"}))
	token := invite.URL[strings.Index(invite.URL, "token=")+len("token="):]
	viewer := &apiClient{t: t, h: f.srv.Handler()}
	if rec := viewer.request(http.MethodPost, "/api/v1/invites/"+token+"/accept", map[string]string{
		"name": "Viewer", "email": "viewer@example.com", "password": "another long password",
	}); rec.Code != http.StatusOK {
		t.Fatalf("accept = %d body=%s", rec.Code, rec.Body)
	}

	list := viewer.request(http.MethodGet, "/api/v1/orgs/acme/connections", nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "Shared") ||
		strings.Contains(list.Body.String(), `"syncs"`) {
		t.Fatalf("viewer list = %d body=%s", list.Code, list.Body)
	}
	for _, rec := range []*httptest.ResponseRecorder{
		viewer.request(http.MethodPost, "/api/v1/orgs/acme/connections", map[string]any{
			"provider": "forgejo", "name": "Mine", "token": "x",
		}),
		viewer.request(http.MethodPatch, "/api/v1/orgs/acme/connections/"+conn.ID, map[string]any{"token": "x"}),
		viewer.request(http.MethodPost, "/api/v1/orgs/acme/connections/"+conn.ID+"/test", nil),
		viewer.request(http.MethodDelete, "/api/v1/orgs/acme/connections/"+conn.ID, nil),
	} {
		if rec.Code != http.StatusForbidden {
			t.Fatalf("viewer write = %d body=%s", rec.Code, rec.Body)
		}
	}
}

func TestSyncConnectionTestAWS(t *testing.T) {
	var gotAuth, gotBody, gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := new(strings.Builder)
		_, _ = io.Copy(body, r.Body)
		gotAuth, gotBody, gotToken = r.Header.Get("Authorization"), body.String(), r.Header.Get("X-Amz-Security-Token")
		_, _ = w.Write([]byte(`<GetCallerIdentityResponse><GetCallerIdentityResult><Arn>arn:aws:iam::123456789012:user/clavis</Arn></GetCallerIdentityResult></GetCallerIdentityResponse>`))
	}))
	defer srv.Close()
	f := newFixture(t)
	owner, _ := f.setup(t)
	created := owner.request(http.MethodPost, "/api/v1/orgs/acme/connections", map[string]any{
		"provider": "aws", "name": "Prod AWS", "base_url": srv.URL,
		"aws_access_key": "AKIDEXAMPLE", "aws_secret_key": "secret", "aws_session_token": "session",
	})
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"auth":"aws_keys"`) {
		t.Fatalf("create = %d body=%s", created.Code, created.Body)
	}
	conn := decode[connectionResponse](t, created)
	rec := owner.request(http.MethodPost, "/api/v1/orgs/acme/connections/"+conn.ID+"/test", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "user/clavis") {
		t.Fatalf("test = %d body=%s", rec.Code, rec.Body)
	}
	if gotBody != "Action=GetCallerIdentity&Version=2011-06-15" || gotToken != "session" {
		t.Fatalf("sts body=%q token=%q", gotBody, gotToken)
	}
	// Signed headers must be sorted and include the session token but no
	// X-Amz-Target for a query-protocol call.
	if !strings.Contains(gotAuth, "Credential=AKIDEXAMPLE/") || !strings.Contains(gotAuth, "/us-east-1/sts/aws4_request") ||
		!strings.Contains(gotAuth, "SignedHeaders=content-type;host;x-amz-content-sha256;x-amz-date;x-amz-security-token,") {
		t.Fatalf("authorization = %q", gotAuth)
	}
}

// A sync-only Forgejo token has no "user" permission, so GET /user is a 403.
// The test must still pass for it, and must point at the destination that
// fails rather than blaming the token.
func TestSyncConnectionTestForgejoDestinations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimPrefix(r.Header.Get("Authorization"), "token ") != "sync-only" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/user":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"token does not have at least one of required scope(s): [read:user]"}`))
		case "/api/v1/repos/team/app/actions/secrets":
			_, _ = w.Write([]byte(`[]`))
		case "/api/v1/repos/team/locked/actions/secrets":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"user should have admin write access to repo"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	f := newFixture(t)
	owner, _ := f.setup(t)
	const connections = "/api/v1/orgs/acme/connections"
	created := owner.request(http.MethodPost, connections, map[string]any{
		"provider": "forgejo", "name": "Sync only", "base_url": srv.URL, "token": "sync-only",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d body=%s", created.Code, created.Body)
	}
	conn := decode[connectionResponse](t, created)
	testURL := connections + "/" + conn.ID + "/test"

	type result struct {
		OK     bool   `json:"ok"`
		Detail string `json:"detail"`
		Checks []struct {
			Target string `json:"target"`
			OK     bool   `json:"ok"`
			Detail string `json:"detail"`
		} `json:"checks"`
	}

	// No destination yet: token is valid, repository access is unchecked.
	bare := decode[result](t, owner.request(http.MethodPost, testURL, nil))
	if !bare.OK || !strings.Contains(bare.Detail, "not checked") || len(bare.Checks) != 1 {
		t.Fatalf("bare test = %+v", bare)
	}

	good := decode[result](t, owner.request(http.MethodPost, testURL,
		map[string]any{"forgejo_scope": "repo", "forgejo_repo": "team/app"}))
	if !good.OK || !strings.Contains(good.Detail, "team/app") {
		t.Fatalf("good repo = %+v", good)
	}

	for repo, want := range map[string]string{
		"team/locked":  "cannot manage Actions",
		"team/missing": "Not found",
		"team":         "owner/name",
	} {
		bad := decode[result](t, owner.request(http.MethodPost, testURL,
			map[string]any{"forgejo_scope": "repo", "forgejo_repo": repo}))
		if bad.OK || !strings.Contains(bad.Detail, want) {
			t.Fatalf("%s = %+v, want %q", repo, bad, want)
		}
	}

	// A wrong token is reported as the token, not as a destination.
	wrong := owner.request(http.MethodPost, connections, map[string]any{
		"provider": "forgejo", "name": "Wrong", "base_url": srv.URL, "token": "nope",
	})
	wrongConn := decode[connectionResponse](t, wrong)
	rejected := decode[result](t, owner.request(http.MethodPost, connections+"/"+wrongConn.ID+"/test", nil))
	if rejected.OK || !strings.Contains(rejected.Detail, "rejected the token") {
		t.Fatalf("rejected = %+v", rejected)
	}
}

// Syncs to a private address (a Forgejo on the LAN) are blocked unless the
// operator allowlists it, and the error says how.
func TestForgejoSyncOutboundAllowlist(t *testing.T) {
	_, srv := newFakeForgejo(t, "fj-one")

	run := func(allow []string) (int, string) {
		f := newFixtureConfig(t, func(cfg *config.Config) {
			cfg.Server.Dev = false // dev mode already allows loopback
			cfg.Server.BaseURL = "http://192.168.1.10:8080" // plain-HTTP LAN host: plain cookies
			cfg.Security.OutboundAllowlist = allow
		})
		owner, _ := f.setup(t)
		created := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/syncs", map[string]any{
			"environment": "dev", "provider": "forgejo", "forgejo_repo": "team/app",
			"base_url": srv.URL, "token": "fj-one",
		})
		if created.Code != http.StatusCreated {
			t.Fatalf("create sync = %d body=%s", created.Code, created.Body)
		}
		id := decode[connectionResponse](t, created).ID
		if rec := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets",
			map[string]any{"key": "API_KEY", "value": "v1"}); rec.Code != http.StatusCreated {
			t.Fatalf("secret = %d", rec.Code)
		}
		rec := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/syncs/"+id+"/run",
			map[string]any{"confirm": true})
		return rec.Code, rec.Body.String()
	}

	if code, body := run(nil); code != http.StatusCreated || !strings.Contains(body, `"status":"failed"`) ||
		!strings.Contains(body, "outbound_allowlist") {
		t.Fatalf("blocked run = %d body=%s", code, body)
	}
	if code, body := run([]string{"127.0.0.1/32"}); code != http.StatusCreated || !strings.Contains(body, `"status":"success"`) {
		t.Fatalf("allowlisted run = %d body=%s", code, body)
	}
}

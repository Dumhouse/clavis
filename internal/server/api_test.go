package server_test

import (
	"bytes"
	"context"
	stdcrypto "crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/nacl/box"

	"github.com/dumhouse/clavis/internal/authn"
	"github.com/dumhouse/clavis/internal/config"
	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/email"
	"github.com/dumhouse/clavis/internal/server"
	"github.com/dumhouse/clavis/internal/store"
	"github.com/dumhouse/clavis/internal/store/storetest"
)

type fixture struct {
	srv    *server.Server
	st     *store.Store
	dir    string
	mailer *fakeMailer
}

type fakeMailer struct {
	mu       sync.Mutex
	messages []email.Message
}

func (f *fakeMailer) Send(_ context.Context, msg email.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, msg)
	return nil
}

func (f *fakeMailer) Messages() []email.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]email.Message(nil), f.messages...)
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	return newFixtureOptions(t)
}

func newFixtureOptions(t *testing.T, opts ...server.Option) *fixture {
	t.Helper()
	st := storetest.Open(t)
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Server.Addr = "127.0.0.1:0"
	cfg.Server.BaseURL = "http://localhost:8080"
	cfg.Server.Dev = true
	cfg.Database.Path = filepath.Join(dir, "clavis.db")
	mailer := &fakeMailer{}
	baseOpts := []server.Option{server.WithWebFS(testWebFS()), server.WithMailer(mailer)}
	srv, err := server.New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), key, append(baseOpts, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{srv: srv, st: st, dir: dir, mailer: mailer}
}

type apiClient struct {
	t       *testing.T
	h       http.Handler
	cookies []*http.Cookie
	csrf    string
}

func (c *apiClient) request(method, path string, body any) *httptest.ResponseRecorder {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		reader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, cookie := range c.cookies {
		req.AddCookie(cookie)
	}
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	c.captureCookies(rec)
	return rec
}

func (c *apiClient) requestHeaders(method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		reader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	for _, cookie := range c.cookies {
		req.AddCookie(cookie)
	}
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	c.captureCookies(rec)
	return rec
}

func (c *apiClient) reauth(password string) string {
	c.t.Helper()
	rec := c.request(http.MethodPost, "/api/v1/auth/reauthenticate", map[string]string{"password": password})
	if rec.Code != http.StatusOK {
		c.t.Fatalf("reauthenticate = %d body=%s", rec.Code, rec.Body)
	}
	var out struct {
		Token string `json:"reauth_token"`
	}
	out = decode[struct {
		Token string `json:"reauth_token"`
	}](c.t, rec)
	return out.Token
}

func (c *apiClient) captureCookies(rec *httptest.ResponseRecorder) {
	c.t.Helper()
	for _, cookie := range rec.Result().Cookies() {
		if cookie.MaxAge < 0 {
			continue
		}
		found := false
		for i := range c.cookies {
			if c.cookies[i].Name == cookie.Name {
				c.cookies[i] = cookie
				found = true
			}
		}
		if !found {
			c.cookies = append(c.cookies, cookie)
		}
		if cookie.Name == "clavis_csrf" {
			c.csrf = cookie.Value
		}
	}
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %T: %v (body %s)", out, err, rec.Body)
	}
	return out
}

type setupResponse struct {
	User struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"user"`
	Orgs []struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
		Role string `json:"role"`
	} `json:"orgs"`
	CSRF string `json:"csrf_token"`
}

type projectResponse struct {
	ID           string `json:"id"`
	Slug         string `json:"slug"`
	Environments []struct {
		Slug string `json:"slug"`
		Type string `json:"type"`
	} `json:"environments"`
}

type secretResponse struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Version  int    `json:"version"`
	Revealed bool   `json:"revealed"`
}

func (f *fixture) setup(t *testing.T) (*apiClient, setupResponse) {
	t.Helper()
	tokenBytes, err := os.ReadFile(filepath.Join(f.dir, "setup-token"))
	if err != nil {
		t.Fatal(err)
	}
	c := &apiClient{t: t, h: f.srv.Handler()}
	rec := c.request(http.MethodPost, "/api/v1/setup", map[string]string{
		"token":        strings.TrimSpace(string(tokenBytes)),
		"email":        "owner@example.com",
		"name":         "Owner",
		"password":     "correct horse battery staple",
		"org_name":     "Acme",
		"org_slug":     "acme",
		"project_name": "Web App",
		"project_slug": "web-app",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("setup status = %d body=%s", rec.Code, rec.Body)
	}
	var body setupResponse
	body = decode[setupResponse](t, rec)
	if body.User.Email != "owner@example.com" || len(body.Orgs) != 1 || body.Orgs[0].Role != "Owner" {
		t.Fatalf("setup body = %+v", body)
	}
	if c.csrf == "" || c.csrf != body.CSRF {
		t.Fatalf("csrf missing or mismatched: cookie=%q body=%q", c.csrf, body.CSRF)
	}
	for _, cookie := range c.cookies {
		if cookie.Name == "clavis_session" {
			if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.MaxAge <= 0 {
				t.Fatalf("insecure session cookie: %+v", cookie)
			}
		}
	}
	return c, body
}

func TestSetupLoginAndSecretsFlow(t *testing.T) {
	f := newFixture(t)
	c, setup := f.setup(t)
	orgID := setup.Orgs[0].ID

	// Setup closed.
	status := c.request(http.MethodGet, "/api/v1/setup/status", nil)
	if got := decode[map[string]bool](t, status); got["required"] {
		t.Fatal("setup still required")
	}
	again := c.request(http.MethodPost, "/api/v1/setup", map[string]string{"token": "x", "email": "x@example.com", "name": "X", "password": "correct horse battery staple", "org_name": "X"})
	if again.Code != http.StatusGone {
		t.Fatalf("second setup status = %d body=%s", again.Code, again.Body)
	}

	// Me.
	me := c.request(http.MethodGet, "/api/v1/auth/me", nil)
	if me.Code != http.StatusOK {
		t.Fatalf("me status = %d", me.Code)
	}

	// Project exists from setup.
	projects := c.request(http.MethodGet, "/api/v1/orgs/acme/projects", nil)
	if projects.Code != http.StatusOK {
		t.Fatalf("projects status = %d body=%s", projects.Code, projects.Body)
	}
	var projectList struct {
		Projects []projectResponse `json:"projects"`
	}
	projectList = decode[struct {
		Projects []projectResponse `json:"projects"`
	}](t, projects)
	if len(projectList.Projects) != 1 || len(projectList.Projects[0].Environments) != 3 {
		t.Fatalf("projects = %+v", projectList.Projects)
	}

	// Create another project.
	created := c.request(http.MethodPost, "/api/v1/orgs/acme/projects", map[string]string{"name": "Payments", "slug": "payments"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create project status = %d body=%s", created.Code, created.Body)
	}

	base := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	// Create.
	createdSecret := c.request(http.MethodPost, base, map[string]string{
		"key": "STRIPE_SECRET_KEY", "value": "sk_test_123", "comment": "test key", "message": "initial",
	})
	if createdSecret.Code != http.StatusCreated {
		t.Fatalf("create secret status = %d body=%s", createdSecret.Code, createdSecret.Body)
	}

	// Encryption at rest: neither the key name nor the value may appear in the DB.
	var keyCT, valueCT []byte
	if err := f.st.DB().QueryRow(`SELECT key_ciphertext, value_ciphertext FROM items LIMIT 1`).Scan(&keyCT, &valueCT); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(keyCT, []byte("STRIPE_SECRET_KEY")) || bytes.Contains(valueCT, []byte("sk_test_123")) {
		t.Fatal("plaintext found in encrypted columns")
	}

	// Duplicate conflict.
	dup := c.request(http.MethodPost, base, map[string]string{"key": "STRIPE_SECRET_KEY", "value": "x"})
	if dup.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d body=%s", dup.Code, dup.Body)
	}

	// List without reveal.
	list := c.request(http.MethodGet, base, nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", list.Code, list.Body)
	}
	var listed struct {
		Secrets []secretResponse `json:"secrets"`
	}
	listed = decode[struct {
		Secrets []secretResponse `json:"secrets"`
	}](t, list)
	if len(listed.Secrets) != 1 || listed.Secrets[0].Key != "STRIPE_SECRET_KEY" || listed.Secrets[0].Value != "" {
		t.Fatalf("listed = %+v", listed.Secrets)
	}

	// Reveal.
	revealed := c.request(http.MethodGet, base+"/STRIPE_SECRET_KEY?reveal=true&reason=testing", nil)
	if revealed.Code != http.StatusOK {
		t.Fatalf("reveal status = %d body=%s", revealed.Code, revealed.Body)
	}
	got := decode[secretResponse](t, revealed)
	if got.Value != "sk_test_123" || !got.Revealed {
		t.Fatalf("revealed = %+v", got)
	}

	// Update creates a version.
	updated := c.request(http.MethodPut, base+"/STRIPE_SECRET_KEY", map[string]string{
		"value": "sk_test_456", "kind": "secret", "comment": "rotated", "message": "rotate",
	})
	if updated.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", updated.Code, updated.Body)
	}
	updatedBody := decode[secretResponse](t, updated)
	if updatedBody.Version != 2 {
		t.Fatalf("updated version = %d", updatedBody.Version)
	}

	versions := c.request(http.MethodGet, base+"/STRIPE_SECRET_KEY/versions", nil)
	if versions.Code != http.StatusOK || !strings.Contains(versions.Body.String(), `"version":2`) {
		t.Fatalf("versions status = %d body=%s", versions.Code, versions.Body)
	}

	// Rollback to v1.
	rolled := c.request(http.MethodPost, base+"/STRIPE_SECRET_KEY/rollback", map[string]int{"version": 1})
	if rolled.Code != http.StatusCreated {
		t.Fatalf("rollback status = %d body=%s", rolled.Code, rolled.Body)
	}
	afterRollback := c.request(http.MethodGet, base+"/STRIPE_SECRET_KEY?reveal=true", nil)
	if value := decode[secretResponse](t, afterRollback); value.Value != "sk_test_123" || value.Version != 3 {
		t.Fatalf("after rollback = %+v", value)
	}

	// CSRF is enforced.
	c.csrf = ""
	noCSRF := c.request(http.MethodPost, base, map[string]string{"key": "OTHER", "value": "x"})
	if noCSRF.Code != http.StatusForbidden || !strings.Contains(noCSRF.Body.String(), "csrf_failed") {
		t.Fatalf("csrf status = %d body=%s", noCSRF.Code, noCSRF.Body)
	}
	c.csrf = strings.TrimSpace(c.csrf)

	// Delete.
	// Restore the CSRF token from the cookie.
	for _, cookie := range c.cookies {
		if cookie.Name == "clavis_csrf" {
			c.csrf = cookie.Value
		}
	}
	deleted := c.request(http.MethodDelete, base+"/STRIPE_SECRET_KEY", nil)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body=%s", deleted.Code, deleted.Body)
	}
	list = c.request(http.MethodGet, base, nil)
	listed = decode[struct {
		Secrets []secretResponse `json:"secrets"`
	}](t, list)
	if len(listed.Secrets) != 0 {
		t.Fatalf("secret still listed: %+v", listed.Secrets)
	}

	// Audit chain verifies and has events.
	if err := f.st.VerifyAuditChain(context.Background(), orgID); err != nil {
		t.Fatal(err)
	}
	auditRec := c.request(http.MethodGet, "/api/v1/orgs/acme/audit-events", nil)
	if auditRec.Code != http.StatusOK {
		t.Fatalf("audit API status = %d body=%s", auditRec.Code, auditRec.Body)
	}
	if !strings.Contains(auditRec.Body.String(), `"verified":true`) {
		t.Fatalf("audit API not verified: %s", auditRec.Body)
	}
	if strings.Contains(auditRec.Body.String(), "STRIPE_SECRET_KEY") || strings.Contains(auditRec.Body.String(), "sk_test_123") {
		t.Fatal("audit API leaked secret material")
	}
	events, err := f.st.ListAuditEvents(context.Background(), orgID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 8 {
		t.Fatalf("audit events = %d, want >= 8", len(events))
	}
	raw, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	if i := bytes.Index(raw, []byte("STRIPE_SECRET_KEY")); i >= 0 {
		start := i - 200
		if start < 0 {
			start = 0
		}
		end := i + 200
		if end > len(raw) {
			end = len(raw)
		}
		t.Fatalf("audit log contains key name: %s", raw[start:end])
	}
	if bytes.Contains(raw, []byte("sk_test_123")) {
		t.Fatal("audit log contains secret value")
	}
	var revealCount int
	if err := f.st.DB().QueryRow(`SELECT COUNT(*) FROM reveal_events`).Scan(&revealCount); err != nil {
		t.Fatal(err)
	}
	if revealCount < 2 {
		t.Fatalf("reveal events = %d, want >= 2", revealCount)
	}
}

func TestLoginAndSessionRevocation(t *testing.T) {
	f := newFixture(t)
	f.setup(t)

	anon := &apiClient{t: t, h: f.srv.Handler()}
	wrong := anon.request(http.MethodPost, "/api/v1/auth/login", map[string]string{"email": "owner@example.com", "password": "wrong password"})
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password status = %d body=%s", wrong.Code, wrong.Body)
	}
	unknown := anon.request(http.MethodPost, "/api/v1/auth/login", map[string]string{"email": "nobody@example.com", "password": "wrong password"})
	if unknown.Code != http.StatusUnauthorized {
		t.Fatalf("unknown user status = %d", unknown.Code)
	}

	login := anon.request(http.MethodPost, "/api/v1/auth/login", map[string]string{"email": "owner@example.com", "password": "correct horse battery staple"})
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", login.Code, login.Body)
	}
	me := anon.request(http.MethodGet, "/api/v1/auth/me", nil)
	if me.Code != http.StatusOK {
		t.Fatalf("me status = %d", me.Code)
	}

	sessions := anon.request(http.MethodGet, "/api/v1/auth/sessions", nil)
	if sessions.Code != http.StatusOK {
		t.Fatalf("sessions status = %d", sessions.Code)
	}
	var sessionList struct {
		Sessions []struct {
			ID      string `json:"id"`
			Current bool   `json:"current"`
		} `json:"sessions"`
	}
	sessionList = decode[struct {
		Sessions []struct {
			ID      string `json:"id"`
			Current bool   `json:"current"`
		} `json:"sessions"`
	}](t, sessions)
	var currentID string
	for _, s := range sessionList.Sessions {
		if s.Current {
			currentID = s.ID
		}
	}
	if currentID == "" {
		t.Fatal("no current session marked")
	}

	logout := anon.request(http.MethodPost, "/api/v1/auth/logout", nil)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d body=%s", logout.Code, logout.Body)
	}
	if me := anon.request(http.MethodGet, "/api/v1/auth/me", nil); me.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout status = %d", me.Code)
	}
}

func TestUnauthenticatedAndForbidden(t *testing.T) {
	f := newFixture(t)
	c := &apiClient{t: t, h: f.srv.Handler()}
	if rec := c.request(http.MethodGet, "/api/v1/orgs", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("orgs status = %d", rec.Code)
	}
	if rec := c.request(http.MethodGet, "/api/v1/orgs/acme/projects", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("projects status = %d", rec.Code)
	}
	// Setup without the token is rejected.
	rec := c.request(http.MethodPost, "/api/v1/setup", map[string]string{
		"token": "wrong", "email": "x@example.com", "name": "X",
		"password": "correct horse battery staple", "org_name": "X",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("bad setup token status = %d body=%s", rec.Code, rec.Body)
	}
}

func TestDeviceLoginFlow(t *testing.T) {
	f := newFixture(t)
	c, _ := f.setup(t)
	anon := &apiClient{t: t, h: f.srv.Handler()}

	codeRec := anon.request(http.MethodPost, "/api/v1/auth/device/code", map[string]any{})
	if codeRec.Code != http.StatusOK {
		t.Fatalf("device code status = %d body=%s", codeRec.Code, codeRec.Body)
	}
	var code struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	code = decode[struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}](t, codeRec)
	if code.DeviceCode == "" || code.UserCode == "" || code.ExpiresIn <= 0 || !strings.Contains(code.VerificationURI, "/device") {
		t.Fatalf("device code = %+v", code)
	}

	pending := anon.request(http.MethodPost, "/api/v1/auth/device/token", map[string]string{"device_code": code.DeviceCode})
	var pendingBody struct {
		Status string `json:"status"`
	}
	pendingBody = decode[struct {
		Status string `json:"status"`
	}](t, pending)
	if pending.Code != http.StatusOK || pendingBody.Status != "pending" {
		t.Fatalf("pending = %d %+v", pending.Code, pendingBody)
	}

	approve := c.request(http.MethodPost, "/api/v1/auth/device/approve", map[string]string{"user_code": code.UserCode, "action": "approve"})
	if approve.Code != http.StatusOK || !strings.Contains(approve.Body.String(), "approved") {
		t.Fatalf("approve = %d body=%s", approve.Code, approve.Body)
	}

	exchanged := anon.request(http.MethodPost, "/api/v1/auth/device/token", map[string]string{"device_code": code.DeviceCode})
	var tokenBody struct {
		Status string `json:"status"`
		Token  string `json:"token"`
		User   struct {
			Email string `json:"email"`
		} `json:"user"`
	}
	tokenBody = decode[struct {
		Status string `json:"status"`
		Token  string `json:"token"`
		User   struct {
			Email string `json:"email"`
		} `json:"user"`
	}](t, exchanged)
	if exchanged.Code != http.StatusOK || tokenBody.Status != "approved" || tokenBody.Token == "" || tokenBody.User.Email != "owner@example.com" {
		t.Fatalf("exchange = %d %+v", exchanged.Code, tokenBody)
	}

	// The bearer token authenticates without cookies or CSRF.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+tokenBody.Token)
	rec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bearer me status = %d body=%s", rec.Code, rec.Body)
	}

	// A used device code cannot be replayed.
	replay := anon.request(http.MethodPost, "/api/v1/auth/device/token", map[string]string{"device_code": code.DeviceCode})
	if replay.Code == http.StatusOK {
		t.Fatalf("device code replayed: %s", replay.Body)
	}
}

func TestInvitesAndMembers(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.setup(t)

	create := owner.request(http.MethodPost, "/api/v1/orgs/acme/invites", map[string]any{
		"role": "Developer", "expires_in_days": 7,
	})
	if create.Code != http.StatusCreated {
		t.Fatalf("invite status = %d body=%s", create.Code, create.Body)
	}
	var inv struct {
		ID   string `json:"id"`
		Role string `json:"role"`
		URL  string `json:"url"`
	}
	inv = decode[struct {
		ID   string `json:"id"`
		Role string `json:"role"`
		URL  string `json:"url"`
	}](t, create)
	if inv.ID == "" || inv.Role != "Developer" || !strings.Contains(inv.URL, "/join?token=") {
		t.Fatalf("invite = %+v", inv)
	}
	token := inv.URL[strings.Index(inv.URL, "token=")+len("token="):]

	info := owner.request(http.MethodGet, "/api/v1/invites/"+token, nil)
	if info.Code != http.StatusOK || !strings.Contains(info.Body.String(), "Acme") {
		t.Fatalf("invite info = %d %s", info.Code, info.Body)
	}

	newbie := &apiClient{t: t, h: f.srv.Handler()}
	accept := newbie.request(http.MethodPost, "/api/v1/invites/"+token+"/accept", map[string]string{
		"name": "New Person", "email": "new@example.com", "password": "another long password",
	})
	if accept.Code != http.StatusOK {
		t.Fatalf("accept status = %d body=%s", accept.Code, accept.Body)
	}
	var accepted setupResponse
	accepted = decode[setupResponse](t, accept)
	if accepted.User.Email != "new@example.com" || len(accepted.Orgs) != 1 {
		t.Fatalf("accepted = %+v", accepted)
	}
	if me := newbie.request(http.MethodGet, "/api/v1/auth/me", nil); me.Code != http.StatusOK {
		t.Fatalf("new member me = %d", me.Code)
	}

	// The token is single-use.
	replay := (&apiClient{t: t, h: f.srv.Handler()}).request(http.MethodPost, "/api/v1/invites/"+token+"/accept", map[string]string{
		"name": "Again", "email": "again@example.com", "password": "another long password",
	})
	if replay.Code == http.StatusOK {
		t.Fatalf("invite replayed: %s", replay.Body)
	}

	// Members list shows both.
	members := owner.request(http.MethodGet, "/api/v1/orgs/acme/members", nil)
	if members.Code != http.StatusOK {
		t.Fatalf("members status = %d body=%s", members.Code, members.Body)
	}
	var list struct {
		Members []struct {
			UserID string `json:"user_id"`
			Email  string `json:"email"`
			Role   string `json:"role"`
		} `json:"members"`
	}
	list = decode[struct {
		Members []struct {
			UserID string `json:"user_id"`
			Email  string `json:"email"`
			Role   string `json:"role"`
		} `json:"members"`
	}](t, members)
	if len(list.Members) != 2 {
		t.Fatalf("members = %+v", list.Members)
	}
	var newbieID string
	for _, member := range list.Members {
		if member.Email == "new@example.com" {
			newbieID = member.UserID
		}
	}
	if newbieID == "" {
		t.Fatal("new member missing")
	}

	// The new Developer cannot manage members.
	denied := newbie.request(http.MethodGet, "/api/v1/orgs/acme/members", nil)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("developer members status = %d", denied.Code)
	}

	// Owner promotes and then removes the member.
	promote := owner.request(http.MethodPatch, "/api/v1/orgs/acme/members/"+newbieID, map[string]string{"role": "Viewer"})
	if promote.Code != http.StatusNoContent {
		t.Fatalf("promote status = %d body=%s", promote.Code, promote.Body)
	}
	remove := owner.requestHeaders(http.MethodDelete, "/api/v1/orgs/acme/members/"+newbieID, nil,
		map[string]string{"X-Clavis-Reauth": owner.reauth("correct horse battery staple")})
	if remove.Code != http.StatusNoContent {
		t.Fatalf("remove status = %d body=%s", remove.Code, remove.Body)
	}
	if me := newbie.request(http.MethodGet, "/api/v1/auth/me", nil); me.Code != http.StatusUnauthorized {
		t.Fatalf("removed member session still valid: %d", me.Code)
	}
}

func TestInviteEmailRestrictionAndLastOwner(t *testing.T) {
	f := newFixture(t)
	owner, setup := f.setup(t)

	create := owner.request(http.MethodPost, "/api/v1/orgs/acme/invites", map[string]any{
		"email": "only@example.com", "role": "Viewer",
	})
	if create.Code != http.StatusCreated {
		t.Fatalf("invite status = %d body=%s", create.Code, create.Body)
	}
	var inv struct {
		URL string `json:"url"`
	}
	inv = decode[struct {
		URL string `json:"url"`
	}](t, create)
	token := inv.URL[strings.Index(inv.URL, "token=")+len("token="):]

	// The invite's email wins over anything typed in the form.
	forced := (&apiClient{t: t, h: f.srv.Handler()}).request(http.MethodPost, "/api/v1/invites/"+token+"/accept", map[string]string{
		"name": "Invited Person", "email": "wrong@example.com", "password": "another long password",
	})
	if forced.Code != http.StatusOK || !strings.Contains(forced.Body.String(), "only@example.com") {
		t.Fatalf("forced email status = %d body=%s", forced.Code, forced.Body)
	}

	// The last owner cannot be demoted.
	self := setup.User.ID
	demote := owner.request(http.MethodPatch, "/api/v1/orgs/acme/members/"+self, map[string]string{"role": "Admin"})
	if demote.Code != http.StatusConflict || !strings.Contains(demote.Body.String(), "last_owner") {
		t.Fatalf("demote last owner status = %d body=%s", demote.Code, demote.Body)
	}
	removeSelf := owner.requestHeaders(http.MethodDelete, "/api/v1/orgs/acme/members/"+self, nil,
		map[string]string{"X-Clavis-Reauth": owner.reauth("correct horse battery staple")})
	if removeSelf.Code != http.StatusConflict {
		t.Fatalf("remove last owner status = %d", removeSelf.Code)
	}
}

func TestReauthenticationRequired(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.setup(t)
	invite := owner.request(http.MethodPost, "/api/v1/orgs/acme/invites", map[string]any{"role": "Viewer"})
	var inv struct {
		URL string `json:"url"`
	}
	inv = decode[struct {
		URL string `json:"url"`
	}](t, invite)
	token := inv.URL[strings.Index(inv.URL, "token=")+len("token="):]
	viewer := &apiClient{t: t, h: f.srv.Handler()}
	accept := viewer.request(http.MethodPost, "/api/v1/invites/"+token+"/accept", map[string]string{
		"name": "Temp", "email": "temp@example.com", "password": "another long password",
	})
	if accept.Code != http.StatusOK {
		t.Fatalf("accept = %d", accept.Code)
	}
	accepted := decode[setupResponse](t, accept)

	path := "/api/v1/orgs/acme/members/" + accepted.User.ID
	if rec := owner.request(http.MethodDelete, path, nil); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "reauth_required") {
		t.Fatalf("remove without reauth = %d body=%s", rec.Code, rec.Body)
	}
	if rec := owner.request(http.MethodPost, "/api/v1/auth/reauthenticate", map[string]string{
		"password": "wrong password value",
	}); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "reauth_required") {
		t.Fatalf("wrong password = %d body=%s", rec.Code, rec.Body)
	}
	reauth := owner.request(http.MethodPost, "/api/v1/auth/reauthenticate", map[string]string{
		"password": "correct horse battery staple",
	})
	if reauth.Code != http.StatusOK {
		t.Fatalf("reauth = %d body=%s", reauth.Code, reauth.Body)
	}
	var result struct {
		Token string `json:"reauth_token"`
	}
	result = decode[struct {
		Token string `json:"reauth_token"`
	}](t, reauth)
	removed := owner.requestHeaders(http.MethodDelete, path, nil, map[string]string{
		"X-Clavis-Reauth": result.Token,
	})
	if removed.Code != http.StatusNoContent {
		t.Fatalf("remove with reauth = %d body=%s", removed.Code, removed.Body)
	}
	if rec := owner.requestHeaders(http.MethodDelete, path, nil, map[string]string{
		"X-Clavis-Reauth": "not-a-token",
	}); rec.Code != http.StatusForbidden {
		t.Fatalf("tampered token = %d", rec.Code)
	}
}

func TestEnvironmentTemplates(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.setup(t)
	update := owner.request(http.MethodPatch, "/api/v1/orgs/acme/settings", map[string]any{
		"environment_templates": []map[string]any{
			{"slug": "local", "name": "Local", "type": "development"},
			{"slug": "qa", "name": "QA", "type": "staging"},
			{"slug": "live", "name": "Live", "type": "production"},
		},
	})
	if update.Code != http.StatusOK || !strings.Contains(update.Body.String(), `"slug":"qa"`) {
		t.Fatalf("settings = %d body=%s", update.Code, update.Body)
	}
	if rec := owner.request(http.MethodPatch, "/api/v1/orgs/acme/settings", map[string]any{
		"environment_templates": []map[string]any{
			{"slug": "dup", "name": "One", "type": "development"},
			{"slug": "dup", "name": "Two", "type": "staging"},
		},
	}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("duplicate templates = %d body=%s", rec.Code, rec.Body)
	}

	create := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects", map[string]any{
		"name": "Templated", "slug": "templated",
	})
	if create.Code != http.StatusCreated {
		t.Fatalf("create project = %d body=%s", create.Code, create.Body)
	}
	var project struct {
		Environments []struct {
			Slug      string `json:"slug"`
			Type      string `json:"type"`
			Protected bool   `json:"protected"`
		} `json:"environments"`
	}
	project = decode[struct {
		Environments []struct {
			Slug      string `json:"slug"`
			Type      string `json:"type"`
			Protected bool   `json:"protected"`
		} `json:"environments"`
	}](t, create)
	if len(project.Environments) != 3 || project.Environments[2].Slug != "live" || !project.Environments[2].Protected {
		t.Fatalf("environments = %+v", project.Environments)
	}
}

func TestEnvironmentInheritance(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.setup(t)
	dev := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	staging := "/api/v1/orgs/acme/projects/web-app/environments/staging/secrets"

	if rec := owner.request(http.MethodPost, dev, map[string]any{
		"key": "DATABASE_URL", "value": "postgres://dev", "kind": "variable",
	}); rec.Code != http.StatusCreated {
		t.Fatalf("create parent = %d body=%s", rec.Code, rec.Body)
	}
	link := owner.request(http.MethodPatch, "/api/v1/orgs/acme/projects/web-app/environments/staging", map[string]any{
		"parent_slug": "dev",
	})
	if link.Code != http.StatusOK || !strings.Contains(link.Body.String(), "parent_id") {
		t.Fatalf("link parent = %d body=%s", link.Code, link.Body)
	}

	list := owner.request(http.MethodGet, staging, nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "DATABASE_URL") ||
		!strings.Contains(list.Body.String(), `"source_environment":"dev"`) ||
		!strings.Contains(list.Body.String(), `"inherited":true`) {
		t.Fatalf("inherited list = %d body=%s", list.Code, list.Body)
	}
	get := owner.request(http.MethodGet, staging+"/DATABASE_URL", nil)
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), "postgres://dev") {
		t.Fatalf("inherited get = %d body=%s", get.Code, get.Body)
	}

	// A local item shadows the inherited one.
	if rec := owner.request(http.MethodPut, staging+"/DATABASE_URL", map[string]any{
		"value": "postgres://staging", "kind": "variable",
	}); rec.Code != http.StatusOK {
		t.Fatalf("override = %d body=%s", rec.Code, rec.Body)
	}
	overridden := owner.request(http.MethodGet, staging+"/DATABASE_URL", nil)
	if overridden.Code != http.StatusOK || !strings.Contains(overridden.Body.String(), "postgres://staging") ||
		strings.Contains(overridden.Body.String(), "postgres://dev") {
		t.Fatalf("override get = %d body=%s", overridden.Code, overridden.Body)
	}

	if rec := owner.request(http.MethodPatch, "/api/v1/orgs/acme/projects/web-app/environments/staging", map[string]any{
		"parent_slug": "missing",
	}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing parent = %d body=%s", rec.Code, rec.Body)
	}
	if rec := owner.request(http.MethodPatch, "/api/v1/orgs/acme/projects/web-app/environments/staging", map[string]any{
		"parent_slug": "prod",
	}); rec.Code != http.StatusOK {
		t.Fatalf("staging parent prod = %d body=%s", rec.Code, rec.Body)
	}
	if rec := owner.request(http.MethodPatch, "/api/v1/orgs/acme/projects/web-app/environments/prod", map[string]any{
		"parent_slug": "staging",
	}); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "cycle") {
		t.Fatalf("cycle = %d body=%s", rec.Code, rec.Body)
	}
	if rec := owner.request(http.MethodPatch, "/api/v1/orgs/acme/projects/web-app/environments/staging", map[string]any{
		"parent_slug": "staging",
	}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("self parent = %d body=%s", rec.Code, rec.Body)
	}
}

func TestEnvironmentSnapshots(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.setup(t)
	dev := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	if rec := owner.request(http.MethodPost, dev, map[string]any{"key": "ALPHA", "value": "one"}); rec.Code != http.StatusCreated {
		t.Fatalf("alpha = %d", rec.Code)
	}
	if rec := owner.request(http.MethodPost, dev, map[string]any{"key": "BETA", "value": "two"}); rec.Code != http.StatusCreated {
		t.Fatalf("beta = %d", rec.Code)
	}
	created := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/environments/dev/snapshots", map[string]any{
		"reason": "before rotation",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("snapshot = %d body=%s", created.Code, created.Body)
	}
	var snapshot struct {
		ID string `json:"id"`
	}
	snapshot = decode[struct {
		ID string `json:"id"`
	}](t, created)

	if rec := owner.request(http.MethodPut, dev+"/ALPHA", map[string]any{"value": "changed"}); rec.Code != http.StatusOK {
		t.Fatalf("update = %d", rec.Code)
	}
	if rec := owner.request(http.MethodDelete, dev+"/BETA", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rec.Code)
	}

	list := owner.request(http.MethodGet, "/api/v1/orgs/acme/projects/web-app/environments/dev/snapshots", nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "before rotation") {
		t.Fatalf("list snapshots = %d body=%s", list.Code, list.Body)
	}
	restore := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/environments/dev/snapshots/"+snapshot.ID+"/restore", nil)
	if restore.Code != http.StatusCreated || !strings.Contains(restore.Body.String(), `"restored":2`) {
		t.Fatalf("restore = %d body=%s", restore.Code, restore.Body)
	}
	alpha := owner.request(http.MethodGet, dev+"/ALPHA?reveal=true", nil)
	if alpha.Code != http.StatusOK || !strings.Contains(alpha.Body.String(), "one") {
		t.Fatalf("alpha after restore = %d body=%s", alpha.Code, alpha.Body)
	}
	beta := owner.request(http.MethodGet, dev+"/BETA?reveal=true", nil)
	if beta.Code != http.StatusOK || !strings.Contains(beta.Body.String(), "two") {
		t.Fatalf("beta after restore = %d body=%s", beta.Code, beta.Body)
	}
	if rec := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/environments/staging/snapshots/"+snapshot.ID+"/restore", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-env restore = %d body=%s", rec.Code, rec.Body)
	}
}

func TestPersonalOverrides(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.setup(t)
	dev := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	if rec := owner.request(http.MethodPost, dev, map[string]any{"key": "SHARED", "value": "shared-value"}); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d body=%s", rec.Code, rec.Body)
	}
	override := owner.request(http.MethodPut, dev+"/SHARED/override", map[string]any{"value": "mine"})
	if override.Code != http.StatusOK || !strings.Contains(override.Body.String(), `"overridden":true`) {
		t.Fatalf("override = %d body=%s", override.Code, override.Body)
	}
	get := owner.request(http.MethodGet, dev+"/SHARED?reveal=true", nil)
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"value":"mine"`) ||
		!strings.Contains(get.Body.String(), `"overridden":true`) {
		t.Fatalf("get = %d body=%s", get.Code, get.Body)
	}
	list := owner.request(http.MethodGet, dev+"?reveal=true", nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"value":"mine"`) {
		t.Fatalf("list = %d body=%s", list.Code, list.Body)
	}
	// Other users still see the shared value.
	invite := owner.request(http.MethodPost, "/api/v1/orgs/acme/invites", map[string]any{"role": "Developer"})
	var inv struct {
		URL string `json:"url"`
	}
	inv = decode[struct {
		URL string `json:"url"`
	}](t, invite)
	token := inv.URL[strings.Index(inv.URL, "token=")+len("token="):]
	other := &apiClient{t: t, h: f.srv.Handler()}
	if rec := other.request(http.MethodPost, "/api/v1/invites/"+token+"/accept", map[string]string{
		"name": "Other", "email": "other@example.com", "password": "another long password",
	}); rec.Code != http.StatusOK {
		t.Fatalf("accept = %d body=%s", rec.Code, rec.Body)
	}
	otherGet := other.request(http.MethodGet, dev+"/SHARED?reveal=true", nil)
	if otherGet.Code != http.StatusOK || !strings.Contains(otherGet.Body.String(), "shared-value") ||
		strings.Contains(otherGet.Body.String(), `"overridden":true`) {
		t.Fatalf("other get = %d body=%s", otherGet.Code, otherGet.Body)
	}

	prodPath := "/api/v1/orgs/acme/projects/web-app/environments/prod/secrets"
	if rec := owner.request(http.MethodPost, prodPath, map[string]any{"key": "PROD_KEY", "value": "x"}); rec.Code != http.StatusCreated {
		t.Fatalf("prod create = %d body=%s", rec.Code, rec.Body)
	}
	if rec := owner.request(http.MethodPut, prodPath+"/PROD_KEY/override", map[string]any{"value": "y"}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("prod override = %d body=%s", rec.Code, rec.Body)
	}

	if rec := owner.request(http.MethodDelete, dev+"/SHARED/override", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete override = %d body=%s", rec.Code, rec.Body)
	}
	fallback := owner.request(http.MethodGet, dev+"/SHARED?reveal=true", nil)
	if fallback.Code != http.StatusOK || !strings.Contains(fallback.Body.String(), "shared-value") ||
		strings.Contains(fallback.Body.String(), `"overridden":true`) {
		t.Fatalf("fallback = %d body=%s", fallback.Code, fallback.Body)
	}
}

func TestSecretReferences(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.setup(t)
	dev := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	for _, item := range []map[string]any{
		{"key": "HOST", "value": "db.internal"},
		{"key": "DSN", "value": "postgres://${HOST}/app"},
		{"key": "WRAPPED", "value": "[${DSN}]"},
	} {
		if rec := owner.request(http.MethodPost, dev, item); rec.Code != http.StatusCreated {
			t.Fatalf("create = %d body=%s", rec.Code, rec.Body)
		}
	}
	resolved := owner.request(http.MethodGet, dev+":resolve?channel=api", nil)
	if resolved.Code != http.StatusOK {
		t.Fatalf("resolve = %d body=%s", resolved.Code, resolved.Body)
	}
	if !strings.Contains(resolved.Body.String(), `"value":"postgres://db.internal/app"`) ||
		!strings.Contains(resolved.Body.String(), `"value":"[postgres://db.internal/app]"`) {
		t.Fatalf("resolved body = %s", resolved.Body)
	}

	if rec := owner.request(http.MethodPost, dev, map[string]any{"key": "BROKEN", "value": "${MISSING}"}); rec.Code != http.StatusCreated {
		t.Fatalf("missing create = %d", rec.Code)
	}
	if rec := owner.request(http.MethodGet, dev+":resolve", nil); rec.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(rec.Body.String(), "unknown key") {
		t.Fatalf("unknown ref = %d body=%s", rec.Code, rec.Body)
	}
	owner.request(http.MethodDelete, dev+"/BROKEN", nil)

	if rec := owner.request(http.MethodPost, dev, map[string]any{"key": "CYCLE_A", "value": "${CYCLE_B}"}); rec.Code != http.StatusCreated {
		t.Fatalf("cycle a = %d", rec.Code)
	}
	if rec := owner.request(http.MethodPost, dev, map[string]any{"key": "CYCLE_B", "value": "${CYCLE_A}"}); rec.Code != http.StatusCreated {
		t.Fatalf("cycle b = %d", rec.Code)
	}
	if rec := owner.request(http.MethodGet, dev+":resolve", nil); rec.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(rec.Body.String(), "cycle") {
		t.Fatalf("cycle = %d body=%s", rec.Code, rec.Body)
	}
}

func TestPasskeyCeremoniesStart(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.setup(t)

	begin := owner.request(http.MethodPost, "/api/v1/auth/passkeys/register/begin", nil)
	if begin.Code != http.StatusOK {
		t.Fatalf("begin registration = %d body=%s", begin.Code, begin.Body)
	}
	var options struct {
		ChallengeID string          `json:"challenge_id"`
		Options     json.RawMessage `json:"options"`
	}
	options = decode[struct {
		ChallengeID string          `json:"challenge_id"`
		Options     json.RawMessage `json:"options"`
	}](t, begin)
	if options.ChallengeID == "" || !strings.Contains(string(options.Options), "publicKey") {
		t.Fatalf("options = %s", begin.Body)
	}

	list := owner.request(http.MethodGet, "/api/v1/auth/passkeys", nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"passkeys":[]`) {
		t.Fatalf("list = %d body=%s", list.Code, list.Body)
	}

	if rec := owner.request(http.MethodPost, "/api/v1/auth/passkeys/register/finish", map[string]any{
		"challenge_id": options.ChallengeID,
		"credential":   map[string]any{"id": "bogus"},
	}); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "passkey_failed") {
		t.Fatalf("bad finish = %d body=%s", rec.Code, rec.Body)
	}

	anonymous := &apiClient{t: t, h: f.srv.Handler()}
	login := anonymous.request(http.MethodPost, "/api/v1/auth/passkeys/login/begin", nil)
	if login.Code != http.StatusOK || !strings.Contains(login.Body.String(), "challenge_id") {
		t.Fatalf("begin login = %d body=%s", login.Code, login.Body)
	}
}

func TestScimProvisioning(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.setup(t)
	tokenResponse := owner.request(http.MethodPost, "/api/v1/orgs/acme/scim/tokens", nil)
	if tokenResponse.Code != http.StatusCreated {
		t.Fatalf("scim token = %d body=%s", tokenResponse.Code, tokenResponse.Body)
	}
	var tokenBody struct {
		Token string `json:"token"`
	}
	tokenBody = decode[struct {
		Token string `json:"token"`
	}](t, tokenResponse)
	if !strings.HasPrefix(tokenBody.Token, "clv_scim_") {
		t.Fatalf("token = %q", tokenBody.Token)
	}

	scim := func(method, path string, body any) *httptest.ResponseRecorder {
		var reader io.Reader
		if body != nil {
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			reader = bytes.NewReader(raw)
		}
		req := httptest.NewRequest(method, path, reader)
		req.Header.Set("Authorization", "Bearer "+tokenBody.Token)
		if body != nil {
			req.Header.Set("Content-Type", "application/scim+json")
		}
		rec := httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(rec, req)
		return rec
	}

	unauthorized := httptest.NewRequest(http.MethodGet, "/scim/v2/Users", nil)
	unauthorizedRec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(unauthorizedRec, unauthorized)
	if unauthorizedRec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized scim = %d", unauthorizedRec.Code)
	}

	list := scim(http.MethodGet, "/scim/v2/Users", nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "owner@example.com") {
		t.Fatalf("list users = %d body=%s", list.Code, list.Body)
	}

	created := scim(http.MethodPost, "/scim/v2/Users", map[string]any{
		"schemas":  []string{"urn:ietf:params:scim:schemas:core:2.0:User"},
		"userName": "scim@example.com",
		"name":     map[string]any{"givenName": "Scim", "familyName": "User"},
	})
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"userName":"scim@example.com"`) {
		t.Fatalf("create user = %d body=%s", created.Code, created.Body)
	}
	var userBody struct {
		ID string `json:"id"`
	}
	userBody = decode[struct {
		ID string `json:"id"`
	}](t, created)
	if userBody.ID == "" {
		t.Fatal("missing scim user id")
	}

	group := scim(http.MethodPost, "/scim/v2/Groups", map[string]any{
		"schemas":     []string{"urn:ietf:params:scim:schemas:core:2.0:Group"},
		"displayName": "SCIM Team",
	})
	if group.Code != http.StatusCreated {
		t.Fatalf("create group = %d body=%s", group.Code, group.Body)
	}
	var groupBody struct {
		ID string `json:"id"`
	}
	groupBody = decode[struct {
		ID string `json:"id"`
	}](t, group)
	patch := scim(http.MethodPatch, "/scim/v2/Groups/"+groupBody.ID, map[string]any{
		"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:PatchOp"},
		"Operations": []map[string]any{
			{"op": "add", "path": "members", "value": []map[string]string{{"value": userBody.ID}}},
		},
	})
	if patch.Code != http.StatusOK || !strings.Contains(patch.Body.String(), userBody.ID) {
		t.Fatalf("patch group = %d body=%s", patch.Code, patch.Body)
	}

	del := scim(http.MethodDelete, "/scim/v2/Users/"+userBody.ID, nil)
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete user = %d body=%s", del.Code, del.Body)
	}
}

func TestOIDCSSO(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var issuer string
	oidcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                                issuer,
				"authorization_endpoint":                issuer + "/auth",
				"token_endpoint":                        issuer + "/token",
				"jwks_uri":                              issuer + "/jwks",
				"response_types_supported":              []string{"code"},
				"subject_types_supported":               []string{"public"},
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"keys": []map[string]any{{
					"kty": "RSA", "kid": "test", "use": "sig", "alg": "RS256",
					"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
					"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
				}},
			})
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			now := time.Now().Unix()
			header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"test","typ":"JWT"}`))
			payloadBytes, _ := json.Marshal(map[string]any{
				"iss": issuer, "sub": "sso-user-1", "aud": "clavis-test",
				"email": "sso@example.com", "email_verified": true, "name": "SSO User",
				"iat": now, "exp": now + 300,
			})
			payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
			digest := sha256.Sum256([]byte(header + "." + payload))
			signature, _ := rsa.SignPKCS1v15(rand.Reader, key, stdcrypto.SHA256, digest[:])
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "access", "token_type": "Bearer",
				"id_token": header + "." + payload + "." + base64.RawURLEncoding.EncodeToString(signature),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer oidcServer.Close()
	issuer = oidcServer.URL

	f := newFixture(t)
	owner, _ := f.setup(t)
	configure := owner.request(http.MethodPatch, "/api/v1/orgs/acme/settings", map[string]any{
		"sso": map[string]any{
			"enabled": true, "provider": "generic", "client_id": "clavis-test",
			"client_secret": "secret", "issuer": issuer, "role": "Developer",
			"allowed_domains": []string{"example.com"},
		},
	})
	if configure.Code != http.StatusOK || !strings.Contains(configure.Body.String(), `"client_secret_set":true`) {
		t.Fatalf("configure sso = %d body=%s", configure.Code, configure.Body)
	}

	startReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/sso/acme/start?return=/projects", nil)
	startRec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(startRec, startReq)
	if startRec.Code != http.StatusFound {
		t.Fatalf("start = %d body=%s", startRec.Code, startRec.Body)
	}
	location, err := url.Parse(startRec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := location.Query().Get("state")
	if state == "" || location.Query().Get("code_challenge") == "" {
		t.Fatalf("authorize url = %s", location)
	}

	callbackReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/sso/acme/callback?state="+url.QueryEscape(state)+"&code=test-code", nil)
	callbackRec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(callbackRec, callbackReq)
	if callbackRec.Code != http.StatusFound || callbackRec.Header().Get("Location") != "/projects" {
		t.Fatalf("callback = %d location=%s body=%s", callbackRec.Code, callbackRec.Header().Get("Location"), callbackRec.Body)
	}
	ssoClient := &apiClient{t: t, h: f.srv.Handler(), cookies: callbackRec.Result().Cookies()}
	me := ssoClient.request(http.MethodGet, "/api/v1/auth/me", nil)
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), "sso@example.com") {
		t.Fatalf("sso me = %d body=%s", me.Code, me.Body)
	}

	// Enforcing SSO now blocks password sign-in for org members.
	if rec := owner.request(http.MethodPatch, "/api/v1/orgs/acme/settings", map[string]any{
		"sso": map[string]any{"enforce": true},
	}); rec.Code != http.StatusOK {
		t.Fatalf("enforce sso = %d body=%s", rec.Code, rec.Body)
	}
	anonymous := &apiClient{t: t, h: f.srv.Handler()}
	denied := anonymous.request(http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": "owner@example.com", "password": "correct horse battery staple",
	})
	if denied.Code != http.StatusForbidden || !strings.Contains(denied.Body.String(), "sso_required") {
		t.Fatalf("password login with enforced sso = %d body=%s", denied.Code, denied.Body)
	}
}

func TestSlackProtectedWebhook(t *testing.T) {
	type message struct {
		body []byte
	}
	messages := make(chan message, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		messages <- message{body: body}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	f := newFixture(t)
	owner, _ := f.setup(t)
	hook := owner.request(http.MethodPost, "/api/v1/orgs/acme/webhooks", map[string]any{
		"url": srv.URL, "kind": "slack", "protected_only": true,
		"events": []string{"secret.*"}, "enabled": true,
	})
	if hook.Code != http.StatusCreated || !strings.Contains(hook.Body.String(), `"kind":"slack"`) {
		t.Fatalf("create hook = %d body=%s", hook.Code, hook.Body)
	}
	dev := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	if rec := owner.request(http.MethodPost, dev, map[string]any{"key": "DEV_ONLY", "value": "x"}); rec.Code != http.StatusCreated {
		t.Fatalf("dev create = %d", rec.Code)
	}
	prod := "/api/v1/orgs/acme/projects/web-app/environments/prod/secrets"
	if rec := owner.request(http.MethodPost, prod, map[string]any{"key": "PROD_KEY", "value": "x"}); rec.Code != http.StatusCreated {
		t.Fatalf("prod create = %d body=%s", rec.Code, rec.Body)
	}

	select {
	case message := <-messages:
		var payload struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(message.body, &payload); err != nil {
			t.Fatalf("payload = %s (%v)", message.body, err)
		}
		if !strings.Contains(payload.Text, "prod") || strings.Contains(payload.Text, "dev") {
			t.Fatalf("slack text = %q", payload.Text)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("slack notification timed out")
	}
}

func TestSAMLSettingsAndRedirect(t *testing.T) {
	idpKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(42), Subject: pkix.Name{CommonName: "Test IdP"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &idpKey.PublicKey, idpKey)
	if err != nil {
		t.Fatal(err)
	}
	certB64 := base64.StdEncoding.EncodeToString(der)
	metadata := `<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://idp.example.com">
  <IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <KeyDescriptor use="signing"><ds:KeyInfo xmlns:ds="http://www.w3.org/2000/09/xmldsig#"><ds:X509Data><ds:X509Certificate>` + certB64 + `</ds:X509Certificate></ds:X509Data></ds:KeyInfo></KeyDescriptor>
    <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="https://idp.example.com/sso"/>
  </IDPSSODescriptor>
</EntityDescriptor>`

	f := newFixture(t)
	owner, _ := f.setup(t)
	configure := owner.request(http.MethodPatch, "/api/v1/orgs/acme/settings", map[string]any{
		"sso": map[string]any{
			"enabled": true, "provider": "saml", "idp_metadata": metadata,
			"role": "Developer", "allowed_domains": []string{"example.com"},
		},
	})
	if configure.Code != http.StatusOK {
		t.Fatalf("configure saml = %d body=%s", configure.Code, configure.Body)
	}

	metadataReq := httptest.NewRequest(http.MethodGet, "/saml/acme/metadata", nil)
	metadataRec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(metadataRec, metadataReq)
	if metadataRec.Code != http.StatusOK || !strings.Contains(metadataRec.Body.String(), "/saml/acme/acs") {
		t.Fatalf("sp metadata = %d body=%s", metadataRec.Code, metadataRec.Body)
	}

	loginReq := httptest.NewRequest(http.MethodGet, "/saml/acme/login?return=/projects", nil)
	loginRec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusFound {
		t.Fatalf("saml login = %d body=%s", loginRec.Code, loginRec.Body)
	}
	loginURL, err := url.Parse(loginRec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if loginURL.Host != "idp.example.com" || loginURL.Query().Get("SAMLRequest") == "" || loginURL.Query().Get("RelayState") != "/projects" {
		t.Fatalf("saml redirect = %s", loginURL)
	}

	startReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/sso/acme/start", nil)
	startRec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(startRec, startReq)
	if startRec.Code != http.StatusFound || !strings.Contains(startRec.Header().Get("Location"), "idp.example.com") {
		t.Fatalf("sso start = %d location=%s", startRec.Code, startRec.Header().Get("Location"))
	}

	form := url.Values{"SAMLResponse": {"not-a-saml-response"}}
	acsReq := httptest.NewRequest(http.MethodPost, "/saml/acme/acs", strings.NewReader(form.Encode()))
	acsReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	acsRec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(acsRec, acsReq)
	if acsRec.Code != http.StatusFound || !strings.Contains(acsRec.Header().Get("Location"), "sso_error=saml_failed") {
		t.Fatalf("acs = %d location=%s", acsRec.Code, acsRec.Header().Get("Location"))
	}
}

func TestOutgoingWebhooks(t *testing.T) {
	type receivedWebhook struct {
		event     string
		signature string
		body      []byte
	}
	received := make(chan receivedWebhook, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- receivedWebhook{
			event: r.Header.Get("X-Clavis-Event"), signature: r.Header.Get("X-Clavis-Signature"), body: body,
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	f := newFixture(t)
	owner, _ := f.setup(t)
	created := owner.request(http.MethodPost, "/api/v1/orgs/acme/webhooks", map[string]any{
		"url": srv.URL, "events": []string{"secret.*"}, "enabled": true, "secret": "webhook-secret",
	})
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"secret_set":true`) {
		t.Fatalf("create webhook = %d body=%s", created.Code, created.Body)
	}
	dev := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	if rec := owner.request(http.MethodPost, dev, map[string]any{"key": "HOOKED", "value": "value"}); rec.Code != http.StatusCreated {
		t.Fatalf("secret create = %d", rec.Code)
	}

	deadline := time.After(8 * time.Second)
	for {
		select {
		case delivery := <-received:
			mac := hmac.New(sha256.New, []byte("webhook-secret"))
			mac.Write(delivery.body)
			want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
			if delivery.signature != want {
				t.Fatalf("signature = %q want %q", delivery.signature, want)
			}
			if delivery.event == "secret.create" {
				goto delivered
			}
		case <-deadline:
			t.Fatal("webhook delivery timed out")
		}
	}
delivered:
	list := owner.request(http.MethodGet, "/api/v1/orgs/acme/webhooks/deliveries", nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"status":"success"`) {
		t.Fatalf("deliveries = %d body=%s", list.Code, list.Body)
	}
}

func TestOIDCWorkloadIdentity(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var issuer string
	issuerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                                issuer,
				"authorization_endpoint":                issuer + "/auth",
				"token_endpoint":                        issuer + "/token",
				"jwks_uri":                              issuer + "/jwks",
				"response_types_supported":              []string{"id_token"},
				"subject_types_supported":               []string{"public"},
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"keys": []map[string]any{{
					"kty": "RSA", "kid": "test", "use": "sig", "alg": "RS256",
					"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
					"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer issuerServer.Close()
	issuer = issuerServer.URL
	sign := func(repository string) string {
		now := time.Now().Unix()
		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"test","typ":"JWT"}`))
		payloadBytes, _ := json.Marshal(map[string]any{
			"iss": issuer, "aud": "clavis", "sub": "repo:acme/repo:ref:refs/heads/main",
			"repository": repository, "ref": "refs/heads/main", "iat": now, "exp": now + 300,
		})
		payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
		digest := sha256.Sum256([]byte(header + "." + payload))
		signature, _ := rsa.SignPKCS1v15(rand.Reader, key, stdcrypto.SHA256, digest[:])
		return header + "." + payload + "." + base64.RawURLEncoding.EncodeToString(signature)
	}

	f := newFixture(t)
	owner, _ := f.setup(t)
	created := owner.request(http.MethodPost, "/api/v1/orgs/acme/identities", map[string]any{
		"name": "github-actions", "type": "machine", "role": "Developer",
		"project": "web-app", "environment": "dev",
		"oidc": map[string]any{
			"issuer": issuer, "audience": "clavis",
			"claims": map[string]string{"repository": "acme/repo", "ref": "refs/heads/main"},
		},
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create identity = %d body=%s", created.Code, created.Body)
	}

	exchange := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/oidc", strings.NewReader(`{"org":"acme","identity":"github-actions","token":"`+token+`"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(rec, req)
		return rec
	}
	ok := exchange(sign("acme/repo"))
	if ok.Code != http.StatusOK {
		t.Fatalf("exchange = %d body=%s", ok.Code, ok.Body)
	}
	var exchanged struct {
		Token string `json:"token"`
	}
	exchanged = decode[struct {
		Token string `json:"token"`
	}](t, ok)
	if !strings.HasPrefix(exchanged.Token, "clv_mt_") {
		t.Fatalf("token = %q", exchanged.Token)
	}
	bearer := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+exchanged.Token)
		rec := httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(rec, req)
		return rec
	}
	secrets := bearer(http.MethodGet, "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets")
	if secrets.Code != http.StatusOK {
		t.Fatalf("workload token read = %d body=%s", secrets.Code, secrets.Body)
	}
	denied := exchange(sign("evil/repo"))
	if denied.Code != http.StatusForbidden {
		t.Fatalf("claim mismatch = %d body=%s", denied.Code, denied.Body)
	}
}

func TestGenericHTTPSync(t *testing.T) {
	type delivery struct {
		body      []byte
		signature string
	}
	deliveries := make(chan delivery, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		deliveries <- delivery{body: body, signature: r.Header.Get("X-Clavis-Signature")}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	f := newFixture(t)
	owner, _ := f.setup(t)
	created := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/syncs", map[string]any{
		"environment": "dev", "provider": "http", "endpoint": srv.URL, "secret": "sync-secret",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create sync = %d body=%s", created.Code, created.Body)
	}
	var syncBody struct {
		ID string `json:"id"`
	}
	syncBody = decode[struct {
		ID string `json:"id"`
	}](t, created)

	dev := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	if rec := owner.request(http.MethodPost, dev, map[string]any{"key": "SYNCED", "value": "sync-value"}); rec.Code != http.StatusCreated {
		t.Fatalf("secret = %d", rec.Code)
	}
	runURL := "/api/v1/orgs/acme/projects/web-app/syncs/" + syncBody.ID + "/run"
	preview := owner.request(http.MethodPost, runURL, map[string]any{"dry_run": true})
	if preview.Code != http.StatusCreated || !strings.Contains(preview.Body.String(), `"count":1`) {
		t.Fatalf("preview = %d body=%s", preview.Code, preview.Body)
	}
	select {
	case <-deliveries:
		t.Fatal("dry run sent values")
	case <-time.After(150 * time.Millisecond):
	}
	if rec := owner.request(http.MethodPost, runURL, map[string]any{}); rec.Code != http.StatusConflict {
		t.Fatalf("unconfirmed run = %d body=%s", rec.Code, rec.Body)
	}
	run := owner.request(http.MethodPost, runURL, map[string]any{"confirm": true})
	if run.Code != http.StatusCreated || !strings.Contains(run.Body.String(), `"status":"success"`) {
		t.Fatalf("run = %d body=%s", run.Code, run.Body)
	}
	select {
	case body := <-deliveries:
		mac := hmac.New(sha256.New, []byte("sync-secret"))
		mac.Write(body.body)
		want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if body.signature != want {
			t.Fatalf("signature = %q want %q", body.signature, want)
		}
		if !strings.Contains(string(body.body), `"SYNCED":"sync-value"`) {
			t.Fatalf("payload = %s", body.body)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("sync did not deliver")
	}
	runs := owner.request(http.MethodGet, "/api/v1/orgs/acme/projects/web-app/syncs/"+syncBody.ID+"/runs", nil)
	if runs.Code != http.StatusOK || !strings.Contains(runs.Body.String(), `"status":"success"`) {
		t.Fatalf("runs = %d body=%s", runs.Code, runs.Body)
	}
}

func TestGitLabSync(t *testing.T) {
	type variable struct {
		method string
		key    string
		value  string
		masked string
		token  string
	}
	created := make(chan variable, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/variables"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/variables"):
			_ = r.ParseForm()
			created <- variable{
				method: r.Method, key: r.Form.Get("key"), value: r.Form.Get("value"),
				masked: r.Form.Get("masked"), token: r.Header.Get("PRIVATE-TOKEN"),
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	f := newFixture(t)
	owner, _ := f.setup(t)
	sync := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/syncs", map[string]any{
		"environment": "dev", "provider": "gitlab", "base_url": srv.URL,
		"gitlab_project": "group/project", "token": "glpat-test", "masked": true,
	})
	if sync.Code != http.StatusCreated {
		t.Fatalf("create gitlab sync = %d body=%s", sync.Code, sync.Body)
	}
	var syncBody struct {
		ID string `json:"id"`
	}
	syncBody = decode[struct {
		ID string `json:"id"`
	}](t, sync)
	dev := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	if rec := owner.request(http.MethodPost, dev, map[string]any{"key": "GITLAB_KEY", "value": "gitlab-value"}); rec.Code != http.StatusCreated {
		t.Fatalf("secret = %d", rec.Code)
	}
	run := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/syncs/"+syncBody.ID+"/run", map[string]any{"confirm": true})
	if run.Code != http.StatusCreated || !strings.Contains(run.Body.String(), `"status":"success"`) {
		t.Fatalf("run = %d body=%s", run.Code, run.Body)
	}
	select {
	case got := <-created:
		if got.key != "GITLAB_KEY" || got.value != "gitlab-value" || got.masked != "true" || got.token != "glpat-test" {
			t.Fatalf("variable = %+v", got)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("gitlab variable not created")
	}
}

func TestGitHubSync(t *testing.T) {
	publicKey, privateKey, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secretValues := make(chan string, 4)
	variableValues := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/actions/secrets/public-key"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"key_id": "key-1",
				"key":    base64.StdEncoding.EncodeToString(publicKey[:]),
			})
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/actions/secrets/SECRET_KEY"):
			var body struct {
				EncryptedValue string `json:"encrypted_value"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			sealed, _ := base64.StdEncoding.DecodeString(body.EncryptedValue)
			plaintext, ok := box.OpenAnonymous(nil, sealed, publicKey, privateKey)
			if !ok {
				t.Error("could not decrypt sealed secret")
			}
			secretValues <- string(plaintext)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/actions/variables"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"variables":[]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/actions/variables"):
			var body struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			variableValues <- body.Name + "=" + body.Value
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	f := newFixture(t)
	owner, _ := f.setup(t)
	sync := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/syncs", map[string]any{
		"environment": "dev", "provider": "github", "base_url": srv.URL,
		"github_repo": "acme/repo", "github_scope": "repo", "token": "ghp-test",
	})
	if sync.Code != http.StatusCreated {
		t.Fatalf("create github sync = %d body=%s", sync.Code, sync.Body)
	}
	var syncBody struct {
		ID string `json:"id"`
	}
	syncBody = decode[struct {
		ID string `json:"id"`
	}](t, sync)
	dev := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	if rec := owner.request(http.MethodPost, dev, map[string]any{"key": "SECRET_KEY", "value": "super-secret"}); rec.Code != http.StatusCreated {
		t.Fatalf("secret = %d", rec.Code)
	}
	if rec := owner.request(http.MethodPost, dev, map[string]any{"key": "VAR_KEY", "value": "config-value", "kind": "variable"}); rec.Code != http.StatusCreated {
		t.Fatalf("variable = %d", rec.Code)
	}
	run := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/syncs/"+syncBody.ID+"/run", map[string]any{"confirm": true})
	if run.Code != http.StatusCreated || !strings.Contains(run.Body.String(), `"status":"success"`) {
		t.Fatalf("run = %d body=%s", run.Code, run.Body)
	}
	select {
	case value := <-secretValues:
		if value != "super-secret" {
			t.Fatalf("sealed secret = %q", value)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("github secret not delivered")
	}
	select {
	case value := <-variableValues:
		if value != "VAR_KEY=config-value" {
			t.Fatalf("variable = %q", value)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("github variable not delivered")
	}
}

func TestVercelSync(t *testing.T) {
	type envVar struct {
		Key    string   `json:"key"`
		Value  string   `json:"value"`
		Type   string   `json:"type"`
		Target []string `json:"target"`
		Token  string   `json:"-"`
	}
	created := make(chan envVar, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/env"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"envs":[]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/env"):
			var body envVar
			_ = json.NewDecoder(r.Body).Decode(&body)
			body.Token = r.Header.Get("Authorization")
			created <- body
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	f := newFixture(t)
	owner, _ := f.setup(t)
	sync := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/syncs", map[string]any{
		"environment": "dev", "provider": "vercel", "base_url": srv.URL,
		"vercel_project": "proj", "token": "vercel-test",
	})
	if sync.Code != http.StatusCreated {
		t.Fatalf("create vercel sync = %d body=%s", sync.Code, sync.Body)
	}
	var syncBody struct {
		ID string `json:"id"`
	}
	syncBody = decode[struct {
		ID string `json:"id"`
	}](t, sync)
	dev := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	owner.request(http.MethodPost, dev, map[string]any{"key": "V_SECRET", "value": "secret-value"})
	owner.request(http.MethodPost, dev, map[string]any{"key": "V_VAR", "value": "plain-value", "kind": "variable"})
	run := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/syncs/"+syncBody.ID+"/run", map[string]any{"confirm": true})
	if run.Code != http.StatusCreated || !strings.Contains(run.Body.String(), `"status":"success"`) {
		t.Fatalf("run = %d body=%s", run.Code, run.Body)
	}
	seen := map[string]envVar{}
	for i := 0; i < 2; i++ {
		select {
		case variable := <-created:
			seen[variable.Key] = variable
		case <-time.After(8 * time.Second):
			t.Fatal("vercel variables not delivered")
		}
	}
	secret := seen["V_SECRET"]
	if secret.Value != "secret-value" || secret.Type != "encrypted" || len(secret.Target) != 1 || secret.Target[0] != "development" || secret.Token != "Bearer vercel-test" {
		t.Fatalf("secret = %+v", secret)
	}
	variable := seen["V_VAR"]
	if variable.Value != "plain-value" || variable.Type != "plain" {
		t.Fatalf("variable = %+v", variable)
	}
}

func TestAWSSync(t *testing.T) {
	type putParameter struct {
		target string
		auth   string
		name   string
		value  string
		kind   string
	}
	calls := make(chan putParameter, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Name  string `json:"Name"`
			Value string `json:"Value"`
			Type  string `json:"Type"`
		}
		_ = json.Unmarshal(body, &payload)
		calls <- putParameter{
			target: r.Header.Get("X-Amz-Target"), auth: r.Header.Get("Authorization"),
			name: payload.Name, value: payload.Value, kind: payload.Type,
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	f := newFixture(t)
	owner, _ := f.setup(t)
	sync := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/syncs", map[string]any{
		"environment": "dev", "provider": "aws", "base_url": srv.URL,
		"aws_region": "eu-central-1", "aws_access_key": "AKID", "aws_secret_key": "SECRET",
		"aws_service": "ssm",
	})
	if sync.Code != http.StatusCreated {
		t.Fatalf("create aws sync = %d body=%s", sync.Code, sync.Body)
	}
	var syncBody struct {
		ID string `json:"id"`
	}
	syncBody = decode[struct {
		ID string `json:"id"`
	}](t, sync)
	dev := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	owner.request(http.MethodPost, dev, map[string]any{"key": "AWS_SECRET", "value": "secret-value"})
	owner.request(http.MethodPost, dev, map[string]any{"key": "AWS_VAR", "value": "plain-value", "kind": "variable"})
	run := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/syncs/"+syncBody.ID+"/run", map[string]any{"confirm": true})
	if run.Code != http.StatusCreated || !strings.Contains(run.Body.String(), `"status":"success"`) {
		t.Fatalf("run = %d body=%s", run.Code, run.Body)
	}
	got := map[string]putParameter{}
	for i := 0; i < 2; i++ {
		select {
		case call := <-calls:
			got[call.name] = call
		case <-time.After(8 * time.Second):
			t.Fatal("aws parameters not delivered")
		}
	}
	secret := got["clavis/web-app/dev/AWS_SECRET"]
	if secret.target != "AmazonSSM.PutParameter" || secret.value != "secret-value" || secret.kind != "SecureString" ||
		!strings.HasPrefix(secret.auth, "AWS4-HMAC-SHA256 Credential=AKID/") {
		t.Fatalf("secret call = %+v", secret)
	}
	variable := got["clavis/web-app/dev/AWS_VAR"]
	if variable.value != "plain-value" || variable.kind != "String" {
		t.Fatalf("variable call = %+v", variable)
	}
}

func TestGCPSync(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	var tokenURL string
	values := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"gcp-token","token_type":"Bearer"}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/secrets/"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"message":"not found"}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/secrets"):
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, ":addVersion"):
			var body struct {
				Payload struct {
					Data string `json:"data"`
				} `json:"payload"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			decoded, _ := base64.StdEncoding.DecodeString(body.Payload.Data)
			values <- string(decoded)
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	tokenURL = srv.URL + "/token"
	account, _ := json.Marshal(map[string]string{
		"client_email": "deployer@project.iam.gserviceaccount.com",
		"private_key":  string(keyPEM),
		"token_uri":    tokenURL,
	})

	f := newFixture(t)
	owner, _ := f.setup(t)
	sync := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/syncs", map[string]any{
		"environment": "dev", "provider": "gcp", "base_url": srv.URL,
		"gcp_project": "project", "gcp_service_account": string(account),
	})
	if sync.Code != http.StatusCreated {
		t.Fatalf("create gcp sync = %d body=%s", sync.Code, sync.Body)
	}
	var syncBody struct {
		ID string `json:"id"`
	}
	syncBody = decode[struct {
		ID string `json:"id"`
	}](t, sync)
	dev := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	if rec := owner.request(http.MethodPost, dev, map[string]any{"key": "GCP_KEY", "value": "gcp-value"}); rec.Code != http.StatusCreated {
		t.Fatalf("secret = %d", rec.Code)
	}
	run := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/syncs/"+syncBody.ID+"/run", map[string]any{"confirm": true})
	if run.Code != http.StatusCreated || !strings.Contains(run.Body.String(), `"status":"success"`) {
		t.Fatalf("run = %d body=%s", run.Code, run.Body)
	}
	select {
	case value := <-values:
		if value != "gcp-value" {
			t.Fatalf("gcp secret = %q", value)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("gcp secret not delivered")
	}
}

func TestNetlifySync(t *testing.T) {
	type envVar struct {
		Key      string `json:"key"`
		IsSecret bool   `json:"is_secret"`
		Values   []struct {
			Value   string `json:"value"`
			Context string `json:"context"`
		} `json:"values"`
	}
	created := make(chan envVar, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/env"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/env"):
			var body envVar
			_ = json.NewDecoder(r.Body).Decode(&body)
			created <- body
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f := newFixture(t)
	owner, _ := f.setup(t)
	sync := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/syncs", map[string]any{
		"environment": "dev", "provider": "netlify", "base_url": srv.URL,
		"netlify_account": "acme", "token": "netlify-token",
	})
	if sync.Code != http.StatusCreated {
		t.Fatalf("create netlify sync = %d body=%s", sync.Code, sync.Body)
	}
	var syncBody struct {
		ID string `json:"id"`
	}
	syncBody = decode[struct {
		ID string `json:"id"`
	}](t, sync)
	dev := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	owner.request(http.MethodPost, dev, map[string]any{"key": "N_KEY", "value": "netlify-value"})
	run := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/syncs/"+syncBody.ID+"/run", map[string]any{"confirm": true})
	if run.Code != http.StatusCreated || !strings.Contains(run.Body.String(), `"status":"success"`) {
		t.Fatalf("run = %d body=%s", run.Code, run.Body)
	}
	select {
	case variable := <-created:
		if variable.Key != "N_KEY" || !variable.IsSecret || len(variable.Values) != 1 ||
			variable.Values[0].Value != "netlify-value" || variable.Values[0].Context != "dev" {
			t.Fatalf("netlify variable = %+v", variable)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("netlify variable not delivered")
	}
}

func TestCloudflareWorkersSync(t *testing.T) {
	type secret struct {
		name  string
		text  string
		kind  string
		token string
	}
	created := make(chan secret, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || !strings.HasSuffix(r.URL.Path, "/secrets") {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("multipart: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		created <- secret{
			name: r.FormValue("name"), text: r.FormValue("text"),
			kind: r.FormValue("type"), token: r.Header.Get("Authorization"),
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()
	f := newFixture(t)
	owner, _ := f.setup(t)
	sync := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/syncs", map[string]any{
		"environment": "dev", "provider": "cloudflare", "base_url": srv.URL,
		"cloudflare_account": "acct", "cloudflare_mode": "workers",
		"cloudflare_script": "worker", "token": "cf-token",
	})
	if sync.Code != http.StatusCreated {
		t.Fatalf("create cloudflare sync = %d body=%s", sync.Code, sync.Body)
	}
	var syncBody struct {
		ID string `json:"id"`
	}
	syncBody = decode[struct {
		ID string `json:"id"`
	}](t, sync)
	dev := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	owner.request(http.MethodPost, dev, map[string]any{"key": "CF_KEY", "value": "cf-value"})
	run := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/syncs/"+syncBody.ID+"/run", map[string]any{"confirm": true})
	if run.Code != http.StatusCreated || !strings.Contains(run.Body.String(), `"status":"success"`) {
		t.Fatalf("run = %d body=%s", run.Code, run.Body)
	}
	select {
	case got := <-created:
		if got.name != "CF_KEY" || got.text != "cf-value" || got.kind != "secret_text" || got.token != "Bearer cf-token" {
			t.Fatalf("cloudflare secret = %+v", got)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("cloudflare secret not delivered")
	}
}

func TestSharedSets(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.setup(t)

	created := owner.request(http.MethodPost, "/api/v1/orgs/acme/sets", map[string]any{
		"name": "Shared API", "slug": "shared",
	})
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"slug":"shared"`) {
		t.Fatalf("create set = %d body=%s", created.Code, created.Body)
	}
	setBase := "/api/v1/orgs/acme/sets/shared"
	if rec := owner.request(http.MethodPut, setBase+"/items/SHARED_KEY", map[string]any{"value": "from-set"}); rec.Code != http.StatusNoContent {
		t.Fatalf("set item = %d body=%s", rec.Code, rec.Body)
	}
	if rec := owner.request(http.MethodPut, setBase+"/items/SHARED_REF", map[string]any{"value": "referenced"}); rec.Code != http.StatusNoContent {
		t.Fatalf("set ref item = %d", rec.Code)
	}
	importURL := "/api/v1/orgs/acme/projects/web-app/environments/dev/sets/shared"
	if rec := owner.request(http.MethodPost, importURL, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("import = %d body=%s", rec.Code, rec.Body)
	}
	imports := owner.request(http.MethodGet, "/api/v1/orgs/acme/projects/web-app/environments/dev/sets", nil)
	if imports.Code != http.StatusOK || !strings.Contains(imports.Body.String(), "shared") {
		t.Fatalf("imports = %d body=%s", imports.Code, imports.Body)
	}

	dev := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	list := owner.request(http.MethodGet, dev+"?reveal=true", nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "from-set") ||
		!strings.Contains(list.Body.String(), `"source_environment":"shared"`) {
		t.Fatalf("list = %d body=%s", list.Code, list.Body)
	}
	get := owner.request(http.MethodGet, dev+"/SHARED_KEY?reveal=true", nil)
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), "from-set") {
		t.Fatalf("get shared = %d body=%s", get.Code, get.Body)
	}

	// A local value wins over the shared set.
	if rec := owner.request(http.MethodPut, dev+"/SHARED_KEY", map[string]any{"value": "local"}); rec.Code != http.StatusOK {
		t.Fatalf("local override = %d body=%s", rec.Code, rec.Body)
	}
	get = owner.request(http.MethodGet, dev+"/SHARED_KEY?reveal=true", nil)
	if !strings.Contains(get.Body.String(), "local") || strings.Contains(get.Body.String(), "from-set") {
		t.Fatalf("local should win: %s", get.Body)
	}

	// ${set:KEY} references resolve from imported sets.
	if rec := owner.request(http.MethodPost, dev, map[string]any{"key": "COMPOSED", "value": "prefix-${set:SHARED_REF}"}); rec.Code != http.StatusCreated {
		t.Fatalf("composed = %d", rec.Code)
	}
	composed := owner.request(http.MethodGet, dev+"/COMPOSED?reveal=true", nil)
	if !strings.Contains(composed.Body.String(), "prefix-referenced") {
		t.Fatalf("composed = %s", composed.Body)
	}

	if rec := owner.request(http.MethodDelete, importURL, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("detach = %d", rec.Code)
	}
	after := owner.request(http.MethodGet, dev+"?reveal=true", nil)
	if strings.Contains(after.Body.String(), "from-set") {
		t.Fatalf("set still applied after detach: %s", after.Body)
	}
}

func TestAWSWorkloadIdentity(t *testing.T) {
	sts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(`<GetCallerIdentityResponse><GetCallerIdentityResult><Arn>arn:aws:iam::123456789012:role/ci</Arn><UserId>AIDAEXAMPLE</UserId><Account>123456789012</Account></GetCallerIdentityResult></GetCallerIdentityResponse>`))
	}))
	defer sts.Close()

	f := newFixtureOptions(t, server.WithAWSSTSEndpoint(sts.URL))
	owner, _ := f.setup(t)
	created := owner.request(http.MethodPost, "/api/v1/orgs/acme/identities", map[string]any{
		"name": "aws-ci", "type": "machine", "role": "Developer",
		"project": "web-app", "environment": "dev",
		"aws": map[string]any{
			"account": "123456789012", "arn_pattern": "arn:aws:iam::123456789012:role/*",
		},
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create aws identity = %d body=%s", created.Code, created.Body)
	}
	exchange := func(identity string) *httptest.ResponseRecorder {
		payload, _ := json.Marshal(map[string]string{
			"org": "acme", "identity": identity,
			"authorization": "AWS4-HMAC-SHA256 Credential=AKID/20260101/eu-central-1/sts/aws4_request, SignedHeaders=host;x-amz-date, Signature=abc",
			"amz_date":      "20260101T000000Z",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/aws", strings.NewReader(string(payload)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(rec, req)
		return rec
	}
	ok := exchange("aws-ci")
	if ok.Code != http.StatusOK {
		t.Fatalf("exchange = %d body=%s", ok.Code, ok.Body)
	}
	var exchanged struct {
		Token string `json:"token"`
	}
	exchanged = decode[struct {
		Token string `json:"token"`
	}](t, ok)
	if !strings.HasPrefix(exchanged.Token, "clv_mt_") {
		t.Fatalf("token = %q", exchanged.Token)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets", nil)
	req.Header.Set("Authorization", "Bearer "+exchanged.Token)
	rec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("workload read = %d body=%s", rec.Code, rec.Body)
	}

	// A different account in the trust is rejected.
	if rec := owner.request(http.MethodPost, "/api/v1/orgs/acme/identities", map[string]any{
		"name": "aws-other", "type": "machine", "project": "web-app", "environment": "dev",
		"aws": map[string]any{"account": "999999999999"},
	}); rec.Code != http.StatusCreated {
		t.Fatalf("create other = %d body=%s", rec.Code, rec.Body)
	}
	if rec := exchange("aws-other"); rec.Code != http.StatusForbidden {
		t.Fatalf("mismatched account = %d body=%s", rec.Code, rec.Body)
	}
}

func TestGitLabDrift(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/variables") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"key":"GITLAB_KEY","value":"remote-old"},{"key":"MATCHED","value":"same"}]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	f := newFixture(t)
	owner, _ := f.setup(t)
	sync := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/syncs", map[string]any{
		"environment": "dev", "provider": "gitlab", "base_url": srv.URL,
		"gitlab_project": "group/project", "token": "glpat",
	})
	if sync.Code != http.StatusCreated {
		t.Fatalf("create = %d body=%s", sync.Code, sync.Body)
	}
	var syncBody struct {
		ID string `json:"id"`
	}
	syncBody = decode[struct {
		ID string `json:"id"`
	}](t, sync)
	dev := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	owner.request(http.MethodPost, dev, map[string]any{"key": "GITLAB_KEY", "value": "local-new"})
	owner.request(http.MethodPost, dev, map[string]any{"key": "MATCHED", "value": "same"})
	drift := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/syncs/"+syncBody.ID+"/drift", nil)
	if drift.Code != http.StatusOK || !strings.Contains(drift.Body.String(), `"supported":true`) ||
		!strings.Contains(drift.Body.String(), "GITLAB_KEY") || strings.Contains(drift.Body.String(), "MATCHED") {
		t.Fatalf("drift = %d body=%s", drift.Code, drift.Body)
	}
	list := owner.request(http.MethodGet, "/api/v1/orgs/acme/projects/web-app/syncs", nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"drift_keys":["GITLAB_KEY"]`) {
		t.Fatalf("list = %d body=%s", list.Code, list.Body)
	}
}

func TestGitHubPreviewEnvironments(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.setup(t)
	if rec := owner.request(http.MethodPatch, "/api/v1/orgs/acme/settings", map[string]any{
		"preview_enabled": true, "github_webhook_secret": "hook-secret", "preview_retention_days": 7,
	}); rec.Code != http.StatusOK {
		t.Fatalf("settings = %d body=%s", rec.Code, rec.Body)
	}
	webhook := func(action string, number int, signature string) *httptest.ResponseRecorder {
		payload, _ := json.Marshal(map[string]any{
			"action": action, "number": number,
			"pull_request": map[string]any{
				"head": map[string]string{"ref": "feature/test"},
				"base": map[string]string{"ref": "main"},
			},
		})
		if signature == "" {
			mac := hmac.New(sha256.New, []byte("hook-secret"))
			mac.Write(payload)
			signature = "sha256=" + hex.EncodeToString(mac.Sum(nil))
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/integrations/github/webhook", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-GitHub-Event", "pull_request")
		req.Header.Set("X-Hub-Signature-256", signature)
		rec := httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(rec, req)
		return rec
	}
	if rec := webhook("opened", 42, "sha256=bad"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad signature = %d body=%s", rec.Code, rec.Body)
	}
	if rec := webhook("opened", 42, ""); rec.Code != http.StatusAccepted {
		t.Fatalf("open = %d body=%s", rec.Code, rec.Body)
	}
	project := owner.request(http.MethodGet, "/api/v1/orgs/acme/projects/web-app", nil)
	if project.Code != http.StatusOK || !strings.Contains(project.Body.String(), `"slug":"pr-42"`) {
		t.Fatalf("project envs = %d body=%s", project.Code, project.Body)
	}
	if rec := webhook("closed", 42, ""); rec.Code != http.StatusAccepted {
		t.Fatalf("close = %d body=%s", rec.Code, rec.Body)
	}
	project = owner.request(http.MethodGet, "/api/v1/orgs/acme/projects/web-app", nil)
	if strings.Contains(project.Body.String(), `"slug":"pr-42"`) {
		t.Fatalf("preview not deleted: %s", project.Body)
	}
}

func TestSyncDeleteMissing(t *testing.T) {
	deleted := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/variables"):
			_, _ = w.Write([]byte(`[{"key":"EXTRA_KEY","value":"stale"}]`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/variables"):
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodDelete:
			deleted <- r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f := newFixture(t)
	owner, _ := f.setup(t)
	sync := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/syncs", map[string]any{
		"environment": "dev", "provider": "gitlab", "base_url": srv.URL,
		"gitlab_project": "group/project", "token": "glpat", "delete_missing": true,
	})
	if sync.Code != http.StatusCreated || !strings.Contains(sync.Body.String(), `"delete_missing":true`) {
		t.Fatalf("create = %d body=%s", sync.Code, sync.Body)
	}
	var syncBody struct {
		ID string `json:"id"`
	}
	syncBody = decode[struct {
		ID string `json:"id"`
	}](t, sync)
	dev := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	owner.request(http.MethodPost, dev, map[string]any{"key": "KEEP_KEY", "value": "keep"})
	run := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/syncs/"+syncBody.ID+"/run", map[string]any{"confirm": true})
	if run.Code != http.StatusCreated || !strings.Contains(run.Body.String(), `"status":"success"`) {
		t.Fatalf("run = %d body=%s", run.Code, run.Body)
	}
	select {
	case path := <-deleted:
		if !strings.HasSuffix(path, "/variables/EXTRA_KEY") {
			t.Fatalf("deleted path = %s", path)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("extra key was not deleted")
	}
}

func TestSetupDefaultsProject(t *testing.T) {
	f := newFixture(t)
	tokenBytes, err := os.ReadFile(filepath.Join(f.dir, "setup-token"))
	if err != nil {
		t.Fatal(err)
	}
	c := &apiClient{t: t, h: f.srv.Handler()}
	rec := c.request(http.MethodPost, "/api/v1/setup", map[string]string{
		"token": strings.TrimSpace(string(tokenBytes)), "email": "owner@example.com", "name": "Owner",
		"password": "correct horse battery staple", "org_name": "Acme",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("setup status = %d body=%s", rec.Code, rec.Body)
	}
	projects := c.request(http.MethodGet, "/api/v1/orgs/acme/projects", nil)
	if projects.Code != http.StatusOK || !strings.Contains(projects.Body.String(), `"slug":"default"`) {
		t.Fatalf("projects = %d %s", projects.Code, projects.Body)
	}
}

func TestMachineIdentityTokens(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.setup(t)

	created := owner.request(http.MethodPost, "/api/v1/orgs/acme/identities", map[string]any{
		"name": "ci-runner", "type": "machine", "role": "Developer",
		"project": "web-app", "environment": "dev", "expires_in_days": 30,
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create identity status = %d body=%s", created.Code, created.Body)
	}
	var identity struct {
		Identity struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"identity"`
		Token string `json:"token"`
	}
	identity = decode[struct {
		Identity struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"identity"`
		Token string `json:"token"`
	}](t, created)
	if !strings.HasPrefix(identity.Token, "clv_mt_") || identity.Identity.Type != "machine" {
		t.Fatalf("identity = %+v", identity)
	}

	bearer := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(rec, req)
		return rec
	}
	if rec := bearer(identity.Token); rec.Code != http.StatusOK {
		t.Fatalf("token list status = %d body=%s", rec.Code, rec.Body)
	}

	// Production is outside the token's environment scope.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orgs/acme/projects/web-app/environments/prod/secrets", nil)
	req.Header.Set("Authorization", "Bearer "+identity.Token)
	rec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("prod token status = %d body=%s", rec.Code, rec.Body)
	}

	// The token can write in its scope without CSRF.
	writeReq := httptest.NewRequest(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets",
		strings.NewReader(`{"key":"CI_TOKEN","value":"from-machine"}`))
	writeReq.Header.Set("Authorization", "Bearer "+identity.Token)
	writeReq.Header.Set("Content-Type", "application/json")
	writeRec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(writeRec, writeReq)
	if writeRec.Code != http.StatusCreated {
		t.Fatalf("token write status = %d body=%s", writeRec.Code, writeRec.Body)
	}

	// Tampering with the checksum is rejected.
	tampered := []byte(identity.Token)
	if tampered[len(tampered)-1] == '0' {
		tampered[len(tampered)-1] = '1'
	} else {
		tampered[len(tampered)-1] = '0'
	}
	if rec := bearer(string(tampered)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("tampered token status = %d", rec.Code)
	}

	// Rotation revokes the old token.
	rotate := owner.requestHeaders(http.MethodPost, "/api/v1/orgs/acme/identities/"+identity.Identity.ID+"/tokens", nil,
		map[string]string{"X-Clavis-Reauth": owner.reauth("correct horse battery staple")})
	if rotate.Code != http.StatusCreated {
		t.Fatalf("rotate status = %d body=%s", rotate.Code, rotate.Body)
	}
	var rotated struct {
		Token string `json:"token"`
	}
	rotated = decode[struct {
		Token string `json:"token"`
	}](t, rotate)
	if rotated.Token == "" || rotated.Token == identity.Token {
		t.Fatalf("rotated token = %q", rotated.Token)
	}
	if rec := bearer(identity.Token); rec.Code != http.StatusUnauthorized {
		t.Fatalf("old token after rotate = %d", rec.Code)
	}
	if rec := bearer(rotated.Token); rec.Code != http.StatusOK {
		t.Fatalf("new token = %d body=%s", rec.Code, rec.Body)
	}

	// Revocation invalidates the new token.
	revoke := owner.requestHeaders(http.MethodDelete, "/api/v1/orgs/acme/identities/"+identity.Identity.ID, nil,
		map[string]string{"X-Clavis-Reauth": owner.reauth("correct horse battery staple")})
	if revoke.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d body=%s", revoke.Code, revoke.Body)
	}
	if rec := bearer(rotated.Token); rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token status = %d", rec.Code)
	}

	// Listing identities shows the token prefix but never the secret.
	list := owner.request(http.MethodGet, "/api/v1/orgs/acme/identities", nil)
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), rotated.Token) {
		t.Fatalf("identities list = %d %s", list.Code, list.Body)
	}
}

func TestPreferences(t *testing.T) {
	f := newFixture(t)
	c, _ := f.setup(t)
	patch := c.request(http.MethodPatch, "/api/v1/auth/preferences", map[string]string{"theme": "light", "appearance": "warm"})
	if patch.Code != http.StatusOK {
		t.Fatalf("preferences status = %d body=%s", patch.Code, patch.Body)
	}
	me := c.request(http.MethodGet, "/api/v1/auth/me", nil)
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"theme":"light"`) || !strings.Contains(me.Body.String(), `"appearance":"warm"`) {
		t.Fatalf("me = %d %s", me.Code, me.Body)
	}
	bad := c.request(http.MethodPatch, "/api/v1/auth/preferences", map[string]string{"theme": "neon", "appearance": "warm"})
	if bad.Code == http.StatusOK {
		t.Fatalf("invalid theme accepted: %s", bad.Body)
	}
}

func TestPasswordResetFlow(t *testing.T) {
	f := newFixture(t)
	c, _ := f.setup(t)

	forgot := c.request(http.MethodPost, "/api/v1/auth/password/forgot", map[string]string{"email": "owner@example.com"})
	if forgot.Code != http.StatusNoContent {
		t.Fatalf("forgot status = %d body=%s", forgot.Code, forgot.Body)
	}
	messages := f.mailer.Messages()
	if len(messages) != 1 || messages[0].To != "owner@example.com" || !strings.Contains(messages[0].Text, "/reset?token=") {
		t.Fatalf("reset email = %+v", messages)
	}
	// Unknown accounts get the same response and no email.
	unknown := (&apiClient{t: t, h: f.srv.Handler()}).request(http.MethodPost, "/api/v1/auth/password/forgot", map[string]string{"email": "nobody@example.com"})
	if unknown.Code != http.StatusNoContent || len(f.mailer.Messages()) != 1 {
		t.Fatalf("unknown forgot = %d messages=%d", unknown.Code, len(f.mailer.Messages()))
	}

	raw := messages[0].Text[strings.Index(messages[0].Text, "/reset?token=")+len("/reset?token="):]
	token := strings.Fields(raw)[0]

	weak := (&apiClient{t: t, h: f.srv.Handler()}).request(http.MethodPost, "/api/v1/auth/password/reset", map[string]string{"token": token, "password": "short"})
	if weak.Code != http.StatusUnprocessableEntity {
		t.Fatalf("weak reset status = %d body=%s", weak.Code, weak.Body)
	}

	reset := (&apiClient{t: t, h: f.srv.Handler()}).request(http.MethodPost, "/api/v1/auth/password/reset", map[string]string{
		"token": token, "password": "a brand new password",
	})
	if reset.Code != http.StatusNoContent {
		t.Fatalf("reset status = %d body=%s", reset.Code, reset.Body)
	}
	// Existing sessions are revoked.
	if me := c.request(http.MethodGet, "/api/v1/auth/me", nil); me.Code != http.StatusUnauthorized {
		t.Fatalf("old session after reset = %d", me.Code)
	}
	anon := &apiClient{t: t, h: f.srv.Handler()}
	if old := anon.request(http.MethodPost, "/api/v1/auth/login", map[string]string{"email": "owner@example.com", "password": "correct horse battery staple"}); old.Code != http.StatusUnauthorized {
		t.Fatalf("old password still works: %d", old.Code)
	}
	if fresh := anon.request(http.MethodPost, "/api/v1/auth/login", map[string]string{"email": "owner@example.com", "password": "a brand new password"}); fresh.Code != http.StatusOK {
		t.Fatalf("new password login = %d body=%s", fresh.Code, fresh.Body)
	}
	replay := (&apiClient{t: t, h: f.srv.Handler()}).request(http.MethodPost, "/api/v1/auth/password/reset", map[string]string{
		"token": token, "password": "another brand new password",
	})
	if replay.Code != http.StatusGone {
		t.Fatalf("replay status = %d body=%s", replay.Code, replay.Body)
	}
}

func TestInviteEmailDelivery(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.setup(t)
	rec := owner.request(http.MethodPost, "/api/v1/orgs/acme/invites", map[string]any{
		"email": "new@example.com", "role": "Viewer",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("invite status = %d body=%s", rec.Code, rec.Body)
	}
	messages := f.mailer.Messages()
	if len(messages) != 1 || messages[0].To != "new@example.com" || !strings.Contains(messages[0].Text, "join?token=") {
		t.Fatalf("invite email = %+v", messages)
	}
}

func TestProjectScopedAccessGrants(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.setup(t)

	invite := owner.request(http.MethodPost, "/api/v1/orgs/acme/invites", map[string]any{"role": "Viewer"})
	if invite.Code != http.StatusCreated {
		t.Fatalf("invite = %d body=%s", invite.Code, invite.Body)
	}
	var inv struct {
		URL string `json:"url"`
	}
	inv = decode[struct {
		URL string `json:"url"`
	}](t, invite)
	token := inv.URL[strings.Index(inv.URL, "token=")+len("token="):]

	viewer := &apiClient{t: t, h: f.srv.Handler()}
	accept := viewer.request(http.MethodPost, "/api/v1/invites/"+token+"/accept", map[string]string{
		"name": "Viewer", "email": "viewer@example.com", "password": "another long password",
	})
	if accept.Code != http.StatusOK {
		t.Fatalf("accept = %d body=%s", accept.Code, accept.Body)
	}
	accepted := decode[setupResponse](t, accept)

	// A Viewer cannot write anywhere yet.
	devPath := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	if rec := viewer.request(http.MethodPost, devPath, map[string]string{"key": "DENIED", "value": "x"}); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer write = %d body=%s", rec.Code, rec.Body)
	}

	grant := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/access", map[string]any{
		"user_id": accepted.User.ID, "role": "Developer", "environment": "dev",
	})
	if grant.Code != http.StatusCreated {
		t.Fatalf("grant = %d body=%s", grant.Code, grant.Body)
	}
	var grantBody struct {
		ID string `json:"id"`
	}
	grantBody = decode[struct {
		ID string `json:"id"`
	}](t, grant)

	if rec := viewer.request(http.MethodPost, devPath, map[string]string{"key": "ALLOWED", "value": "ok"}); rec.Code != http.StatusCreated {
		t.Fatalf("scoped write = %d body=%s", rec.Code, rec.Body)
	}
	prodPath := "/api/v1/orgs/acme/projects/web-app/environments/prod/secrets"
	if rec := viewer.request(http.MethodPost, prodPath, map[string]string{"key": "DENIED", "value": "x"}); rec.Code != http.StatusForbidden {
		t.Fatalf("prod write = %d body=%s", rec.Code, rec.Body)
	}

	list := owner.request(http.MethodGet, "/api/v1/orgs/acme/projects/web-app/access", nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "viewer@example.com") {
		t.Fatalf("list = %d %s", list.Code, list.Body)
	}

	revoke := owner.request(http.MethodDelete, "/api/v1/orgs/acme/projects/web-app/access/"+grantBody.ID, nil)
	if revoke.Code != http.StatusNoContent {
		t.Fatalf("revoke = %d body=%s", revoke.Code, revoke.Body)
	}
	if rec := viewer.request(http.MethodPost, devPath, map[string]string{"key": "DENIED2", "value": "x"}); rec.Code != http.StatusForbidden {
		t.Fatalf("write after revoke = %d body=%s", rec.Code, rec.Body)
	}
}

func TestGroupsAndGroupAccess(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.setup(t)

	invite := owner.request(http.MethodPost, "/api/v1/orgs/acme/invites", map[string]any{"role": "Viewer"})
	var inv struct {
		URL string `json:"url"`
	}
	inv = decode[struct {
		URL string `json:"url"`
	}](t, invite)
	token := inv.URL[strings.Index(inv.URL, "token=")+len("token="):]
	viewer := &apiClient{t: t, h: f.srv.Handler()}
	accept := viewer.request(http.MethodPost, "/api/v1/invites/"+token+"/accept", map[string]string{
		"name": "Grouped", "email": "grouped@example.com", "password": "another long password",
	})
	if accept.Code != http.StatusOK {
		t.Fatalf("accept = %d body=%s", accept.Code, accept.Body)
	}
	accepted := decode[setupResponse](t, accept)

	group := owner.request(http.MethodPost, "/api/v1/orgs/acme/groups", map[string]any{"name": "Backend"})
	if group.Code != http.StatusCreated {
		t.Fatalf("create group = %d body=%s", group.Code, group.Body)
	}
	var groupBody struct {
		ID string `json:"id"`
	}
	groupBody = decode[struct {
		ID string `json:"id"`
	}](t, group)

	if rec := owner.request(http.MethodPost, "/api/v1/orgs/acme/groups/"+groupBody.ID+"/members", map[string]any{
		"user_id": accepted.User.ID,
	}); rec.Code != http.StatusNoContent {
		t.Fatalf("add member = %d body=%s", rec.Code, rec.Body)
	}

	devPath := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	prodPath := "/api/v1/orgs/acme/projects/web-app/environments/prod/secrets"
	if rec := viewer.request(http.MethodPost, devPath, map[string]string{"key": "DENIED", "value": "x"}); rec.Code != http.StatusForbidden {
		t.Fatalf("pre-grant write = %d", rec.Code)
	}

	grant := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/access", map[string]any{
		"subject_kind": "group", "subject_id": groupBody.ID, "role": "Developer", "environment": "dev",
	})
	if grant.Code != http.StatusCreated {
		t.Fatalf("group grant = %d body=%s", grant.Code, grant.Body)
	}
	if !strings.Contains(grant.Body.String(), `"subject_kind":"group"`) || !strings.Contains(grant.Body.String(), "Backend") {
		t.Fatalf("grant body = %s", grant.Body)
	}
	if rec := viewer.request(http.MethodPost, devPath, map[string]string{"key": "ALLOWED", "value": "ok"}); rec.Code != http.StatusCreated {
		t.Fatalf("group write = %d body=%s", rec.Code, rec.Body)
	}
	if rec := viewer.request(http.MethodPost, prodPath, map[string]string{"key": "DENIED", "value": "x"}); rec.Code != http.StatusForbidden {
		t.Fatalf("group prod write = %d", rec.Code)
	}

	list := owner.request(http.MethodGet, "/api/v1/orgs/acme/projects/web-app/access", nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "Backend") {
		t.Fatalf("grant list = %d %s", list.Code, list.Body)
	}

	matrix := owner.request(http.MethodGet, "/api/v1/orgs/acme/access/matrix?project=web-app&environment=dev", nil)
	if matrix.Code != http.StatusOK {
		t.Fatalf("matrix = %d body=%s", matrix.Code, matrix.Body)
	}
	if !strings.Contains(matrix.Body.String(), `"subject_kind":"group"`) || !strings.Contains(matrix.Body.String(), "secrets:write") {
		t.Fatalf("matrix body = %s", matrix.Body)
	}
	explain := owner.request(http.MethodGet, "/api/v1/orgs/acme/access/explain?subject_kind=user&subject_id="+accepted.User.ID+"&permission=secrets:write&project=web-app&environment=dev", nil)
	if explain.Code != http.StatusOK || !strings.Contains(explain.Body.String(), `"allowed":true`) || !strings.Contains(explain.Body.String(), `"source":"group"`) {
		t.Fatalf("explain = %d body=%s", explain.Code, explain.Body)
	}
	denied := owner.request(http.MethodGet, "/api/v1/orgs/acme/access/explain?subject_kind=user&subject_id="+accepted.User.ID+"&permission=secrets:write&project=web-app&environment=prod", nil)
	if denied.Code != http.StatusOK || !strings.Contains(denied.Body.String(), `"allowed":false`) {
		t.Fatalf("explain prod = %d body=%s", denied.Code, denied.Body)
	}

	rename := owner.request(http.MethodPatch, "/api/v1/orgs/acme/groups/"+groupBody.ID, map[string]any{"name": "Platform"})
	if rename.Code != http.StatusOK || !strings.Contains(rename.Body.String(), "Platform") {
		t.Fatalf("rename = %d body=%s", rename.Code, rename.Body)
	}

	groups := owner.request(http.MethodGet, "/api/v1/orgs/acme/groups", nil)
	if groups.Code != http.StatusOK || !strings.Contains(groups.Body.String(), "grouped@example.com") {
		t.Fatalf("groups = %d %s", groups.Code, groups.Body)
	}

	remove := owner.request(http.MethodDelete, "/api/v1/orgs/acme/groups/"+groupBody.ID+"/members/"+accepted.User.ID, nil)
	if remove.Code != http.StatusNoContent {
		t.Fatalf("remove member = %d body=%s", remove.Code, remove.Body)
	}
	if rec := viewer.request(http.MethodPost, devPath, map[string]string{"key": "DENIED2", "value": "x"}); rec.Code != http.StatusForbidden {
		t.Fatalf("write after removal = %d", rec.Code)
	}
}

func TestCustomRoles(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.setup(t)

	// Find a custom role for a scoped project grant.
	created := owner.request(http.MethodPost, "/api/v1/orgs/acme/roles", map[string]any{
		"name": "Deployer", "permissions": []string{"secrets:read-names", "secrets:read-values", "secrets:write"},
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create role = %d body=%s", created.Code, created.Body)
	}
	var role struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Builtin     bool     `json:"builtin"`
		Permissions []string `json:"permissions"`
	}
	role = decode[struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Builtin     bool     `json:"builtin"`
		Permissions []string `json:"permissions"`
	}](t, created)
	if role.ID == "" || role.Builtin || len(role.Permissions) != 3 {
		t.Fatalf("role = %+v", role)
	}

	if rec := owner.request(http.MethodPost, "/api/v1/orgs/acme/roles", map[string]any{
		"name": "Owner", "permissions": []string{"org:read"},
	}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("reserved name = %d body=%s", rec.Code, rec.Body)
	}

	invite := owner.request(http.MethodPost, "/api/v1/orgs/acme/invites", map[string]any{"role": "Viewer"})
	var inv struct {
		URL string `json:"url"`
	}
	inv = decode[struct {
		URL string `json:"url"`
	}](t, invite)
	token := inv.URL[strings.Index(inv.URL, "token=")+len("token="):]
	member := &apiClient{t: t, h: f.srv.Handler()}
	accept := member.request(http.MethodPost, "/api/v1/invites/"+token+"/accept", map[string]string{
		"name": "Custom", "email": "custom@example.com", "password": "another long password",
	})
	if accept.Code != http.StatusOK {
		t.Fatalf("accept = %d body=%s", accept.Code, accept.Body)
	}
	accepted := decode[setupResponse](t, accept)

	grant := owner.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/access", map[string]any{
		"subject_kind": "user", "subject_id": accepted.User.ID, "role": "Deployer", "environment": "dev",
	})
	if grant.Code != http.StatusCreated {
		t.Fatalf("custom role grant = %d body=%s", grant.Code, grant.Body)
	}
	devPath := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	prodPath := "/api/v1/orgs/acme/projects/web-app/environments/prod/secrets"
	if rec := member.request(http.MethodPost, devPath, map[string]string{"key": "CUSTOM", "value": "ok"}); rec.Code != http.StatusCreated {
		t.Fatalf("custom role write = %d body=%s", rec.Code, rec.Body)
	}
	if rec := member.request(http.MethodPost, prodPath, map[string]string{"key": "NOPE", "value": "x"}); rec.Code != http.StatusForbidden {
		t.Fatalf("custom role prod write = %d", rec.Code)
	}

	// Editors can remove a permission and the change applies immediately.
	update := owner.request(http.MethodPatch, "/api/v1/orgs/acme/roles/"+role.ID, map[string]any{
		"name": "Deployer", "permissions": []string{"secrets:read-names"},
	})
	if update.Code != http.StatusOK {
		t.Fatalf("update role = %d body=%s", update.Code, update.Body)
	}
	if rec := member.request(http.MethodPost, devPath, map[string]string{"key": "CUSTOM2", "value": "x"}); rec.Code != http.StatusForbidden {
		t.Fatalf("write after downgrade = %d", rec.Code)
	}

	// Built-ins are immutable.
	list := owner.request(http.MethodGet, "/api/v1/orgs/acme/roles", nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "\"builtin\":true") {
		t.Fatalf("list roles = %d %s", list.Code, list.Body)
	}
	var builtinID string
	var listed struct {
		Roles []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Builtin bool   `json:"builtin"`
		} `json:"roles"`
	}
	listed = decode[struct {
		Roles []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Builtin bool   `json:"builtin"`
		} `json:"roles"`
	}](t, list)
	for _, item := range listed.Roles {
		if item.Builtin {
			builtinID = item.ID
		}
	}
	if rec := owner.request(http.MethodPatch, "/api/v1/orgs/acme/roles/"+builtinID, map[string]any{
		"name": "Hacked", "permissions": []string{"org:read"},
	}); rec.Code != http.StatusForbidden {
		t.Fatalf("edit builtin = %d", rec.Code)
	}

	// A role that is still assigned to a member cannot be deleted.
	if rec := owner.request(http.MethodPatch, "/api/v1/orgs/acme/members/"+accepted.User.ID, map[string]any{
		"role": "Deployer",
	}); rec.Code != http.StatusNoContent {
		t.Fatalf("assign custom role = %d body=%s", rec.Code, rec.Body)
	}
	if rec := owner.request(http.MethodDelete, "/api/v1/orgs/acme/roles/"+role.ID, nil); rec.Code != http.StatusConflict {
		t.Fatalf("delete assigned role = %d body=%s", rec.Code, rec.Body)
	}
	if rec := owner.request(http.MethodPatch, "/api/v1/orgs/acme/members/"+accepted.User.ID, map[string]any{
		"role": "Viewer",
	}); rec.Code != http.StatusNoContent {
		t.Fatalf("restore viewer = %d body=%s", rec.Code, rec.Body)
	}
	if rec := owner.request(http.MethodDelete, "/api/v1/orgs/acme/roles/"+role.ID, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete role = %d body=%s", rec.Code, rec.Body)
	}
}

func TestCustomRoleEscalation(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.setup(t)
	invite := owner.request(http.MethodPost, "/api/v1/orgs/acme/invites", map[string]any{"role": "Admin"})
	var inv struct {
		URL string `json:"url"`
	}
	inv = decode[struct {
		URL string `json:"url"`
	}](t, invite)
	token := inv.URL[strings.Index(inv.URL, "token=")+len("token="):]
	admin := &apiClient{t: t, h: f.srv.Handler()}
	if rec := admin.request(http.MethodPost, "/api/v1/invites/"+token+"/accept", map[string]string{
		"name": "Admin", "email": "admin@example.com", "password": "another long password",
	}); rec.Code != http.StatusOK {
		t.Fatalf("accept = %d", rec.Code)
	}
	if rec := admin.request(http.MethodPost, "/api/v1/orgs/acme/roles", map[string]any{
		"name": "Danger", "permissions": []string{"org:delete"},
	}); rec.Code != http.StatusForbidden {
		t.Fatalf("escalation = %d body=%s", rec.Code, rec.Body)
	}
	if rec := admin.request(http.MethodPost, "/api/v1/orgs/acme/roles", map[string]any{
		"name": "Reader", "permissions": []string{"secrets:read-names"},
	}); rec.Code != http.StatusCreated {
		t.Fatalf("admin role = %d body=%s", rec.Code, rec.Body)
	}
}

func TestSecretOwnerGroup(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.setup(t)

	group := owner.request(http.MethodPost, "/api/v1/orgs/acme/groups", map[string]any{"name": "Payments"})
	var groupBody struct {
		ID string `json:"id"`
	}
	groupBody = decode[struct {
		ID string `json:"id"`
	}](t, group)

	devPath := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	created := owner.request(http.MethodPost, devPath, map[string]any{
		"key": "STRIPE_KEY", "value": "sk_test", "owner_group_id": groupBody.ID,
	})
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"owner_group_name":"Payments"`) {
		t.Fatalf("create = %d body=%s", created.Code, created.Body)
	}

	list := owner.request(http.MethodGet, devPath, nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"owner_group_id":"`+groupBody.ID+`"`) {
		t.Fatalf("list = %d body=%s", list.Code, list.Body)
	}

	if rec := owner.request(http.MethodPut, devPath+"/STRIPE_KEY", map[string]any{
		"value": "sk_test_2", "owner_group_id": "missing",
	}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid owner group = %d body=%s", rec.Code, rec.Body)
	}

	updated := owner.request(http.MethodPut, devPath+"/STRIPE_KEY", map[string]any{
		"value": "sk_test_2", "owner_group_id": "",
	})
	if updated.Code != http.StatusOK || strings.Contains(updated.Body.String(), "owner_group_id") {
		t.Fatalf("clear owner group = %d body=%s", updated.Code, updated.Body)
	}
}

func TestEmailVerification(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.setup(t)
	if me := owner.request(http.MethodGet, "/api/v1/auth/me", nil); !strings.Contains(me.Body.String(), `"email_verified":true`) {
		t.Fatalf("setup owner should be verified: %s", me.Body)
	}

	// Invite without an email restriction produces an unverified account.
	invite := owner.request(http.MethodPost, "/api/v1/orgs/acme/invites", map[string]any{"role": "Viewer"})
	var inv struct {
		URL string `json:"url"`
	}
	inv = decode[struct {
		URL string `json:"url"`
	}](t, invite)
	token := inv.URL[strings.Index(inv.URL, "token=")+len("token="):]
	newbie := &apiClient{t: t, h: f.srv.Handler()}
	accept := newbie.request(http.MethodPost, "/api/v1/invites/"+token+"/accept", map[string]string{
		"name": "Unverified", "email": "unverified@example.com", "password": "another long password",
	})
	if accept.Code != http.StatusOK {
		t.Fatalf("accept = %d body=%s", accept.Code, accept.Body)
	}
	me := newbie.request(http.MethodGet, "/api/v1/auth/me", nil)
	if !strings.Contains(me.Body.String(), `"email_verified":false`) {
		t.Fatalf("expected unverified: %s", me.Body)
	}

	request := newbie.request(http.MethodPost, "/api/v1/auth/verify/request", nil)
	if request.Code != http.StatusNoContent {
		t.Fatalf("verify request = %d body=%s", request.Code, request.Body)
	}
	messages := f.mailer.Messages()
	if len(messages) == 0 {
		t.Fatal("no verification email")
	}
	last := messages[len(messages)-1]
	if last.To != "unverified@example.com" || !strings.Contains(last.Text, "/verify?token=") {
		t.Fatalf("verification email = %+v", last)
	}
	raw := last.Text[strings.Index(last.Text, "/verify?token=")+len("/verify?token="):]
	verifyToken := strings.Fields(raw)[0]

	verify := newbie.request(http.MethodPost, "/api/v1/auth/verify", map[string]string{"token": verifyToken})
	if verify.Code != http.StatusNoContent {
		t.Fatalf("verify = %d body=%s", verify.Code, verify.Body)
	}
	me = newbie.request(http.MethodGet, "/api/v1/auth/me", nil)
	if !strings.Contains(me.Body.String(), `"email_verified":true`) {
		t.Fatalf("expected verified: %s", me.Body)
	}
	replay := newbie.request(http.MethodPost, "/api/v1/auth/verify", map[string]string{"token": verifyToken})
	if replay.Code != http.StatusGone {
		t.Fatalf("replay = %d body=%s", replay.Code, replay.Body)
	}
}

func TestOrgAppearanceDefault(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.setup(t)

	settings := owner.request(http.MethodPatch, "/api/v1/orgs/acme/settings", map[string]string{"default_appearance": "warm"})
	if settings.Code != http.StatusOK || !strings.Contains(settings.Body.String(), `"default_appearance":"warm"`) {
		t.Fatalf("settings = %d %s", settings.Code, settings.Body)
	}

	invite := owner.request(http.MethodPost, "/api/v1/orgs/acme/invites", map[string]any{"role": "Viewer"})
	var inv struct {
		URL string `json:"url"`
	}
	inv = decode[struct {
		URL string `json:"url"`
	}](t, invite)
	token := inv.URL[strings.Index(inv.URL, "token=")+len("token="):]
	member := &apiClient{t: t, h: f.srv.Handler()}
	accept := member.request(http.MethodPost, "/api/v1/invites/"+token+"/accept", map[string]string{
		"name": "Warm Member", "email": "warm@example.com", "password": "another long password",
	})
	if accept.Code != http.StatusOK || !strings.Contains(accept.Body.String(), `"appearance":"warm"`) {
		t.Fatalf("accept = %d %s", accept.Code, accept.Body)
	}
	if me := member.request(http.MethodGet, "/api/v1/auth/me", nil); !strings.Contains(me.Body.String(), `"appearance":"warm"`) || !strings.Contains(me.Body.String(), `"theme":"system"`) {
		t.Fatalf("member preferences = %s", me.Body)
	}
}

func TestBatchSecretsWithETag(t *testing.T) {
	f := newFixture(t)
	c, _ := f.setup(t)
	base := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	for _, key := range []string{"ALPHA", "GAMMA"} {
		rec := c.request(http.MethodPost, base, map[string]string{"key": key, "value": "old-" + key})
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s = %d body=%s", key, rec.Code, rec.Body)
		}
	}
	list := c.request(http.MethodGet, base, nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list = %d", list.Code)
	}
	etag := list.Header().Get("ETag")
	if etag == "" {
		t.Fatal("list response has no ETag")
	}

	batch := c.request(http.MethodPatch, base+":batch", map[string]any{
		"changes": []map[string]any{
			{"key": "ALPHA", "value": "new-alpha", "kind": "secret"},
			{"key": "BETA", "value": "new-beta", "kind": "secret"},
		},
		"deletes": []string{"GAMMA"},
		"message": "batch test",
	})
	// Huma passes a missing header through; the service rejects it as a
	// precondition failure rather than writing without optimistic locking.
	if batch.Code != http.StatusPreconditionFailed {
		t.Fatalf("missing If-Match status = %d body=%s", batch.Code, batch.Body)
	}

	req := httptest.NewRequest(http.MethodPatch, base+":batch",
		strings.NewReader(`{"changes":[{"key":"ALPHA","value":"new-alpha","kind":"secret"},{"key":"BETA","value":"new-beta","kind":"secret"}],"deletes":["GAMMA"],"message":"batch test"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", etag)
	for _, cookie := range c.cookies {
		req.AddCookie(cookie)
	}
	req.Header.Set("X-CSRF-Token", c.csrf)
	rec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("batch = %d body=%s", rec.Code, rec.Body)
	}
	var result struct {
		Created int    `json:"created"`
		Updated int    `json:"updated"`
		Deleted int    `json:"deleted"`
		ETag    string `json:"etag"`
	}
	result = decode[struct {
		Created int    `json:"created"`
		Updated int    `json:"updated"`
		Deleted int    `json:"deleted"`
		ETag    string `json:"etag"`
	}](t, rec)
	if result.Created != 1 || result.Updated != 1 || result.Deleted != 1 || result.ETag == "" {
		t.Fatalf("batch result = %+v", result)
	}

	revealed := c.request(http.MethodGet, base+"/ALPHA?reveal=true", nil)
	if value := decode[secretResponse](t, revealed); value.Value != "new-alpha" {
		t.Fatalf("alpha = %+v", value)
	}
	if beta := c.request(http.MethodGet, base+"/BETA?reveal=true", nil); beta.Code != http.StatusOK {
		t.Fatalf("beta missing: %d", beta.Code)
	}
	if gamma := c.request(http.MethodGet, base+"/GAMMA", nil); gamma.Code != http.StatusNotFound {
		t.Fatalf("gamma still present: %d", gamma.Code)
	}

	// Reusing the old ETag fails.
	stale := httptest.NewRequest(http.MethodPatch, base+":batch",
		strings.NewReader(`{"changes":[{"key":"ALPHA","value":"stale"}]}`))
	stale.Header.Set("Content-Type", "application/json")
	stale.Header.Set("If-Match", etag)
	for _, cookie := range c.cookies {
		stale.AddCookie(cookie)
	}
	stale.Header.Set("X-CSRF-Token", c.csrf)
	staleRec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(staleRec, stale)
	if staleRec.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale batch = %d body=%s", staleRec.Code, staleRec.Body)
	}
}

func TestMultiEnvironmentBatch(t *testing.T) {
	f := newFixture(t)
	c, _ := f.setup(t)
	dev := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	staging := "/api/v1/orgs/acme/projects/web-app/environments/staging/secrets"

	devList := c.request(http.MethodGet, dev, nil)
	stagingList := c.request(http.MethodGet, staging, nil)
	devETag := devList.Header().Get("ETag")
	stagingETag := stagingList.Header().Get("ETag")
	if devETag == "" || stagingETag == "" {
		t.Fatal("missing ETags")
	}

	body := map[string]any{
		"changes": []map[string]any{
			{"environment": "dev", "key": "SHARED", "value": "dev-value"},
			{"environment": "staging", "key": "SHARED", "value": "staging-value"},
		},
		"if_match": map[string]string{"dev": devETag, "staging": stagingETag},
		"message":  "multi env",
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/orgs/acme/projects/web-app/secrets:batch", strings.NewReader(mustJSON(t, body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", c.csrf)
	for _, cookie := range c.cookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("multi batch = %d body=%s", rec.Code, rec.Body)
	}
	var result struct {
		Created int `json:"created"`
	}
	result = decode[struct {
		Created int `json:"created"`
	}](t, rec)
	if result.Created != 2 {
		t.Fatalf("created = %d", result.Created)
	}
	devValue := decode[secretResponse](t, c.request(http.MethodGet, dev+"/SHARED?reveal=true", nil))
	stagingValue := decode[secretResponse](t, c.request(http.MethodGet, staging+"/SHARED?reveal=true", nil))
	if devValue.Value != "dev-value" || stagingValue.Value != "staging-value" {
		t.Fatalf("values = %q / %q", devValue.Value, stagingValue.Value)
	}

	// Reusing stale ETags rolls back every environment.
	stale := map[string]any{
		"changes": []map[string]any{
			{"environment": "dev", "key": "SHARED", "value": "stale-dev"},
			{"environment": "staging", "key": "STALE_ONLY", "value": "stale-staging"},
		},
		"if_match": map[string]string{"dev": devETag, "staging": stagingETag},
	}
	staleReq := httptest.NewRequest(http.MethodPatch, "/api/v1/orgs/acme/projects/web-app/secrets:batch", strings.NewReader(mustJSON(t, stale)))
	staleReq.Header.Set("Content-Type", "application/json")
	staleReq.Header.Set("X-CSRF-Token", c.csrf)
	for _, cookie := range c.cookies {
		staleReq.AddCookie(cookie)
	}
	staleRec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(staleRec, staleReq)
	if staleRec.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale multi batch = %d body=%s", staleRec.Code, staleRec.Body)
	}
	if got := decode[secretResponse](t, c.request(http.MethodGet, dev+"/SHARED?reveal=true", nil)); got.Value != "dev-value" {
		t.Fatalf("dev changed despite rollback: %+v", got)
	}
	if rec := c.request(http.MethodGet, staging+"/STALE_ONLY", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("stale key created: %d", rec.Code)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestSecretFolders(t *testing.T) {
	f := newFixture(t)
	c, _ := f.setup(t)
	base := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	created := c.request(http.MethodPost, base, map[string]any{
		"key": "DB_PASSWORD", "value": "secret", "folder": "/backend/db",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d body=%s", created.Code, created.Body)
	}
	list := c.request(http.MethodGet, base, nil)
	if !strings.Contains(list.Body.String(), `"folder":"/backend/db"`) {
		t.Fatalf("list = %s", list.Body)
	}
	folders := c.request(http.MethodGet, "/api/v1/orgs/acme/projects/web-app/environments/dev/folders", nil)
	if folders.Code != http.StatusOK || !strings.Contains(folders.Body.String(), "/backend/db") {
		t.Fatalf("folders = %d %s", folders.Code, folders.Body)
	}
	updated := c.request(http.MethodPut, base+"/DB_PASSWORD", map[string]any{
		"value": "secret", "kind": "secret", "folder": "/backend",
	})
	if updated.Code != http.StatusOK {
		t.Fatalf("update = %d body=%s", updated.Code, updated.Body)
	}
	list = c.request(http.MethodGet, base, nil)
	if !strings.Contains(list.Body.String(), `"folder":"/backend"`) {
		t.Fatalf("updated list = %s", list.Body)
	}
}

func TestTOTPMFAFlow(t *testing.T) {
	f := newFixture(t)
	c, _ := f.setup(t)

	enroll := c.request(http.MethodPost, "/api/v1/auth/mfa/totp/enroll", nil)
	if enroll.Code != http.StatusOK {
		t.Fatalf("enroll = %d body=%s", enroll.Code, enroll.Body)
	}
	var enrollment struct {
		Secret string `json:"secret"`
		URL    string `json:"url"`
	}
	enrollment = decode[struct {
		Secret string `json:"secret"`
		URL    string `json:"url"`
	}](t, enroll)
	if enrollment.Secret == "" || !strings.HasPrefix(enrollment.URL, "otpauth://totp/") {
		t.Fatalf("enrollment = %+v", enrollment)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(enrollment.Secret)
	if err != nil {
		t.Fatal(err)
	}
	code := authn.TOTPCode(secret, time.Now())

	activate := c.request(http.MethodPost, "/api/v1/auth/mfa/totp/activate", map[string]string{"code": code})
	if activate.Code != http.StatusOK {
		t.Fatalf("activate = %d body=%s", activate.Code, activate.Body)
	}
	var recovery struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	recovery = decode[struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}](t, activate)
	if len(recovery.RecoveryCodes) != authn.RecoveryCodeCount {
		t.Fatalf("recovery codes = %d", len(recovery.RecoveryCodes))
	}

	status := c.request(http.MethodGet, "/api/v1/auth/mfa", nil)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"enabled":true`) {
		t.Fatalf("status = %d %s", status.Code, status.Body)
	}

	anon := &apiClient{t: t, h: f.srv.Handler()}
	login := anon.request(http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": "owner@example.com", "password": "correct horse battery staple",
	})
	var challenge struct {
		MFARequired bool   `json:"mfa_required"`
		Challenge   string `json:"challenge"`
	}
	challenge = decode[struct {
		MFARequired bool   `json:"mfa_required"`
		Challenge   string `json:"challenge"`
	}](t, login)
	if login.Code != http.StatusOK || !challenge.MFARequired || challenge.Challenge == "" {
		t.Fatalf("login = %d %+v", login.Code, challenge)
	}
	if anon.csrf != "" {
		t.Fatal("session cookies issued before MFA")
	}

	wrong := anon.request(http.MethodPost, "/api/v1/auth/mfa/verify", map[string]string{
		"challenge": challenge.Challenge, "code": "000000",
	})
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong code = %d body=%s", wrong.Code, wrong.Body)
	}
	right := anon.request(http.MethodPost, "/api/v1/auth/mfa/verify", map[string]string{
		"challenge": challenge.Challenge, "code": authn.TOTPCode(secret, time.Now()),
	})
	if right.Code != http.StatusOK || !strings.Contains(right.Body.String(), "owner@example.com") {
		t.Fatalf("verify = %d body=%s", right.Code, right.Body)
	}

	// A recovery code works once.
	loginAgain := (&apiClient{t: t, h: f.srv.Handler()}).request(http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": "owner@example.com", "password": "correct horse battery staple",
	})
	var second struct {
		Challenge string `json:"challenge"`
	}
	second = decode[struct {
		Challenge string `json:"challenge"`
	}](t, loginAgain)
	recoveryClient := &apiClient{t: t, h: f.srv.Handler()}
	used := recoveryClient.request(http.MethodPost, "/api/v1/auth/mfa/verify", map[string]string{
		"challenge": second.Challenge, "recovery_code": recovery.RecoveryCodes[0],
	})
	if used.Code != http.StatusOK {
		t.Fatalf("recovery login = %d body=%s", used.Code, used.Body)
	}

	// Disable requires a current TOTP and then login skips MFA.
	disable := c.request(http.MethodPost, "/api/v1/auth/mfa/totp/disable", map[string]string{
		"code": authn.TOTPCode(secret, time.Now()),
	})
	if disable.Code != http.StatusNoContent {
		t.Fatalf("disable = %d body=%s", disable.Code, disable.Body)
	}
	plain := (&apiClient{t: t, h: f.srv.Handler()}).request(http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": "owner@example.com", "password": "correct horse battery staple",
	})
	if plain.Code != http.StatusOK || strings.Contains(plain.Body.String(), `"mfa_required":true`) {
		t.Fatalf("login after disable = %d %s", plain.Code, plain.Body)
	}
}

func TestOrgRequireMFA(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.setup(t)

	invite := owner.request(http.MethodPost, "/api/v1/orgs/acme/invites", map[string]any{"role": "Viewer"})
	var inv struct {
		URL string `json:"url"`
	}
	inv = decode[struct {
		URL string `json:"url"`
	}](t, invite)
	token := inv.URL[strings.Index(inv.URL, "token=")+len("token="):]
	viewer := &apiClient{t: t, h: f.srv.Handler()}
	accept := viewer.request(http.MethodPost, "/api/v1/invites/"+token+"/accept", map[string]string{
		"name": "Viewer", "email": "viewer@example.com", "password": "another long password",
	})
	if accept.Code != http.StatusOK {
		t.Fatalf("accept = %d body=%s", accept.Code, accept.Body)
	}

	settings := owner.request(http.MethodPatch, "/api/v1/orgs/acme/settings", map[string]any{"require_mfa": true})
	if settings.Code != http.StatusOK || !strings.Contains(settings.Body.String(), `"require_mfa":true`) {
		t.Fatalf("settings = %d %s", settings.Code, settings.Body)
	}
	if rec := viewer.request(http.MethodGet, "/api/v1/orgs/acme/projects", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("unverified MFA access = %d body=%s", rec.Code, rec.Body)
	}

	enroll := viewer.request(http.MethodPost, "/api/v1/auth/mfa/totp/enroll", nil)
	var enrollment struct {
		Secret string `json:"secret"`
	}
	enrollment = decode[struct {
		Secret string `json:"secret"`
	}](t, enroll)
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(enrollment.Secret)
	if err != nil {
		t.Fatal(err)
	}
	activate := viewer.request(http.MethodPost, "/api/v1/auth/mfa/totp/activate", map[string]string{
		"code": authn.TOTPCode(secret, time.Now()),
	})
	if activate.Code != http.StatusOK {
		t.Fatalf("activate = %d body=%s", activate.Code, activate.Body)
	}
	if rec := viewer.request(http.MethodGet, "/api/v1/orgs/acme/projects", nil); rec.Code != http.StatusOK {
		t.Fatalf("MFA access = %d body=%s", rec.Code, rec.Body)
	}
}

func TestOrgIPAllowlistAndSessionLimits(t *testing.T) {
	f := newFixture(t)
	owner, setup := f.setup(t)

	settings := owner.request(http.MethodPatch, "/api/v1/orgs/acme/settings", map[string]any{
		"ip_allowlist": []string{"192.0.2.0/24"}, "session_idle_hours": 1,
	})
	if settings.Code != http.StatusOK {
		t.Fatalf("settings = %d %s", settings.Code, settings.Body)
	}
	if rec := owner.request(http.MethodGet, "/api/v1/orgs/acme/projects", nil); rec.Code != http.StatusOK {
		t.Fatalf("allowed IP = %d", rec.Code)
	}

	// Idle timeout from the org policy is enforced on org-scoped routes.
	twoHoursAgo := time.Now().UTC().Add(-2 * time.Hour).Format("2006-01-02T15:04:05.000000000Z")
	if _, err := f.st.DB().Exec(`UPDATE sessions SET last_seen_at = ? WHERE user_id = ?`, twoHoursAgo, setup.User.ID); err != nil {
		t.Fatal(err)
	}
	if rec := owner.request(http.MethodGet, "/api/v1/orgs/acme/projects", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("idle session = %d", rec.Code)
	}

	// Restore activity, then use an allowlist that excludes the test client.
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")
	if _, err := f.st.DB().Exec(`UPDATE sessions SET last_seen_at = ? WHERE user_id = ?`, now, setup.User.ID); err != nil {
		t.Fatal(err)
	}
	settings = owner.request(http.MethodPatch, "/api/v1/orgs/acme/settings", map[string]any{
		"session_idle_hours": 0, "ip_allowlist": []string{"10.0.0.0/8"},
	})
	if settings.Code != http.StatusOK {
		t.Fatalf("settings = %d %s", settings.Code, settings.Body)
	}
	if rec := owner.request(http.MethodGet, "/api/v1/orgs/acme/projects", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("blocked IP = %d body=%s", rec.Code, rec.Body)
	}
}

func TestProtectedEnvironmentPolicy(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.setup(t)

	// Invite a Developer.
	invite := owner.request(http.MethodPost, "/api/v1/orgs/acme/invites", map[string]any{"role": "Developer"})
	var inv struct {
		URL string `json:"url"`
	}
	inv = decode[struct {
		URL string `json:"url"`
	}](t, invite)
	token := inv.URL[strings.Index(inv.URL, "token=")+len("token="):]
	developer := &apiClient{t: t, h: f.srv.Handler()}
	if rec := developer.request(http.MethodPost, "/api/v1/invites/"+token+"/accept", map[string]string{
		"name": "Developer", "email": "developer@example.com", "password": "another long password",
	}); rec.Code != http.StatusOK {
		t.Fatalf("accept = %d body=%s", rec.Code, rec.Body)
	}

	prod := "/api/v1/orgs/acme/projects/web-app/environments/prod/secrets"
	dev := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"

	if rec := developer.request(http.MethodPost, prod, map[string]string{"key": "PROD_KEY", "value": "x"}); rec.Code != http.StatusForbidden {
		t.Fatalf("developer prod write = %d body=%s", rec.Code, rec.Body)
	}
	if rec := developer.request(http.MethodPost, dev, map[string]string{"key": "DEV_KEY", "value": "x"}); rec.Code != http.StatusCreated {
		t.Fatalf("developer dev write = %d body=%s", rec.Code, rec.Body)
	}
	if rec := owner.request(http.MethodPost, prod, map[string]string{"key": "PROD_KEY", "value": "prod-secret"}); rec.Code != http.StatusCreated {
		t.Fatalf("owner prod write = %d body=%s", rec.Code, rec.Body)
	}

	// Production reveal requires a reason.
	if rec := developer.request(http.MethodGet, prod+"/PROD_KEY?reveal=true", nil); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("prod reveal without reason = %d body=%s", rec.Code, rec.Body)
	}
	if rec := developer.request(http.MethodGet, prod+"/PROD_KEY?reveal=true&reason=debugging", nil); rec.Code != http.StatusOK {
		t.Fatalf("prod reveal with reason = %d body=%s", rec.Code, rec.Body)
	}

	// Agent identities are denied production entirely.
	agent := owner.request(http.MethodPost, "/api/v1/orgs/acme/identities", map[string]any{
		"name": "prod-agent", "type": "agent", "role": "Developer",
		"project": "web-app", "environment": "prod",
	})
	var agentResult struct {
		Token string `json:"token"`
	}
	agentResult = decode[struct {
		Token string `json:"token"`
	}](t, agent)
	bearer := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+agentResult.Token)
		rec := httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(rec, req)
		return rec
	}
	if rec := bearer(prod); rec.Code != http.StatusForbidden {
		t.Fatalf("agent prod = %d body=%s", rec.Code, rec.Body)
	}
}

func TestVariablesAreNotSecrets(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.setup(t)

	invite := owner.request(http.MethodPost, "/api/v1/orgs/acme/invites", map[string]any{"role": "Viewer"})
	var inv struct {
		URL string `json:"url"`
	}
	inv = decode[struct {
		URL string `json:"url"`
	}](t, invite)
	token := inv.URL[strings.Index(inv.URL, "token=")+len("token="):]
	viewer := &apiClient{t: t, h: f.srv.Handler()}
	if rec := viewer.request(http.MethodPost, "/api/v1/invites/"+token+"/accept", map[string]string{
		"name": "Viewer", "email": "viewer@example.com", "password": "another long password",
	}); rec.Code != http.StatusOK {
		t.Fatalf("accept = %d body=%s", rec.Code, rec.Body)
	}

	dev := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	if rec := owner.request(http.MethodPost, dev, map[string]string{
		"key": "LOG_LEVEL", "value": "debug", "kind": "variable",
	}); rec.Code != http.StatusCreated {
		t.Fatalf("variable create = %d body=%s", rec.Code, rec.Body)
	}
	if rec := owner.request(http.MethodPost, dev, map[string]string{
		"key": "API_KEY", "value": "super-secret", "kind": "secret",
	}); rec.Code != http.StatusCreated {
		t.Fatalf("secret create = %d body=%s", rec.Code, rec.Body)
	}

	list := viewer.request(http.MethodGet, dev, nil)
	body := list.Body.String()
	if !strings.Contains(body, `"key":"LOG_LEVEL"`) || !strings.Contains(body, `"value":"debug"`) {
		t.Fatalf("variable missing from viewer list: %s", body)
	}
	if strings.Contains(body, "super-secret") {
		t.Fatalf("secret value leaked to viewer list: %s", body)
	}

	revealsBefore := countRevealEvents(t, f)
	if rec := viewer.request(http.MethodGet, dev+"/LOG_LEVEL", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"value":"debug"`) {
		t.Fatalf("variable get = %d body=%s", rec.Code, rec.Body)
	}
	if rec := viewer.request(http.MethodGet, dev+"/LOG_LEVEL?reveal=true", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"value":"debug"`) {
		t.Fatalf("variable reveal = %d body=%s", rec.Code, rec.Body)
	}
	if rec := viewer.request(http.MethodGet, dev+"/API_KEY?reveal=true", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer secret reveal = %d body=%s", rec.Code, rec.Body)
	}
	if got := countRevealEvents(t, f); got != revealsBefore {
		t.Fatalf("variable reads created reveal events: %d -> %d", revealsBefore, got)
	}
	if rec := owner.request(http.MethodGet, dev+"/API_KEY?reveal=true&reason=test&channel=cli", nil); rec.Code != http.StatusOK {
		t.Fatalf("owner reveal = %d body=%s", rec.Code, rec.Body)
	}
	var cliReveals int
	if err := f.st.DB().QueryRow(`SELECT COUNT(*) FROM reveal_events WHERE channel = 'cli' AND version = 1`).Scan(&cliReveals); err != nil {
		t.Fatal(err)
	}
	if cliReveals < 1 {
		t.Fatal("cli reveal event not recorded with channel")
	}

	// Production variables need no reveal reason.
	prod := "/api/v1/orgs/acme/projects/web-app/environments/prod/secrets"
	if rec := owner.request(http.MethodPost, prod, map[string]string{
		"key": "PUBLIC_URL", "value": "https://example.com", "kind": "variable",
	}); rec.Code != http.StatusCreated {
		t.Fatalf("prod variable create = %d body=%s", rec.Code, rec.Body)
	}
	if rec := viewer.request(http.MethodGet, prod+"/PUBLIC_URL", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "example.com") {
		t.Fatalf("prod variable get = %d body=%s", rec.Code, rec.Body)
	}
}

func countRevealEvents(t *testing.T, f *fixture) int {
	t.Helper()
	var n int
	if err := f.st.DB().QueryRow(`SELECT COUNT(*) FROM reveal_events`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCloneEnvironment(t *testing.T) {
	f := newFixture(t)
	c, _ := f.setup(t)
	dev := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	if rec := c.request(http.MethodPost, dev, map[string]any{
		"key": "CLONED_KEY", "value": "cloned-value", "kind": "secret",
		"comment": "original", "tags": []string{"demo"}, "folder": "/backend",
	}); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d body=%s", rec.Code, rec.Body)
	}

	keysOnly := c.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/environments/dev/clone", map[string]any{
		"slug": "qa", "name": "QA", "include_values": false,
	})
	if keysOnly.Code != http.StatusCreated {
		t.Fatalf("clone keys = %d body=%s", keysOnly.Code, keysOnly.Body)
	}
	qa := "/api/v1/orgs/acme/projects/web-app/environments/qa/secrets"
	list := c.request(http.MethodGet, qa, nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "CLONED_KEY") {
		t.Fatalf("qa list = %d %s", list.Code, list.Body)
	}
	revealed := decode[secretResponse](t, c.request(http.MethodGet, qa+"/CLONED_KEY?reveal=true&reason=test", nil))
	if revealed.Value != "" {
		t.Fatalf("keys-only clone has a value: %q", revealed.Value)
	}

	withValues := c.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/environments/dev/clone", map[string]any{
		"slug": "qa-values", "name": "QA values", "include_values": true,
	})
	if withValues.Code != http.StatusCreated {
		t.Fatalf("clone values = %d body=%s", withValues.Code, withValues.Body)
	}
	qaValues := "/api/v1/orgs/acme/projects/web-app/environments/qa-values/secrets"
	copied := decode[secretResponse](t, c.request(http.MethodGet, qaValues+"/CLONED_KEY?reveal=true&reason=test", nil))
	if copied.Value != "cloned-value" {
		t.Fatalf("copied value = %q", copied.Value)
	}
	if !strings.Contains(c.request(http.MethodGet, qaValues, nil).Body.String(), `"folder":"/backend"`) {
		t.Fatal("folder not copied")
	}
}

func TestItemMetadataAndClone(t *testing.T) {
	f := newFixture(t)
	c, _ := f.setup(t)
	dev := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	created := c.request(http.MethodPost, dev, map[string]any{
		"key": "METADATA_KEY", "value": "value", "kind": "secret",
		"notes": "Rotate in the Stripe dashboard.", "provider_url": "https://dashboard.stripe.com/apikeys",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d body=%s", created.Code, created.Body)
	}
	got := c.request(http.MethodGet, dev+"/METADATA_KEY", nil)
	if !strings.Contains(got.Body.String(), "Rotate in the Stripe dashboard.") ||
		!strings.Contains(got.Body.String(), "dashboard.stripe.com") {
		t.Fatalf("metadata = %s", got.Body)
	}

	clone := c.request(http.MethodPost, "/api/v1/orgs/acme/projects/web-app/environments/dev/clone", map[string]any{
		"slug": "metadata", "name": "Metadata", "include_values": true,
	})
	if clone.Code != http.StatusCreated {
		t.Fatalf("clone = %d body=%s", clone.Code, clone.Body)
	}
	cloned := c.request(http.MethodGet, "/api/v1/orgs/acme/projects/web-app/environments/metadata/secrets/METADATA_KEY", nil)
	if !strings.Contains(cloned.Body.String(), "Rotate in the Stripe dashboard.") ||
		!strings.Contains(cloned.Body.String(), "dashboard.stripe.com") {
		t.Fatalf("cloned metadata = %s", cloned.Body)
	}
}

func TestOrgCanBlockCLIProductionWrites(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.setup(t)
	if rec := owner.request(http.MethodPatch, "/api/v1/orgs/acme/settings", map[string]any{"block_cli_writes": true}); rec.Code != http.StatusOK {
		t.Fatalf("settings = %d body=%s", rec.Code, rec.Body)
	}
	prod := "/api/v1/orgs/acme/projects/web-app/environments/prod/secrets"

	write := func(userAgent string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, prod,
			strings.NewReader(`{"key":"CLI_CHECK","value":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("X-CSRF-Token", owner.csrf)
		for _, cookie := range owner.cookies {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(rec, req)
		return rec
	}
	if rec := write("clavis-cli/dev"); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "cli_writes_blocked") {
		t.Fatalf("CLI write = %d body=%s", rec.Code, rec.Body)
	}
	if rec := write("Mozilla/5.0"); rec.Code != http.StatusCreated {
		t.Fatalf("browser write = %d body=%s", rec.Code, rec.Body)
	}
	dev := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	req := httptest.NewRequest(http.MethodPost, dev, strings.NewReader(`{"key":"CLI_DEV","value":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "clavis-cli/dev")
	req.Header.Set("X-CSRF-Token", owner.csrf)
	for _, cookie := range owner.cookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("CLI dev write = %d body=%s", rec.Code, rec.Body)
	}
}

func TestSignedAuditCheckpoints(t *testing.T) {
	f := newFixture(t)
	c, _ := f.setup(t)
	dev := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"
	if rec := c.request(http.MethodPost, dev, map[string]string{"key": "AUDIT", "value": "x"}); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}
	checkpoint := c.request(http.MethodPost, "/api/v1/orgs/acme/audit-events/checkpoint", nil)
	if checkpoint.Code != http.StatusCreated {
		t.Fatalf("checkpoint = %d body=%s", checkpoint.Code, checkpoint.Body)
	}
	var body struct {
		Seq       int64  `json:"seq"`
		PublicKey string `json:"public_key"`
	}
	body = decode[struct {
		Seq       int64  `json:"seq"`
		PublicKey string `json:"public_key"`
	}](t, checkpoint)
	if body.Seq == 0 || body.PublicKey == "" {
		t.Fatalf("checkpoint = %+v", body)
	}

	verify := c.request(http.MethodGet, "/api/v1/orgs/acme/audit-events/verify", nil)
	if verify.Code != http.StatusOK || !strings.Contains(verify.Body.String(), `"chain_valid":true`) {
		t.Fatalf("verify = %d %s", verify.Code, verify.Body)
	}
	list := c.request(http.MethodGet, "/api/v1/orgs/acme/audit-events/checkpoints", nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"seq":`) {
		t.Fatalf("list = %d %s", list.Code, list.Body)
	}

	if _, err := f.st.DB().Exec(`UPDATE audit_events SET action = 'tampered' WHERE seq = 1`); err != nil {
		t.Fatal(err)
	}
	broken := c.request(http.MethodGet, "/api/v1/orgs/acme/audit-events/verify", nil)
	if broken.Code != http.StatusOK || !strings.Contains(broken.Body.String(), `"chain_valid":false`) {
		t.Fatalf("broken verify = %d %s", broken.Code, broken.Body)
	}
}

func TestPersonalAccessTokens(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.setup(t)
	dev := "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets"

	created := owner.request(http.MethodPost, "/api/v1/orgs/acme/tokens", map[string]any{
		"permissions": []string{"secrets:read-names"}, "expires_in_days": 30,
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create token = %d body=%s", created.Code, created.Body)
	}
	var result struct {
		Token     string `json:"token"`
		TokenInfo struct {
			ID     string `json:"id"`
			Prefix string `json:"prefix"`
		} `json:"token_info"`
	}
	result = decode[struct {
		Token     string `json:"token"`
		TokenInfo struct {
			ID     string `json:"id"`
			Prefix string `json:"prefix"`
		} `json:"token_info"`
	}](t, created)
	if !strings.HasPrefix(result.Token, "clv_pat_") || result.TokenInfo.ID == "" {
		t.Fatalf("token = %+v", result)
	}

	bearer := func(method, path string) *httptest.ResponseRecorder {
		var body *strings.Reader
		if method == http.MethodPost {
			body = strings.NewReader(`{"key":"PAT","value":"x"}`)
		} else {
			body = strings.NewReader("")
		}
		req := httptest.NewRequest(method, path, body)
		req.Header.Set("Authorization", "Bearer "+result.Token)
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(rec, req)
		return rec
	}
	if rec := bearer(http.MethodGet, dev); rec.Code != http.StatusOK {
		t.Fatalf("read with PAT = %d body=%s", rec.Code, rec.Body)
	}
	if rec := bearer(http.MethodPost, dev); rec.Code != http.StatusForbidden {
		t.Fatalf("write with read-only PAT = %d body=%s", rec.Code, rec.Body)
	}

	list := owner.request(http.MethodGet, "/api/v1/orgs/acme/tokens", nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), result.TokenInfo.Prefix) {
		t.Fatalf("list tokens = %d %s", list.Code, list.Body)
	}

	// Expiry is enforced.
	if _, err := f.st.DB().Exec(`UPDATE access_tokens SET expires_at = '2020-01-01T00:00:00.000000000Z' WHERE id = ?`, result.TokenInfo.ID); err != nil {
		t.Fatal(err)
	}
	if rec := bearer(http.MethodGet, dev); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expired PAT = %d", rec.Code)
	}

	// Revocation is enforced.
	second := owner.request(http.MethodPost, "/api/v1/orgs/acme/tokens", map[string]any{
		"permissions": []string{"secrets:read-names"},
	})
	var secondResult struct {
		Token     string `json:"token"`
		TokenInfo struct {
			ID string `json:"id"`
		} `json:"token_info"`
	}
	secondResult = decode[struct {
		Token     string `json:"token"`
		TokenInfo struct {
			ID string `json:"id"`
		} `json:"token_info"`
	}](t, second)
	revoke := owner.request(http.MethodDelete, "/api/v1/orgs/acme/tokens/"+secondResult.TokenInfo.ID, nil)
	if revoke.Code != http.StatusNoContent {
		t.Fatalf("revoke = %d body=%s", revoke.Code, revoke.Body)
	}
	req := httptest.NewRequest(http.MethodGet, dev, nil)
	req.Header.Set("Authorization", "Bearer "+secondResult.Token)
	rec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked PAT = %d", rec.Code)
	}
}

func TestPersonalTokenCannotEscalate(t *testing.T) {
	f := newFixture(t)
	owner, _ := f.setup(t)
	invite := owner.request(http.MethodPost, "/api/v1/orgs/acme/invites", map[string]any{"role": "Viewer"})
	var inv struct {
		URL string `json:"url"`
	}
	inv = decode[struct {
		URL string `json:"url"`
	}](t, invite)
	token := inv.URL[strings.Index(inv.URL, "token=")+len("token="):]
	viewer := &apiClient{t: t, h: f.srv.Handler()}
	if rec := viewer.request(http.MethodPost, "/api/v1/invites/"+token+"/accept", map[string]string{
		"name": "Viewer", "email": "viewer@example.com", "password": "another long password",
	}); rec.Code != http.StatusOK {
		t.Fatalf("accept = %d", rec.Code)
	}
	escalation := viewer.request(http.MethodPost, "/api/v1/orgs/acme/tokens", map[string]any{
		"permissions": []string{"settings:manage"},
	})
	if escalation.Code != http.StatusForbidden {
		t.Fatalf("escalation = %d body=%s", escalation.Code, escalation.Body)
	}
}

func TestPerIdentityRateLimit(t *testing.T) {
	st := storetest.Open(t)
	key, _ := crypto.GenerateKey()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Server.BaseURL = "http://localhost:8080"
	cfg.Server.Dev = true
	cfg.Server.RateLimitPerMinute = 3
	cfg.Database.Path = filepath.Join(dir, "clavis.db")
	srv, err := server.New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), key, server.WithWebFS(testWebFS()))
	if err != nil {
		t.Fatal(err)
	}
	tokenBytes, _ := os.ReadFile(filepath.Join(dir, "setup-token"))
	c := &apiClient{t: t, h: srv.Handler()}
	if rec := c.request(http.MethodPost, "/api/v1/setup", map[string]string{
		"token": strings.TrimSpace(string(tokenBytes)), "email": "owner@example.com", "name": "Owner",
		"password": "correct horse battery staple", "org_name": "Acme",
	}); rec.Code != http.StatusOK {
		t.Fatalf("setup = %d body=%s", rec.Code, rec.Body)
	}
	for i := 0; i < 3; i++ {
		rec := c.request(http.MethodGet, "/api/v1/auth/me", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d = %d", i+1, rec.Code)
		}
		if rec.Header().Get("RateLimit-Limit") != "3" || rec.Header().Get("RateLimit-Remaining") == "" {
			t.Fatalf("rate headers = %v", rec.Header())
		}
	}
	limited := c.request(http.MethodGet, "/api/v1/auth/me", nil)
	if limited.Code != http.StatusTooManyRequests || !strings.Contains(limited.Body.String(), "rate_limited") {
		t.Fatalf("limited = %d body=%s", limited.Code, limited.Body)
	}
}

func TestPasswordPolicy(t *testing.T) {
	f := newFixture(t)
	tokenBytes, _ := os.ReadFile(filepath.Join(f.dir, "setup-token"))
	c := &apiClient{t: t, h: f.srv.Handler()}
	rec := c.request(http.MethodPost, "/api/v1/setup", map[string]string{
		"token": strings.TrimSpace(string(tokenBytes)), "email": "owner@example.com", "name": "Owner",
		"password": "short", "org_name": "Acme",
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("weak password status = %d body=%s", rec.Code, rec.Body)
	}
}

var _ = authn.MinPasswordLength

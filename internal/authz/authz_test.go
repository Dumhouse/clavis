package authz

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/dumhouse/clavis/pkg/problem"
)

func now() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

func actorWith(bindings ...Binding) Actor {
	return Actor{ID: "user-1", Kind: SubjectUser, OrgID: "org-1", Bindings: bindings}
}

func TestAuthorizeDenyByDefault(t *testing.T) {
	e := New()
	if d := e.Authorize(Actor{}, SecretsReadNames, Scope{OrgID: "org-1"}, now()); d.Allowed {
		t.Fatal("anonymous actor allowed")
	}
	if d := e.Authorize(actorWith(), SecretsReadNames, Scope{OrgID: "org-1"}, now()); d.Allowed {
		t.Fatal("actor without bindings allowed")
	}
}

func TestAuthorizeBuiltinRoles(t *testing.T) {
	e := New()
	scope := Scope{OrgID: "org-1", ProjectID: "proj-1", Environment: "dev", Path: "/backend"}

	cases := []struct {
		name string
		role string
		perm Permission
		want bool
	}{
		{"owner deletes org", RoleOwner, OrgDelete, true},
		{"admin cannot delete org", RoleAdmin, OrgDelete, false},
		{"admin writes secrets", RoleAdmin, SecretsWrite, true},
		{"developer reads values", RoleDeveloper, SecretsReadValues, true},
		{"developer manages members", RoleDeveloper, MembersManage, false},
		{"viewer reads names", RoleViewer, SecretsReadNames, true},
		{"viewer cannot read values", RoleViewer, SecretsReadValues, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actor := actorWith(Binding{Role: tc.role, Scope: Scope{OrgID: "org-1"}})
			got := e.Authorize(actor, tc.perm, scope, now()).Allowed
			if got != tc.want {
				t.Fatalf("allowed = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAuthorizeScope(t *testing.T) {
	e := New()
	reqScope := Scope{OrgID: "org-1", ProjectID: "proj-1", Environment: "production", Path: "/backend/db"}

	bindings := []Binding{
		{Role: RoleDeveloper, Scope: Scope{OrgID: "org-1", ProjectID: "other"}},
	}
	if e.Authorize(actorWith(bindings...), SecretsReadNames, reqScope, now()).Allowed {
		t.Fatal("project scope not enforced")
	}

	bindings = []Binding{
		{Role: RoleDeveloper, Scope: Scope{OrgID: "org-1", Environment: "dev*"}},
	}
	if e.Authorize(actorWith(bindings...), SecretsReadNames, reqScope, now()).Allowed {
		t.Fatal("environment glob not enforced")
	}

	bindings = []Binding{
		{Role: RoleDeveloper, Scope: Scope{OrgID: "org-1", Path: "/backend/**"}},
	}
	if !e.Authorize(actorWith(bindings...), SecretsReadNames, reqScope, now()).Allowed {
		t.Fatal("path glob should match")
	}

	bindings = []Binding{
		{Role: RoleDeveloper, Scope: Scope{OrgID: "org-1", Path: "/*"}},
	}
	if e.Authorize(actorWith(bindings...), SecretsReadNames, reqScope, now()).Allowed {
		t.Fatal("* must not cross / boundaries")
	}
}

func TestAuthorizeExpires(t *testing.T) {
	e := New()
	expiry := now().Add(-time.Minute)
	actor := actorWith(Binding{Role: RoleDeveloper, Scope: Scope{OrgID: "org-1"}, ExpiresAt: expiry})
	if e.Authorize(actor, SecretsReadNames, Scope{OrgID: "org-1"}, now()).Allowed {
		t.Fatal("expired binding honoured")
	}
	actor.Bindings[0].ExpiresAt = now().Add(time.Minute)
	if !e.Authorize(actor, SecretsReadNames, Scope{OrgID: "org-1"}, now()).Allowed {
		t.Fatal("unexpired binding rejected")
	}
}

func TestAuthorizeCustomPermissionsAndOrgMismatch(t *testing.T) {
	e := New()
	actor := actorWith(Binding{Permissions: []Permission{SecretsUse}, Scope: Scope{OrgID: "org-1"}})
	if !e.Authorize(actor, SecretsUse, Scope{OrgID: "org-1"}, now()).Allowed {
		t.Fatal("custom permission rejected")
	}
	if e.Authorize(actor, SecretsReadValues, Scope{OrgID: "org-1"}, now()).Allowed {
		t.Fatal("unheld permission allowed")
	}
	if e.Authorize(actor, SecretsUse, Scope{OrgID: "org-2"}, now()).Allowed {
		t.Fatal("org scope not enforced")
	}
	if e.Authorize(actor, Permission("bogus"), Scope{OrgID: "org-1"}, now()).Allowed {
		t.Fatal("unknown permission allowed")
	}
	if e.Authorize(actor, SecretsUse, Scope{}, now()).Allowed {
		t.Fatal("missing org scope allowed")
	}
}

func TestCanGrantNoEscalation(t *testing.T) {
	e := New()
	developer := actorWith(Binding{Role: RoleDeveloper, Scope: Scope{OrgID: "org-1"}})
	if !e.CanGrant(developer, SecretsWrite, Scope{OrgID: "org-1"}, now()) {
		t.Fatal("developer should grant a held permission")
	}
	if e.CanGrant(developer, OrgDelete, Scope{OrgID: "org-1"}, now()) {
		t.Fatal("privilege escalation allowed")
	}
}

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pattern, value string
		want           bool
	}{
		{"", "anything", true},
		{"prod", "prod", true},
		{"prod", "production", false},
		{"prod*", "production", true},
		{"*", "production", true},
		{"*", "a/b", false},
		{"**", "a/b", true},
		{"/backend/*", "/backend/db", true},
		{"/backend/*", "/backend/db/nested", false},
		{"/backend/**", "/backend/db/nested", true},
		{"/?ackend", "/backend", true},
		{"/back?nd", "/backend", true},
		{"é*", "état", true},
		{"[abc]", "[abc]", true},
	}
	for _, tc := range cases {
		if got := MatchGlob(tc.pattern, tc.value); got != tc.want {
			t.Errorf("MatchGlob(%q, %q) = %v, want %v", tc.pattern, tc.value, got, tc.want)
		}
	}
}

func TestNormalizePath(t *testing.T) {
	cases := map[string]string{"": "/", "/": "/", "backend": "/backend", "/backend/": "/backend", "/a/../b": "/b"}
	for in, want := range cases {
		if got := NormalizePath(in); got != want {
			t.Errorf("NormalizePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	if err := r.Declare("GET", "/api/v1/secret", SecretsReadNames, "list"); err != nil {
		t.Fatal(err)
	}
	if err := r.Declare("GET", "/api/v1/secret", SecretsReadNames, "dup"); err == nil {
		t.Fatal("duplicate route accepted")
	}
	if err := r.Declare("GET", "/api/v1/bad", Permission("nope"), ""); err == nil {
		t.Fatal("unknown permission accepted")
	}
	if err := r.DeclarePublic("GET", "/healthz", ""); err == nil {
		t.Fatal("public route without reason accepted")
	}
	if err := r.DeclarePublic("GET", "/healthz", "liveness probe"); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Lookup("GET", "/api/v1/secret"); !ok {
		t.Fatal("lookup failed")
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(r.Routes()) != 2 {
		t.Fatalf("routes = %d", len(r.Routes()))
	}
}

func TestMiddleware(t *testing.T) {
	reg := NewRegistry()
	_ = reg.DeclarePublic("GET", "/public", "test")
	_ = reg.Declare("GET", "/orgs/{org}/secrets", SecretsReadNames, "test")

	resolve := func(r *http.Request) (Actor, bool) {
		switch r.Header.Get("X-Test-Actor") {
		case "member":
			return actorWith(Binding{Role: RoleViewer, Scope: Scope{OrgID: "org-1"}}), true
		case "stranger":
			return actorWith(Binding{Role: RoleViewer, Scope: Scope{OrgID: "org-2"}}), true
		}
		return Actor{}, false
	}

	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(reg.Middleware(New(), resolve, nil))
	router.Get("/public", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	router.Get("/orgs/{org}/secrets", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	router.Get("/undeclared/api", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	cases := []struct {
		name   string
		target string
		actor  string
		want   int
	}{
		{"public", "/public", "", http.StatusOK},
		{"unauthenticated", "/orgs/org-1/secrets", "", http.StatusUnauthorized},
		{"authorized", "/orgs/org-1/secrets", "member", http.StatusOK},
		{"wrong org", "/orgs/org-1/secrets", "stranger", http.StatusForbidden},
		{"undeclared", "/undeclared/api", "member", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.target, nil)
			if tc.actor != "" {
				req.Header.Set("X-Test-Actor", tc.actor)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body)
			}
			if rec.Code != http.StatusOK {
				var p problem.Problem
				if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
					t.Fatalf("non-problem response: %v", err)
				}
				if p.Code == "" {
					t.Fatal("problem has no code")
				}
				if ct := rec.Header().Get("Content-Type"); ct != problem.ContentType {
					t.Fatalf("content type = %q", ct)
				}
			}
		})
	}
}

func TestActorContext(t *testing.T) {
	actor := actorWith()
	ctx := WithActor(context.Background(), actor)
	got, ok := ActorFromContext(ctx)
	if !ok || got.ID != actor.ID {
		t.Fatal("actor round trip failed")
	}
	if _, ok := ActorFromContext(context.Background()); ok {
		t.Fatal("unexpected actor")
	}
}

func TestIsAPIPath(t *testing.T) {
	if !IsAPIPath("/api/v1/x") || IsAPIPath("/healthz") {
		t.Fatal("IsAPIPath misclassified")
	}
}

func TestAllPermissions(t *testing.T) {
	perms := AllPermissions()
	if len(perms) == 0 {
		t.Fatal("empty catalog")
	}
	perms[0] = "mutated"
	if AllPermissions()[0] == "mutated" {
		t.Fatal("catalog mutable")
	}
	if _, ok := BuiltinRolePermissions("Nobody"); ok {
		t.Fatal("unknown role reported as built-in")
	}
}

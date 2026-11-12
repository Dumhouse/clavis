// Package authz implements Clavis' deny-by-default authorization engine.
package authz

import "time"

// Permission is a capability an actor can hold.
type Permission string

// Permission catalog. API handlers declare exactly one required permission.
const (
	SecretsReadNames   Permission = "secrets:read-names"
	SecretsReadValues  Permission = "secrets:read-values"
	SecretsUse         Permission = "secrets:use"
	SecretsWrite       Permission = "secrets:write"
	SecretsDelete      Permission = "secrets:delete"
	EnvironmentsRead   Permission = "envs:read"
	EnvironmentsManage Permission = "envs:manage"
	OrgRead            Permission = "org:read"
	ProjectsRead       Permission = "projects:read"
	ProjectsManage     Permission = "projects:manage"
	MembersManage      Permission = "members:manage"
	RolesManage        Permission = "roles:manage"
	SyncsManage        Permission = "syncs:manage"
	AuditRead          Permission = "audit:read"
	SettingsManage     Permission = "settings:manage"
	TokensManage       Permission = "tokens:manage"
	OrgDelete          Permission = "org:delete"
)

// allPermissions is the canonical catalog. Keep sorted for stable output.
var allPermissions = []Permission{
	SecretsReadNames,
	SecretsReadValues,
	SecretsUse,
	SecretsWrite,
	SecretsDelete,
	EnvironmentsRead,
	EnvironmentsManage,
	OrgRead,
	ProjectsRead,
	ProjectsManage,
	MembersManage,
	RolesManage,
	SyncsManage,
	AuditRead,
	SettingsManage,
	TokensManage,
	OrgDelete,
}

// AllPermissions returns a copy of the permission catalog.
func AllPermissions() []Permission {
	out := make([]Permission, len(allPermissions))
	copy(out, allPermissions)
	return out
}

// Valid reports whether p is in the catalog.
func (p Permission) Valid() bool {
	for _, candidate := range allPermissions {
		if p == candidate {
			return true
		}
	}
	return false
}

// Built-in role names.
const (
	RoleOwner     = "Owner"
	RoleAdmin     = "Admin"
	RoleDeveloper = "Developer"
	RoleViewer    = "Viewer"
)

var builtinRoles = map[string][]Permission{
	RoleOwner: allPermissions,
	RoleAdmin: {
		SecretsReadNames, SecretsReadValues, SecretsUse, SecretsWrite, SecretsDelete,
		EnvironmentsRead, EnvironmentsManage, OrgRead, ProjectsRead, ProjectsManage,
		MembersManage, RolesManage, SyncsManage, AuditRead, SettingsManage, TokensManage,
	},
	RoleDeveloper: {
		SecretsReadNames, SecretsReadValues, SecretsUse, SecretsWrite, SecretsDelete,
		EnvironmentsRead, OrgRead, ProjectsRead, SyncsManage, AuditRead,
	},
	RoleViewer: {
		SecretsReadNames, EnvironmentsRead, OrgRead, ProjectsRead,
	},
}

// BuiltinRolePermissions returns the permissions granted by a built-in role.
func BuiltinRolePermissions(role string) ([]Permission, bool) {
	perms, ok := builtinRoles[role]
	return append([]Permission(nil), perms...), ok
}

// SubjectKind distinguishes humans from machines and agents.
type SubjectKind string

// Subject kinds.
const (
	SubjectUser    SubjectKind = "user"
	SubjectMachine SubjectKind = "machine"
	SubjectAgent   SubjectKind = "agent"
	SubjectGroup   SubjectKind = "group"
)

// Binding grants a role (or explicit permissions) within a scope. A zero
// ExpiresAt means the grant does not expire (temporary JIT grants set it).
type Binding struct {
	ID          string
	Role        string
	Permissions []Permission
	Scope       Scope
	ExpiresAt   time.Time
}

// Actor is the authenticated subject making a request.
type Actor struct {
	ID       string
	Kind     SubjectKind
	Name     string
	OrgID    string
	Bindings []Binding
}

// Scope narrows a grant. Empty fields mean "all within the parent".
type Scope struct {
	OrgID       string
	ProjectID   string
	Environment string // glob, e.g. "prod", "dev*"
	Path        string // glob, e.g. "/", "/backend/**"
}

// Decision explains an authorization result so the UI can render
// "why can Alex read prod?" explainers.
type Decision struct {
	Allowed bool
	Reason  string
	Binding *Binding
}

// Denied is a convenience constructor.
func denied(reason string) Decision { return Decision{Reason: reason} }

// Engine evaluates permissions. It is stateless and safe for concurrent use.
type Engine struct{}

// New returns an authorization engine.
func New() *Engine { return &Engine{} }

// Authorize evaluates whether actor may perform perm in scope at time now.
// It is deny-by-default: no matching unexpired binding means no access.
func (e *Engine) Authorize(actor Actor, perm Permission, scope Scope, now time.Time) Decision {
	if !perm.Valid() {
		return denied("unknown permission")
	}
	if actor.ID == "" {
		return denied("unauthenticated")
	}
	if scope.OrgID == "" {
		return denied("missing organization scope")
	}
	if actor.OrgID != "" && actor.OrgID != scope.OrgID {
		return denied("actor does not belong to organization")
	}

	for i := range actor.Bindings {
		b := &actor.Bindings[i]
		if !b.ExpiresAt.IsZero() && !now.Before(b.ExpiresAt) {
			continue
		}
		if !b.Scope.matches(scope) {
			continue
		}
		if bindingGrants(*b, perm) {
			return Decision{Allowed: true, Reason: "granted by " + describeBinding(b), Binding: b}
		}
	}
	return denied("no binding grants " + string(perm))
}

// CanGrant reports whether granter may grant perm in scope. A caller can never
// grant a permission they do not hold themselves.
func (e *Engine) CanGrant(granter Actor, perm Permission, scope Scope, now time.Time) bool {
	return e.Authorize(granter, perm, scope, now).Allowed
}

func bindingGrants(b Binding, perm Permission) bool {
	if b.Role != "" {
		if perms, ok := builtinRoles[b.Role]; ok {
			for _, p := range perms {
				if p == perm {
					return true
				}
			}
		}
	}
	for _, p := range b.Permissions {
		if p == perm {
			return true
		}
	}
	return false
}

func describeBinding(b *Binding) string {
	if b.Role != "" {
		return "role " + b.Role
	}
	return "custom grant " + b.ID
}

func (s Scope) matches(req Scope) bool {
	if s.OrgID != "" && s.OrgID != req.OrgID {
		return false
	}
	if s.ProjectID != "" && s.ProjectID != req.ProjectID {
		return false
	}
	if s.Environment != "" && !MatchGlob(s.Environment, req.Environment) {
		return false
	}
	reqPath := req.Path
	if reqPath == "" {
		reqPath = "/"
	}
	if s.Path != "" && !MatchGlob(s.Path, reqPath) {
		return false
	}
	return true
}

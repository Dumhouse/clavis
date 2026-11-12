// Package service contains Clavis' business logic. Handlers authorize and
// validate input, then call these methods; the methods own encryption, audit
// events, and multi-row consistency.
package service

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authn"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/events"
	"github.com/dumhouse/clavis/internal/store"
)

var (
	// ErrValidation is returned for input that passed JSON validation but is
	// semantically invalid.
	ErrValidation = errors.New("service: validation failed")
	// ErrForbidden is returned when the actor lacks a permission.
	ErrForbidden = errors.New("service: forbidden")
	// ErrNotFound is returned when a resource is absent or not visible.
	ErrNotFound = errors.New("service: not found")
	// ErrConflict is returned for duplicate slugs, keys, or emails.
	ErrConflict = errors.New("service: conflict")
	// ErrProtected is returned when a non-admin writes to a protected env.
	ErrProtected = errors.New("service: protected environment")
	// ErrCLIBlocked is returned when an org blocks CLI writes to production.
	ErrCLIBlocked = errors.New("service: CLI writes to production are blocked")
	// ErrBreachedPassword is returned when the optional breach check matches.
	ErrBreachedPassword = errors.New("service: password appears in a public breach corpus")
	// ErrReauth is returned when a sensitive action lacks recent confirmation.
	ErrReauth = errors.New("service: re-authentication required")
)

var slugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,58}[a-z0-9])?$`)

// Service owns domain operations.
type Service struct {
	Store      *store.Store
	Keyring    *crypto.Keyring
	Engine     *authz.Engine
	Logger     *slog.Logger
	Bus        *events.Bus
	SigningKey ed25519.PrivateKey
	Now        func() time.Time
	// BreachCheck is optional. When set, passwords are checked with k-anonymity.
	BreachCheck func(ctx context.Context, password string) (bool, error)
	// SSOClient is used for OIDC discovery, token exchange, and JWKS fetches.
	SSOClient      *http.Client
	AWSSTSEndpoint string
	// AuditSink asynchronously delivers audit events to external systems.
	AuditSink     AuditSink
	WebhookClient *http.Client
	webhookQueue  chan *audit.Event
	ssoStates     *ssoStateStore
	samlTickets   *samlTicketStore
}

// AuditSink receives sealed audit events for external delivery.
type AuditSink interface {
	Publish(event *audit.Event)
}

// New returns a domain service.
func New(st *store.Store, keyring *crypto.Keyring, logger *slog.Logger, bus *events.Bus) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if bus == nil {
		bus = events.NewBus()
	}
	svc := &Service{
		Store:        st,
		Keyring:      keyring,
		Engine:       authz.New(),
		Logger:       logger,
		Bus:          bus,
		Now:          time.Now,
		ssoStates:    newSSOStateStore(time.Now),
		samlTickets:  newSAMLTicketStore(time.Now),
		webhookQueue: make(chan *audit.Event, 256),
	}
	go svc.runWebhookWorker()
	return svc
}

// VerifyActorPassword checks the authenticated user's password for a sensitive
// action. It does not start a new session or change any session state.
func (s *Service) VerifyActorPassword(ctx context.Context, actor authz.Actor, password string) error {
	if password == "" || len(password) > 1024 {
		return ErrReauth
	}
	user, err := s.Store.GetUser(ctx, actor.ID)
	if err != nil {
		return ErrReauth
	}
	ok, err := authn.VerifyPassword(user.PasswordHash, password)
	if err != nil || !ok {
		return ErrReauth
	}
	return nil
}

// CheckPassword applies the base policy and the optional breach check.
func (s *Service) CheckPassword(ctx context.Context, password string) error {
	if err := authn.ValidatePassword(password); err != nil {
		return err
	}
	if s.BreachCheck == nil {
		return nil
	}
	breached, err := s.BreachCheck(ctx, password)
	if err != nil {
		// The check is opt-in hardening; a provider outage must not lock users
		// out of password resets or setup.
		s.Logger.Warn("breach check failed; allowing password", "error", err)
		return nil
	}
	if breached {
		return ErrBreachedPassword
	}
	return nil
}

func (s *Service) publish(orgID, eventType string, project *store.Project, env *store.Environment) {
	if s.Bus == nil {
		return
	}
	event := events.Event{Type: eventType, At: s.Now().UTC()}
	if project != nil {
		event.Project = project.Slug
	}
	if env != nil {
		event.Environment = env.Slug
	}
	s.Bus.Publish(orgID, event)
}

// AuditMeta is request context copied into audit events. It never contains
// secret material.
type AuditMeta struct {
	IP        string
	UserAgent string
	RequestID string
}

// NormalizeSlug validates and lowercases a slug.
func NormalizeSlug(raw string) (string, error) {
	slug := strings.ToLower(strings.TrimSpace(raw))
	if !slugPattern.MatchString(slug) {
		return "", fmt.Errorf("%w: slug must be 1-60 lowercase letters, digits, or hyphens and cannot start or end with a hyphen", ErrValidation)
	}
	return slug, nil
}

// ActorForUser builds an authorization actor from memberships and scoped role
// bindings. Each binding carries its org, so Actor.OrgID stays empty and the
// engine matches per request scope.
func (s *Service) ActorForUser(ctx context.Context, user *store.User) (authz.Actor, error) {
	memberships, err := s.Store.ListOrgsForUser(ctx, user.ID)
	if err != nil {
		return authz.Actor{}, err
	}
	actor := authz.Actor{
		ID:    user.ID,
		Kind:  authz.SubjectUser,
		Name:  user.Name,
		OrgID: "",
	}
	for _, m := range memberships {
		if m.RoleName == "" {
			continue
		}
		actor.Bindings = append(actor.Bindings, authz.Binding{
			Role:        m.RoleName,
			Permissions: parsePermissions(m.RolePermissions),
			Scope:       authz.Scope{OrgID: m.Org.ID},
		})
	}
	scoped, err := s.Store.ListUserBindings(ctx, user.ID)
	if err != nil {
		return authz.Actor{}, err
	}
	for _, b := range scoped {
		binding := authz.Binding{
			Role:        b.RoleName,
			Permissions: parsePermissions(b.RolePermissions),
			Scope:       authz.Scope{OrgID: b.OrgID, ProjectID: b.ProjectID, Environment: b.EnvironmentGlob, Path: b.PathGlob},
		}
		if b.ExpiresAt != nil {
			binding.ExpiresAt = *b.ExpiresAt
		}
		actor.Bindings = append(actor.Bindings, binding)
	}
	return actor, nil
}

// Authorize checks a permission in a scope.
func (s *Service) Authorize(actor authz.Actor, perm authz.Permission, scope authz.Scope) error {
	if !s.Engine.Authorize(actor, perm, scope, s.Now()).Allowed {
		return ErrForbidden
	}
	return nil
}

// CreateOrg creates an organization, its built-in roles, and the owner
// membership. Callers must have completed setup or be authenticated.
func (s *Service) CreateOrg(ctx context.Context, user *store.User, name, rawSlug string) (*store.Org, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return nil, fmt.Errorf("%w: organization name is required and must be under 100 characters", ErrValidation)
	}
	slug, err := NormalizeSlug(rawSlug)
	if err != nil {
		return nil, err
	}

	orgID := uuid.NewString()
	wrapped, _, err := s.Keyring.NewOrgKey(orgID)
	if err != nil {
		return nil, err
	}
	org := &store.Org{
		ID:            orgID,
		Slug:          slug,
		Name:          name,
		WrappedOrgKey: wrapped,
	}
	roles, ownerRoleID := builtinRoles(orgID)
	if err := s.Store.CreateOrgWithOwner(ctx, org, roles, user.ID, ownerRoleID); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrConflict
		}
		return nil, err
	}
	if err := s.record(ctx, audit.Actor{ID: user.ID, Kind: string(authz.SubjectUser), Name: user.Name},
		org.ID, "org.create", audit.Resource{Type: "org", ID: org.ID, Name: org.Slug}, audit.Details{}, AuditMeta{}); err != nil {
		s.Logger.Error("audit org.create failed", "org_id", org.ID, "error", err)
	}
	return org, nil
}

func builtinRoles(orgID string) ([]*store.Role, string) {
	names := []string{authz.RoleOwner, authz.RoleAdmin, authz.RoleDeveloper, authz.RoleViewer}
	roles := make([]*store.Role, 0, len(names))
	var ownerID string
	for _, name := range names {
		role := &store.Role{
			ID:          uuid.NewString(),
			OrgID:       orgID,
			Name:        name,
			Permissions: "[]",
			Builtin:     true,
		}
		if name == authz.RoleOwner {
			ownerID = role.ID
		}
		roles = append(roles, role)
	}
	return roles, ownerID
}

// ListOrgs returns the orgs a user belongs to.
func (s *Service) ListOrgs(ctx context.Context, userID string) ([]store.OrgMembership, error) {
	return s.Store.ListOrgsForUser(ctx, userID)
}

// GetOrg loads one org and the caller's role name.
func (s *Service) GetOrg(ctx context.Context, actor authz.Actor, slug string) (*store.Org, string, error) {
	org, err := s.Store.GetOrgBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, "", ErrNotFound
		}
		return nil, "", err
	}
	if err := s.Authorize(actor, authz.OrgRead, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, "", err
	}
	role := ""
	if m, err := s.Store.GetMembership(ctx, org.ID, actor.ID); err == nil {
		role = m.RoleName
	}
	return org, role, nil
}

// EnvironmentPatch holds optional environment changes.
type EnvironmentPatch struct {
	Name       *string
	Type       *string
	Protected  *bool
	Position   *int
	ParentSlug *string
}

// UpdateEnvironment applies a patch.
func (s *Service) UpdateEnvironment(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug string, patch EnvironmentPatch) (*store.Environment, error) {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.EnvironmentsManage)
	if err != nil {
		return nil, err
	}
	env, err := s.Store.GetEnvironmentBySlug(ctx, project.ID, envSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if patch.Name != nil {
		name := strings.TrimSpace(*patch.Name)
		if name == "" || len(name) > 100 {
			return nil, fmt.Errorf("%w: environment name is required", ErrValidation)
		}
		env.Name = name
	}
	if patch.Type != nil {
		if !validEnvType(*patch.Type) {
			return nil, fmt.Errorf("%w: environment type must be development, preview, staging, or production", ErrValidation)
		}
		env.Type = *patch.Type
		env.Color = envColor(*patch.Type)
		if patch.Protected == nil {
			env.IsProtected = *patch.Type == "production"
		}
	}
	if patch.Protected != nil {
		env.IsProtected = *patch.Protected
	}
	if patch.Position != nil {
		env.Position = *patch.Position
	}
	if patch.ParentSlug != nil {
		parentSlug := strings.TrimSpace(*patch.ParentSlug)
		if parentSlug == "" {
			env.ParentID = nil
		} else {
			parent, err := s.Store.GetEnvironmentBySlug(ctx, project.ID, parentSlug)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return nil, fmt.Errorf("%w: parent environment does not exist", ErrValidation)
				}
				return nil, err
			}
			if parent.ID == env.ID {
				return nil, fmt.Errorf("%w: an environment cannot inherit from itself", ErrValidation)
			}
			cursor := parent
			for cursor.ParentID != nil && *cursor.ParentID != "" {
				if *cursor.ParentID == env.ID {
					return nil, fmt.Errorf("%w: environment inheritance cycle", ErrValidation)
				}
				next, err := s.Store.GetEnvironmentByID(ctx, *cursor.ParentID)
				if err != nil {
					break
				}
				cursor = next
			}
			env.ParentID = &parent.ID
		}
	}
	if err := s.Store.UpdateEnvironment(ctx, env); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrConflict
		}
		return nil, err
	}
	s.publish(org.ID, "environments.changed", project, env)
	if err := s.record(ctx, toAuditActor(actor), org.ID, "env.update",
		audit.Resource{Type: "environment", ID: env.ID, Name: env.Slug, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{Environment: env.Slug}, AuditMeta{}); err != nil {
		s.Logger.Error("audit env.update failed", "env_id", env.ID, "error", err)
	}
	return env, nil
}

// DeleteEnvironment removes an environment, refusing to remove the last one.
func (s *Service) DeleteEnvironment(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug string) error {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.EnvironmentsManage)
	if err != nil {
		return err
	}
	env, err := s.Store.GetEnvironmentBySlug(ctx, project.ID, envSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	envs, err := s.Store.ListEnvironments(ctx, project.ID)
	if err != nil {
		return err
	}
	if len(envs) <= 1 {
		return fmt.Errorf("%w: a project must keep at least one environment", ErrValidation)
	}
	if err := s.Store.DeleteEnvironment(ctx, env.ID); err != nil {
		return err
	}
	s.publish(org.ID, "environments.changed", project, env)
	if err := s.record(ctx, toAuditActor(actor), org.ID, "env.delete",
		audit.Resource{Type: "environment", ID: env.ID, Name: env.Slug, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{Environment: env.Slug}, AuditMeta{}); err != nil {
		s.Logger.Error("audit env.delete failed", "env_id", env.ID, "error", err)
	}
	return nil
}

// CreateProject creates a project with default development, staging, and
// production environments.
func (s *Service) CreateProject(ctx context.Context, actor authz.Actor, orgSlug, name, rawSlug string) (*store.Project, []store.Environment, error) {
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	if err := s.Authorize(actor, authz.ProjectsManage, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return nil, nil, fmt.Errorf("%w: project name is required and must be under 100 characters", ErrValidation)
	}
	slug, err := NormalizeSlug(rawSlug)
	if err != nil {
		return nil, nil, err
	}

	orgKey, err := s.Keyring.UnwrapOrgKey(org.ID, org.WrappedOrgKey)
	if err != nil {
		return nil, nil, fmt.Errorf("service: unwrap org key: %w", err)
	}
	defer orgKey.Zero()
	projectKey, err := crypto.GenerateKey()
	if err != nil {
		return nil, nil, err
	}
	defer projectKey.Zero()
	projectID := uuid.NewString()
	wrapped, err := crypto.WrapProjectKey(orgKey, org.ID, projectID, projectKey)
	if err != nil {
		return nil, nil, err
	}

	project := &store.Project{ID: projectID, OrgID: org.ID, Slug: slug, Name: name, WrappedProjectKey: wrapped}
	envs := environmentsForProject(projectID, ParseOrgSettings(org.Settings).EnvironmentTemplates)
	if err := s.Store.CreateProjectWithEnvironments(ctx, project, ptrEnvs(envs)); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, nil, ErrConflict
		}
		return nil, nil, err
	}
	s.publish(org.ID, "projects.changed", project, nil)
	if err := s.record(ctx, toAuditActor(actor), org.ID, "project.create",
		audit.Resource{Type: "project", ID: project.ID, Name: project.Slug}, audit.Details{}, AuditMeta{}); err != nil {
		s.Logger.Error("audit project.create failed", "project_id", project.ID, "error", err)
	}
	return project, envs, nil
}

func environmentsForProject(projectID string, templates []EnvironmentTemplate) []store.Environment {
	if len(templates) > 0 {
		envs := make([]store.Environment, 0, len(templates))
		for i, template := range templates {
			color := template.Color
			if color == "" {
				color = defaultEnvColor(template.Type)
			}
			envs = append(envs, store.Environment{
				ID: uuid.NewString(), ProjectID: projectID, Slug: template.Slug, Name: template.Name,
				Type: template.Type, Color: color, Position: i, IsProtected: template.Protected || template.Type == "production",
			})
		}
		return envs
	}
	return defaultEnvironments(projectID)
}

func defaultEnvColor(envType string) string {
	switch envType {
	case "development":
		return "#3b82f6"
	case "staging":
		return "#14b8a6"
	case "production":
		return "#f59e0b"
	default:
		return "#8b5cf6"
	}
}

func defaultEnvironments(projectID string) []store.Environment {
	return []store.Environment{
		{ID: uuid.NewString(), ProjectID: projectID, Slug: "dev", Name: "Development", Type: "development", Color: "#3b82f6", Position: 0},
		{ID: uuid.NewString(), ProjectID: projectID, Slug: "staging", Name: "Staging", Type: "staging", Color: "#14b8a6", Position: 1},
		{ID: uuid.NewString(), ProjectID: projectID, Slug: "prod", Name: "Production", Type: "production", Color: "#f59e0b", Position: 2, IsProtected: true},
	}
}

func ptrEnvs(envs []store.Environment) []*store.Environment {
	out := make([]*store.Environment, len(envs))
	for i := range envs {
		out[i] = &envs[i]
	}
	return out
}

// ListProjects returns projects in an org.
func (s *Service) ListProjects(ctx context.Context, actor authz.Actor, orgSlug string) ([]store.Project, error) {
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := s.Authorize(actor, authz.ProjectsRead, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, err
	}
	return s.Store.ListProjects(ctx, org.ID)
}

// GetProject loads a project and its environments.
func (s *Service) GetProject(ctx context.Context, actor authz.Actor, orgSlug, projectSlug string) (*store.Project, []store.Environment, error) {
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	if err := s.Authorize(actor, authz.ProjectsRead, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, nil, err
	}
	project, err := s.Store.GetProjectBySlug(ctx, org.ID, projectSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	envs, err := s.Store.ListEnvironments(ctx, project.ID)
	if err != nil {
		return nil, nil, err
	}
	return project, envs, nil
}

// CreateEnvironment adds an environment to a project.
func (s *Service) CreateEnvironment(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, name, rawSlug, envType string) (*store.Environment, error) {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.EnvironmentsManage)
	if err != nil {
		return nil, err
	}
	slug, err := NormalizeSlug(rawSlug)
	if err != nil {
		return nil, err
	}
	if !validEnvType(envType) {
		return nil, fmt.Errorf("%w: environment type must be development, preview, staging, or production", ErrValidation)
	}
	envs, err := s.Store.ListEnvironments(ctx, project.ID)
	if err != nil {
		return nil, err
	}
	color := envColor(envType)
	env := &store.Environment{
		ID:          uuid.NewString(),
		ProjectID:   project.ID,
		Slug:        slug,
		Name:        strings.TrimSpace(name),
		Type:        envType,
		Color:       color,
		Position:    len(envs),
		IsProtected: envType == "production",
	}
	if env.Name == "" {
		return nil, fmt.Errorf("%w: environment name is required", ErrValidation)
	}
	if err := s.Store.CreateEnvironment(ctx, env); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrConflict
		}
		return nil, err
	}
	s.publish(org.ID, "environments.changed", project, env)
	if err := s.record(ctx, toAuditActor(actor), org.ID, "env.create",
		audit.Resource{Type: "environment", ID: env.ID, Name: env.Slug, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{Environment: env.Slug}, AuditMeta{}); err != nil {
		s.Logger.Error("audit env.create failed", "env_id", env.ID, "error", err)
	}
	return env, nil
}

func validEnvType(t string) bool {
	switch t {
	case "development", "preview", "staging", "production":
		return true
	default:
		return false
	}
}

func envColor(t string) string {
	switch t {
	case "development":
		return "#3b82f6"
	case "preview":
		return "#8b5cf6"
	case "staging":
		return "#14b8a6"
	case "production":
		return "#f59e0b"
	default:
		return "#71717a"
	}
}

func (s *Service) projectForWrite(ctx context.Context, actor authz.Actor, orgSlug, projectSlug string, perm authz.Permission) (*store.Org, *store.Project, error) {
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	project, err := s.Store.GetProjectBySlug(ctx, org.ID, projectSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	if err := s.Authorize(actor, perm, authz.Scope{OrgID: org.ID, ProjectID: project.ID}); err != nil {
		return nil, nil, err
	}
	return org, project, nil
}

func parsePermissions(raw string) []authz.Permission {
	if raw == "" {
		return nil
	}
	var names []string
	if err := json.Unmarshal([]byte(raw), &names); err != nil {
		return nil
	}
	out := make([]authz.Permission, 0, len(names))
	for _, name := range names {
		perm := authz.Permission(name)
		if perm.Valid() {
			out = append(out, perm)
		}
	}
	return out
}

func toAuditActor(actor authz.Actor) audit.Actor {
	return audit.Actor{ID: actor.ID, Kind: string(actor.Kind), Name: actor.Name}
}

func (s *Service) record(ctx context.Context, actor audit.Actor, orgID, action string, resource audit.Resource, details audit.Details, meta AuditMeta) error {
	ev := &audit.Event{
		ID:        uuid.NewString(),
		OrgID:     orgID,
		Actor:     actor,
		Action:    action,
		Resource:  resource,
		Details:   details,
		IP:        meta.IP,
		UserAgent: meta.UserAgent,
		CreatedAt: s.Now().UTC(),
	}
	if err := s.Store.AppendAuditEvent(ctx, ev); err != nil {
		return err
	}
	if s.AuditSink != nil {
		s.AuditSink.Publish(ev)
	}
	if s.webhookQueue != nil {
		select {
		case s.webhookQueue <- ev:
		default:
			s.Logger.Warn("webhook queue full; dropping event", "event_id", ev.ID)
		}
	}
	return nil
}

// ProjectKeys unwraps the org and project keys and derives the value and blind
// index keys. Callers must Zero the returned keys.
func (s *Service) ProjectKeys(ctx context.Context, org *store.Org, project *store.Project) (valueKey, indexKey crypto.Key, err error) {
	orgKey, err := s.Keyring.UnwrapOrgKey(org.ID, org.WrappedOrgKey)
	if err != nil {
		return valueKey, indexKey, fmt.Errorf("service: unwrap org key: %w", err)
	}
	defer orgKey.Zero()
	projectKey, err := crypto.UnwrapProjectKey(orgKey, org.ID, project.ID, project.WrappedProjectKey)
	if err != nil {
		return valueKey, indexKey, fmt.Errorf("service: unwrap project key: %w", err)
	}
	defer projectKey.Zero()
	if valueKey, err = crypto.DeriveValueKey(projectKey); err != nil {
		return valueKey, indexKey, err
	}
	if indexKey, err = crypto.DeriveBlindIndexKey(projectKey); err != nil {
		return valueKey, indexKey, err
	}
	return valueKey, indexKey, nil
}

// UpdateOrg changes an org's name or slug.
func (s *Service) UpdateOrg(ctx context.Context, actor authz.Actor, orgSlug, name, newSlug string, meta AuditMeta) (*store.Org, error) {
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := s.Authorize(actor, authz.SettingsManage, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return nil, fmt.Errorf("%w: organization name is required and must be under 100 characters", ErrValidation)
	}
	if newSlug == "" {
		newSlug = org.Slug
	}
	slug, err := NormalizeSlug(newSlug)
	if err != nil {
		return nil, err
	}
	org.Name = name
	org.Slug = slug
	if err := s.Store.UpdateOrg(ctx, org); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrConflict
		}
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "org.update",
		audit.Resource{Type: "org", ID: org.ID, Name: org.Slug}, audit.Details{}, meta); err != nil {
		s.Logger.Error("audit org.update failed", "org_id", org.ID, "error", err)
	}
	return org, nil
}

// DeleteOrg removes an org. Only Owners hold org:delete.
func (s *Service) DeleteOrg(ctx context.Context, actor authz.Actor, orgSlug string, meta AuditMeta) error {
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.Authorize(actor, authz.OrgDelete, authz.Scope{OrgID: org.ID}); err != nil {
		return err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "org.delete",
		audit.Resource{Type: "org", ID: org.ID, Name: org.Slug}, audit.Details{}, meta); err != nil {
		s.Logger.Error("audit org.delete failed", "org_id", org.ID, "error", err)
	}
	return s.Store.DeleteOrg(ctx, org.ID)
}

// UpdateProject changes a project's name or slug.
func (s *Service) UpdateProject(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, name, newSlug string, meta AuditMeta) (*store.Project, error) {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.ProjectsManage)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return nil, fmt.Errorf("%w: project name is required and must be under 100 characters", ErrValidation)
	}
	if newSlug == "" {
		newSlug = project.Slug
	}
	slug, err := NormalizeSlug(newSlug)
	if err != nil {
		return nil, err
	}
	project.Name = name
	project.Slug = slug
	if err := s.Store.UpdateProject(ctx, project); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrConflict
		}
		return nil, err
	}
	s.publish(org.ID, "projects.changed", project, nil)
	if err := s.record(ctx, toAuditActor(actor), org.ID, "project.update",
		audit.Resource{Type: "project", ID: project.ID, Name: project.Slug}, audit.Details{}, meta); err != nil {
		s.Logger.Error("audit project.update failed", "project_id", project.ID, "error", err)
	}
	return project, nil
}

// DeleteProject removes a project and everything in it.
func (s *Service) DeleteProject(ctx context.Context, actor authz.Actor, orgSlug, projectSlug string, meta AuditMeta) error {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.ProjectsManage)
	if err != nil {
		return err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "project.delete",
		audit.Resource{Type: "project", ID: project.ID, Name: project.Slug}, audit.Details{}, meta); err != nil {
		s.Logger.Error("audit project.delete failed", "project_id", project.ID, "error", err)
	}
	if err := s.Store.DeleteProject(ctx, project.ID); err != nil {
		return err
	}
	s.publish(org.ID, "projects.changed", project, nil)
	return nil
}

// OrgSettings is the subset of org settings the API exposes.
type OrgSettings struct {
	DefaultAppearance    string                `json:"default_appearance,omitempty"`
	RequireMFA           bool                  `json:"require_mfa,omitempty"`
	SessionIdleHours     int                   `json:"session_idle_hours,omitempty"`
	SessionAbsoluteDays  int                   `json:"session_absolute_days,omitempty"`
	IPAllowlist          []string              `json:"ip_allowlist,omitempty"`
	BlockCLIWrites       bool                  `json:"block_cli_writes,omitempty"`
	EnvironmentTemplates []EnvironmentTemplate `json:"environment_templates,omitempty"`
	SSOEnabled           bool                  `json:"sso_enabled,omitempty"`
	SSOProvider          string                `json:"sso_provider,omitempty"`
	SSOClientID          string                `json:"sso_client_id,omitempty"`
	SSOClientSecret      []byte                `json:"sso_client_secret,omitempty"`
	SSOIssuer            string                `json:"sso_issuer,omitempty"`
	SSOEnforce           bool                  `json:"sso_enforce,omitempty"`
	SSOAllowedDomains    []string              `json:"sso_allowed_domains,omitempty"`
	SSORole              string                `json:"sso_role,omitempty"`
	Webhooks             []Webhook             `json:"webhooks,omitempty"`
	SSOIdPMetadata       string                `json:"sso_idp_metadata,omitempty"`
	SSOSPCertificate     string                `json:"sso_sp_certificate,omitempty"`
	SSOSPPrivateKey      []byte                `json:"sso_sp_private_key,omitempty"`
	GitHubWebhookSecret  []byte                `json:"github_webhook_secret,omitempty"`
	PreviewEnabled       bool                  `json:"preview_enabled,omitempty"`
	PreviewRetentionDays int                   `json:"preview_retention_days,omitempty"`
}

// EnvironmentTemplate seeds new projects.
type EnvironmentTemplate struct {
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Color     string `json:"color,omitempty"`
	Protected bool   `json:"protected,omitempty"`
}

// OrgSettingsPatch updates only the provided settings.
type OrgSettingsPatch struct {
	DefaultAppearance    *string
	RequireMFA           *bool
	SessionIdleHours     *int
	SessionAbsoluteDays  *int
	IPAllowlist          *[]string
	BlockCLIWrites       *bool
	EnvironmentTemplates *[]EnvironmentTemplate
	SSOEnabled           *bool
	SSOProvider          *string
	SSOClientID          *string
	SSOClientSecret      *string
	SSOIssuer            *string
	SSOEnforce           *bool
	SSOAllowedDomains    *[]string
	SSORole              *string
	SSOIdPMetadata       *string
	GitHubWebhookSecret  *string
	PreviewEnabled       *bool
	PreviewRetentionDays *int
}

// ParseOrgSettings decodes an org settings document, tolerating unknown fields.
func ParseOrgSettings(raw string) OrgSettings {
	var settings OrgSettings
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &settings)
	}
	return settings
}

func validateEnvironmentTemplates(in []EnvironmentTemplate) ([]EnvironmentTemplate, error) {
	if len(in) == 0 {
		return nil, nil
	}
	if len(in) > 20 {
		return nil, fmt.Errorf("%w: at most 20 environment templates are allowed", ErrValidation)
	}
	seen := make(map[string]bool, len(in))
	out := make([]EnvironmentTemplate, 0, len(in))
	for _, template := range in {
		slug, err := NormalizeSlug(template.Slug)
		if err != nil {
			return nil, err
		}
		if seen[slug] {
			return nil, fmt.Errorf("%w: duplicate environment slug %q", ErrValidation, slug)
		}
		seen[slug] = true
		name := strings.TrimSpace(template.Name)
		if name == "" || len(name) > 100 {
			return nil, fmt.Errorf("%w: environment name is required and must be under 100 characters", ErrValidation)
		}
		switch template.Type {
		case "development", "preview", "staging", "production":
		default:
			return nil, fmt.Errorf("%w: unknown environment type %q", ErrValidation, template.Type)
		}
		out = append(out, EnvironmentTemplate{Slug: slug, Name: name, Type: template.Type, Color: template.Color, Protected: template.Protected})
	}
	return out, nil
}

// UpdateOrgSettings applies security and appearance defaults.
func (s *Service) UpdateOrgSettings(ctx context.Context, actor authz.Actor, orgSlug string, patch OrgSettingsPatch, meta AuditMeta) (*store.Org, OrgSettings, error) {
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, OrgSettings{}, ErrNotFound
		}
		return nil, OrgSettings{}, err
	}
	if err := s.Authorize(actor, authz.SettingsManage, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, OrgSettings{}, err
	}
	settings := ParseOrgSettings(org.Settings)
	if patch.DefaultAppearance != nil {
		if *patch.DefaultAppearance != "classic" && *patch.DefaultAppearance != "warm" {
			return nil, settings, fmt.Errorf("%w: default appearance must be classic or warm", ErrValidation)
		}
		settings.DefaultAppearance = *patch.DefaultAppearance
	}
	if patch.RequireMFA != nil {
		settings.RequireMFA = *patch.RequireMFA
	}
	if patch.SessionIdleHours != nil {
		if *patch.SessionIdleHours < 0 || *patch.SessionIdleHours > 168 {
			return nil, settings, fmt.Errorf("%w: session idle hours must be between 0 and 168", ErrValidation)
		}
		settings.SessionIdleHours = *patch.SessionIdleHours
	}
	if patch.SessionAbsoluteDays != nil {
		if *patch.SessionAbsoluteDays < 0 || *patch.SessionAbsoluteDays > 365 {
			return nil, settings, fmt.Errorf("%w: session absolute days must be between 0 and 365", ErrValidation)
		}
		settings.SessionAbsoluteDays = *patch.SessionAbsoluteDays
	}
	if patch.BlockCLIWrites != nil {
		settings.BlockCLIWrites = *patch.BlockCLIWrites
	}
	if patch.EnvironmentTemplates != nil {
		templates, err := validateEnvironmentTemplates(*patch.EnvironmentTemplates)
		if err != nil {
			return nil, OrgSettings{}, err
		}
		settings.EnvironmentTemplates = templates
	}
	if patch.SSOProvider != nil {
		provider := strings.ToLower(strings.TrimSpace(*patch.SSOProvider))
		switch provider {
		case "", "google", "github", "microsoft", "okta", "generic", "saml":
		default:
			return nil, settings, fmt.Errorf("%w: unknown SSO provider %q", ErrValidation, provider)
		}
		settings.SSOProvider = provider
	}
	if patch.SSOClientID != nil {
		settings.SSOClientID = strings.TrimSpace(*patch.SSOClientID)
	}
	if patch.SSOIssuer != nil {
		settings.SSOIssuer = strings.TrimRight(strings.TrimSpace(*patch.SSOIssuer), "/")
	}
	if patch.SSOClientSecret != nil {
		if *patch.SSOClientSecret == "" {
			settings.SSOClientSecret = nil
		} else {
			ciphertext, err := s.encryptSSOSecret(org, *patch.SSOClientSecret)
			if err != nil {
				return nil, settings, err
			}
			settings.SSOClientSecret = ciphertext
		}
	}
	if patch.SSOEnabled != nil {
		settings.SSOEnabled = *patch.SSOEnabled
	}
	if patch.SSOEnforce != nil {
		settings.SSOEnforce = *patch.SSOEnforce
	}
	if patch.SSOAllowedDomains != nil {
		domains := make([]string, 0, len(*patch.SSOAllowedDomains))
		for _, domain := range *patch.SSOAllowedDomains {
			domain = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(domain), "@"))
			if domain == "" {
				continue
			}
			if strings.ContainsAny(domain, " /:\t") {
				return nil, settings, fmt.Errorf("%w: invalid SSO email domain %q", ErrValidation, domain)
			}
			domains = append(domains, domain)
		}
		settings.SSOAllowedDomains = domains
	}
	if patch.SSOIdPMetadata != nil {
		settings.SSOIdPMetadata = strings.TrimSpace(*patch.SSOIdPMetadata)
	}
	if patch.GitHubWebhookSecret != nil {
		if *patch.GitHubWebhookSecret == "" {
			settings.GitHubWebhookSecret = nil
		} else {
			ciphertext, err := s.encryptOrgSecret(org, "github-preview", "webhook_secret", *patch.GitHubWebhookSecret)
			if err != nil {
				return nil, settings, err
			}
			settings.GitHubWebhookSecret = ciphertext
		}
	}
	if patch.PreviewEnabled != nil {
		settings.PreviewEnabled = *patch.PreviewEnabled
	}
	if patch.PreviewRetentionDays != nil {
		days := *patch.PreviewRetentionDays
		if days < 1 || days > 90 {
			return nil, settings, fmt.Errorf("%w: preview retention must be between 1 and 90 days", ErrValidation)
		}
		settings.PreviewRetentionDays = days
	}
	if settings.PreviewEnabled && len(settings.GitHubWebhookSecret) == 0 {
		return nil, settings, fmt.Errorf("%w: preview environments require a GitHub webhook secret", ErrValidation)
	}
	if settings.PreviewEnabled && settings.PreviewRetentionDays == 0 {
		settings.PreviewRetentionDays = 7
	}
	if patch.SSORole != nil {
		role := strings.TrimSpace(*patch.SSORole)
		if role == "" {
			role = authz.RoleDeveloper
		}
		if _, err := s.Store.GetRoleByName(ctx, org.ID, role); err != nil {
			return nil, settings, fmt.Errorf("%w: unknown SSO role %q", ErrValidation, role)
		}
		settings.SSORole = role
	}
	if settings.SSOEnforce && !settings.SSOEnabled {
		return nil, settings, fmt.Errorf("%w: SSO enforcement requires SSO to be enabled", ErrValidation)
	}
	if settings.SSOEnabled && settings.SSOProvider == "saml" {
		if settings.SSOIdPMetadata == "" {
			return nil, settings, fmt.Errorf("%w: SAML IdP metadata is required", ErrValidation)
		}
		if len(settings.SSOSPPrivateKey) == 0 || settings.SSOSPCertificate == "" {
			certificate, privateKey, err := generateSAMLKeypair()
			if err != nil {
				return nil, settings, err
			}
			ciphertext, err := s.encryptSSOSecret(org, privateKey)
			if err != nil {
				return nil, settings, err
			}
			settings.SSOSPCertificate = certificate
			settings.SSOSPPrivateKey = ciphertext
		}
		if settings.SSORole == "" {
			settings.SSORole = authz.RoleDeveloper
		}
	} else if settings.SSOEnabled {
		if settings.SSOClientID == "" {
			return nil, settings, fmt.Errorf("%w: SSO client ID is required", ErrValidation)
		}
		if settings.SSOProvider != "github" && settings.SSOIssuer == "" {
			switch settings.SSOProvider {
			case "google":
				settings.SSOIssuer = "https://accounts.google.com"
			case "microsoft":
				settings.SSOIssuer = "https://login.microsoftonline.com/common/v2.0"
			default:
				return nil, settings, fmt.Errorf("%w: SSO issuer is required", ErrValidation)
			}
		}
		if settings.SSORole == "" {
			settings.SSORole = authz.RoleDeveloper
		}
	}
	if patch.IPAllowlist != nil {
		if len(*patch.IPAllowlist) > 100 {
			return nil, settings, fmt.Errorf("%w: at most 100 IP ranges are allowed", ErrValidation)
		}
		normalized := make([]string, 0, len(*patch.IPAllowlist))
		for _, entry := range *patch.IPAllowlist {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			prefix, err := netip.ParsePrefix(entry)
			if err != nil {
				addr, addrErr := netip.ParseAddr(entry)
				if addrErr != nil {
					return nil, settings, fmt.Errorf("%w: invalid IP range %q", ErrValidation, entry)
				}
				prefix = netip.PrefixFrom(addr, addr.BitLen())
			}
			normalized = append(normalized, prefix.Masked().String())
		}
		settings.IPAllowlist = normalized
	}

	raw := map[string]any{}
	if org.Settings != "" {
		_ = json.Unmarshal([]byte(org.Settings), &raw)
	}
	raw["default_appearance"] = settings.DefaultAppearance
	raw["require_mfa"] = settings.RequireMFA
	raw["session_idle_hours"] = settings.SessionIdleHours
	raw["session_absolute_days"] = settings.SessionAbsoluteDays
	raw["ip_allowlist"] = settings.IPAllowlist
	raw["block_cli_writes"] = settings.BlockCLIWrites
	raw["environment_templates"] = settings.EnvironmentTemplates
	raw["sso_enabled"] = settings.SSOEnabled
	raw["sso_provider"] = settings.SSOProvider
	raw["sso_client_id"] = settings.SSOClientID
	raw["sso_client_secret"] = settings.SSOClientSecret
	raw["sso_issuer"] = settings.SSOIssuer
	raw["sso_enforce"] = settings.SSOEnforce
	raw["sso_allowed_domains"] = settings.SSOAllowedDomains
	raw["sso_role"] = settings.SSORole
	raw["sso_idp_metadata"] = settings.SSOIdPMetadata
	raw["sso_sp_certificate"] = settings.SSOSPCertificate
	raw["sso_sp_private_key"] = settings.SSOSPPrivateKey
	raw["github_webhook_secret"] = settings.GitHubWebhookSecret
	raw["preview_enabled"] = settings.PreviewEnabled
	raw["preview_retention_days"] = settings.PreviewRetentionDays
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, settings, err
	}
	org.Settings = string(encoded)
	if err := s.Store.UpdateOrgSettings(ctx, org.ID, org.Settings); err != nil {
		return nil, settings, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "org.settings",
		audit.Resource{Type: "org", ID: org.ID, Name: org.Slug},
		audit.Details{Reason: settings.DefaultAppearance}, meta); err != nil {
		s.Logger.Error("audit org.settings failed", "org_id", org.ID, "error", err)
	}
	return org, settings, nil
}

// IPv4/6 checks for the allowlist live in the server so machine tokens and
// browser sessions share one implementation.

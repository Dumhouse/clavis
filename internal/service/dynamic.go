package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/store"
)

// DynamicConfigView omits the encrypted engine configuration.
type DynamicConfigView struct {
	ID         string
	Engine     string
	DefaultTTL int
	MaxTTL     int
	CreatedAt  time.Time
}

// LeaseView omits credential material.
type LeaseView struct {
	ID         string
	ConfigID   string
	IdentityID string
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
}

// LeaseResult includes the one-time credential payload.
type LeaseResult struct {
	Lease LeaseView
	Data  map[string]string
}

type postgresDynamicConfig struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Database       string `json:"database"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	SSLMode        string `json:"sslmode"`
	GrantRole      string `json:"grant_role,omitempty"`
	RoleTemplate   string `json:"role_template,omitempty"`
	RevokeTemplate string `json:"revoke_template,omitempty"`
}

// CreateDynamicConfig stores an encrypted engine configuration.
func (s *Service) CreateDynamicConfig(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, engine string, config map[string]any, defaultTTL, maxTTL int, meta AuditMeta) (*DynamicConfigView, error) {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.ProjectsManage)
	if err != nil {
		return nil, err
	}
	engine = strings.ToLower(strings.TrimSpace(engine))
	switch engine {
	case "postgres", "postgresql":
		engine = "postgres"
		if err := validatePostgresDynamic(config); err != nil {
			return nil, err
		}
	case "aws", "aws-sts":
		engine = "aws"
		if err := validateAWSDynamic(config); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("%w: supported dynamic engines are postgres and aws", ErrValidation)
	}
	if defaultTTL <= 0 {
		defaultTTL = 3600
	}
	if maxTTL <= 0 {
		maxTTL = 86400
	}
	if defaultTTL > maxTTL || maxTTL > 7*24*3600 {
		return nil, fmt.Errorf("%w: TTLs must be positive, default <= max, and max <= 7 days", ErrValidation)
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	id := uuid.NewString()
	ciphertext, err := s.encryptOrgSecret(org, id, "dynamic_config", string(raw))
	if err != nil {
		return nil, err
	}
	row := &store.DynamicSecretConfig{
		ID: id, ProjectID: project.ID, Engine: engine, Config: ciphertext,
		DefaultTTL: defaultTTL, MaxTTL: maxTTL, CreatedAt: s.Now().UTC(),
	}
	if err := s.Store.CreateDynamicSecretConfig(ctx, row); err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "dynamic.config.create",
		audit.Resource{Type: "project", ID: project.ID, Name: project.Slug, ProjectID: project.ID},
		audit.Details{Count: 1, Reason: engine}, meta); err != nil {
		s.Logger.Error("audit dynamic.config.create failed", "config_id", id, "error", err)
	}
	return &DynamicConfigView{ID: row.ID, Engine: row.Engine, DefaultTTL: row.DefaultTTL, MaxTTL: row.MaxTTL, CreatedAt: row.CreatedAt}, nil
}

func validatePostgresDynamic(config map[string]any) error {
	for _, key := range []string{"host", "database", "username", "password"} {
		if value, _ := config[key].(string); strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: postgres config needs %s", ErrValidation, key)
		}
	}
	if grant, _ := config["grant_role"].(string); grant != "" && !safeRoleName(grant) {
		return fmt.Errorf("%w: grant_role must be a simple role name", ErrValidation)
	}
	return nil
}

func safeRoleName(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9' && i > 0) {
			continue
		}
		return false
	}
	return true
}

// ListDynamicConfigs returns project configs without secrets.
func (s *Service) ListDynamicConfigs(ctx context.Context, actor authz.Actor, orgSlug, projectSlug string) ([]DynamicConfigView, error) {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.ProjectsRead)
	if err != nil {
		return nil, err
	}
	_ = org
	configs, err := s.Store.ListDynamicSecretConfigs(ctx, project.ID)
	if err != nil {
		return nil, err
	}
	out := make([]DynamicConfigView, 0, len(configs))
	for _, config := range configs {
		out = append(out, DynamicConfigView{
			ID: config.ID, Engine: config.Engine, DefaultTTL: config.DefaultTTL,
			MaxTTL: config.MaxTTL, CreatedAt: config.CreatedAt,
		})
	}
	return out, nil
}

// CreateLease issues dynamic credentials.
func (s *Service) CreateLease(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, configID string, ttlSeconds int, meta AuditMeta) (*LeaseResult, error) {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.SecretsUse)
	if err != nil {
		return nil, err
	}
	config, err := s.Store.GetDynamicSecretConfig(ctx, project.ID, configID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if ttlSeconds <= 0 {
		ttlSeconds = config.DefaultTTL
	}
	if ttlSeconds > config.MaxTTL {
		ttlSeconds = config.MaxTTL
	}
	leaseID := uuid.NewString()
	expires := s.Now().UTC().Add(time.Duration(ttlSeconds) * time.Second)
	var data map[string]string
	if config.Engine == "aws" {
		settings, err := s.decryptAWSDynamicConfig(org, config)
		if err != nil {
			return nil, err
		}
		data, err = s.createAWSCredentials(ctx, settings, leaseID, ttlSeconds)
		if err != nil {
			return nil, err
		}
	} else {
		settings, err := s.decryptDynamicConfig(org, config)
		if err != nil {
			return nil, err
		}
		data, err = s.createPostgresCredentials(ctx, leaseID, expires, settings)
		if err != nil {
			return nil, err
		}
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	valueKey, _, err := s.ProjectKeys(ctx, org, project)
	if err != nil {
		return nil, err
	}
	defer valueKey.Zero()
	ciphertext, err := valueKey.Encrypt(uint32(project.KeyVersion), crypto.Secret(raw),
		crypto.BindAAD(org.ID, project.ID, config.ID, leaseID, "lease", 1))
	if err != nil {
		return nil, err
	}
	lease := &store.Lease{
		ID: leaseID, ConfigID: config.ID, IdentityID: actor.ID, LeaseData: ciphertext,
		ExpiresAt: expires, CreatedAt: s.Now().UTC(),
	}
	if err := s.Store.CreateLease(ctx, lease); err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "lease.create",
		audit.Resource{Type: "project", ID: project.ID, Name: project.Slug, ProjectID: project.ID},
		audit.Details{Count: 1, Reason: config.Engine}, meta); err != nil {
		s.Logger.Error("audit lease.create failed", "lease_id", lease.ID, "error", err)
	}
	return &LeaseResult{Lease: *leaseView(lease), Data: data}, nil
}

func (s *Service) createPostgresCredentials(ctx context.Context, leaseID string, expires time.Time, config *postgresDynamicConfig) (map[string]string, error) {
	name := "clavis_" + strings.ReplaceAll(leaseID, "-", "")[:12]
	passwordBytes := make([]byte, 24)
	if _, err := rand.Read(passwordBytes); err != nil {
		return nil, err
	}
	password := base64.RawURLEncoding.EncodeToString(passwordBytes)
	roleTemplate := config.RoleTemplate
	if roleTemplate == "" {
		roleTemplate = `CREATE ROLE {{name}} LOGIN PASSWORD '{{password}}' VALID UNTIL '{{expires}}'`
	}
	statement := strings.NewReplacer(
		"{{name}}", name,
		"{{password}}", password,
		"{{expires}}", expires.Format(time.RFC3339),
		"{{database}}", config.Database,
	).Replace(roleTemplate)
	db, err := s.postgresAdminClient(config)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, statement); err != nil {
		return nil, fmt.Errorf("dynamic postgres: create role: %w", err)
	}
	if config.GrantRole != "" {
		grant := `GRANT "` + strings.ReplaceAll(config.GrantRole, `"`, `""`) + `" TO "` + name + `"`
		if _, err := db.ExecContext(ctx, grant); err != nil {
			_, _ = db.ExecContext(context.Background(), `DROP ROLE IF EXISTS "`+name+`"`)
			return nil, fmt.Errorf("dynamic postgres: grant role: %w", err)
		}
	}
	return map[string]string{
		"engine": "postgres", "username": name, "password": password,
		"host": config.Host, "port": fmt.Sprintf("%d", config.Port),
		"database": config.Database,
	}, nil
}

func (s *Service) postgresAdminClient(config *postgresDynamicConfig) (*sql.DB, error) {
	port := config.Port
	if port == 0 {
		port = 5432
	}
	sslmode := config.SSLMode
	if sslmode == "" {
		sslmode = "disable"
	}
	dsn := "postgres://" + url.QueryEscape(config.Username) + ":" + url.QueryEscape(config.Password) +
		"@" + config.Host + ":" + fmt.Sprintf("%d", port) + "/" + url.PathEscape(config.Database) +
		"?sslmode=" + url.QueryEscape(sslmode)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("dynamic postgres: connect: %w", err)
	}
	return db, nil
}

func (s *Service) decryptDynamicConfig(org *store.Org, config *store.DynamicSecretConfig) (*postgresDynamicConfig, error) {
	raw, err := s.decryptOrgSecret(org, config.ID, "dynamic_config", config.Config)
	if err != nil {
		return nil, err
	}
	var out postgresDynamicConfig
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	if out.Port == 0 {
		out.Port = 5432
	}
	return &out, nil
}

// RenewLease extends a lease up to its config max TTL.
func (s *Service) RenewLease(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, leaseID string, ttlSeconds int, meta AuditMeta) (*LeaseView, error) {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.SecretsUse)
	if err != nil {
		return nil, err
	}
	lease, err := s.Store.GetLease(ctx, leaseID)
	if err != nil {
		return nil, ErrNotFound
	}
	if lease.RevokedAt != nil {
		return nil, fmt.Errorf("%w: lease is revoked", ErrConflict)
	}
	config, err := s.Store.GetDynamicSecretConfig(ctx, project.ID, lease.ConfigID)
	if err != nil {
		return nil, ErrNotFound
	}
	if ttlSeconds <= 0 {
		ttlSeconds = config.DefaultTTL
	}
	expires := s.Now().UTC().Add(time.Duration(ttlSeconds) * time.Second)
	maxExpiry := lease.CreatedAt.Add(time.Duration(config.MaxTTL) * time.Second)
	if expires.After(maxExpiry) {
		expires = maxExpiry
	}
	if !expires.After(s.Now().UTC()) {
		return nil, fmt.Errorf("%w: lease has reached its maximum TTL", ErrConflict)
	}
	settings, err := s.decryptDynamicConfig(org, config)
	if err != nil {
		return nil, err
	}
	valueKey, _, err := s.ProjectKeys(ctx, org, project)
	if err != nil {
		return nil, err
	}
	defer valueKey.Zero()
	raw, _, err := valueKey.Decrypt(lease.LeaseData,
		crypto.BindAAD(org.ID, project.ID, config.ID, lease.ID, "lease", 1))
	if err != nil {
		return nil, err
	}
	var data map[string]string
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	if settings.Host != "" {
		statement := `ALTER ROLE "` + strings.ReplaceAll(data["username"], `"`, `""`) +
			`" VALID UNTIL '` + expires.Format(time.RFC3339) + `'`
		db, err := s.postgresAdminClient(settings)
		if err != nil {
			return nil, err
		}
		_, execErr := db.ExecContext(ctx, statement)
		db.Close()
		if execErr != nil {
			return nil, execErr
		}
	}
	if config.Engine == "aws" {
		return nil, fmt.Errorf("%w: AWS STS leases cannot be renewed; create a new lease", ErrConflict)
	}
	if err := s.Store.UpdateLeaseExpiry(ctx, lease.ID, expires); err != nil {
		return nil, err
	}
	lease.ExpiresAt = expires
	return &LeaseView{ID: lease.ID, ConfigID: lease.ConfigID, IdentityID: lease.IdentityID,
		ExpiresAt: lease.ExpiresAt, CreatedAt: lease.CreatedAt}, nil
}

// RevokeLease drops credentials and marks the lease revoked.
func (s *Service) RevokeLease(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, leaseID string, meta AuditMeta) error {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.SecretsDelete)
	if err != nil {
		return err
	}
	return s.revokeLease(ctx, org, project, leaseID, toAuditActor(actor), meta)
}

func (s *Service) revokeLease(ctx context.Context, org *store.Org, project *store.Project, leaseID string, actor audit.Actor, meta AuditMeta) error {
	lease, err := s.Store.GetLease(ctx, leaseID)
	if err != nil {
		return ErrNotFound
	}
	if lease.RevokedAt != nil {
		return nil
	}
	config, err := s.Store.GetDynamicSecretConfig(ctx, project.ID, lease.ConfigID)
	if err != nil {
		return err
	}
	if config.Engine == "aws" {
		if err := s.Store.RevokeLease(ctx, lease.ID, s.Now().UTC()); err != nil {
			return err
		}
		return s.record(ctx, actor, org.ID, "lease.revoke",
			audit.Resource{Type: "project", ID: project.ID, Name: project.Slug, ProjectID: project.ID},
			audit.Details{Count: 1}, meta)
	}
	settings, err := s.decryptDynamicConfig(org, config)
	if err != nil {
		return err
	}
	valueKey, _, err := s.ProjectKeys(ctx, org, project)
	if err != nil {
		return err
	}
	defer valueKey.Zero()
	raw, _, err := valueKey.Decrypt(lease.LeaseData,
		crypto.BindAAD(org.ID, project.ID, config.ID, lease.ID, "lease", 1))
	if err != nil {
		return err
	}
	var data map[string]string
	if err := json.Unmarshal(raw, &data); err != nil {
		return err
	}
	username := data["username"]
	if username != "" {
		template := settings.RevokeTemplate
		if template == "" {
			template = `DROP ROLE IF EXISTS {{name}}`
		}
		statement := strings.NewReplacer("{{name}}", username, "{{database}}", settings.Database).Replace(template)
		db, err := s.postgresAdminClient(settings)
		if err == nil {
			_, _ = db.ExecContext(ctx, statement)
			db.Close()
		}
	}
	if err := s.Store.RevokeLease(ctx, lease.ID, s.Now().UTC()); err != nil {
		return err
	}
	if err := s.record(ctx, actor, org.ID, "lease.revoke",
		audit.Resource{Type: "project", ID: project.ID, Name: project.Slug, ProjectID: project.ID},
		audit.Details{Count: 1}, meta); err != nil {
		s.Logger.Error("audit lease.revoke failed", "lease_id", lease.ID, "error", err)
	}
	return nil
}

// ListLeases returns lease metadata for a config.
func (s *Service) ListLeases(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, configID string) ([]LeaseView, error) {
	if _, _, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.ProjectsRead); err != nil {
		return nil, err
	}
	leases, err := s.Store.ListLeases(ctx, configID, true)
	if err != nil {
		return nil, err
	}
	out := make([]LeaseView, 0, len(leases))
	for i := range leases {
		out = append(out, *leaseView(&leases[i]))
	}
	return out, nil
}

func leaseView(lease *store.Lease) *LeaseView {
	return &LeaseView{
		ID: lease.ID, ConfigID: lease.ConfigID, IdentityID: lease.IdentityID,
		ExpiresAt: lease.ExpiresAt, RevokedAt: lease.RevokedAt, CreatedAt: lease.CreatedAt,
	}
}

// RevokeExpiredLeases is called by the background worker.
func (s *Service) RevokeExpiredLeases(ctx context.Context) {
	leases, err := s.Store.ListExpiredLeases(ctx, s.Now().UTC())
	if err != nil {
		s.Logger.Error("list expired leases failed", "error", err)
		return
	}
	actor := audit.Actor{ID: "lease-expiry", Kind: string(authz.SubjectMachine), Name: "lease-expiry"}
	for i := range leases {
		lease := &leases[i]
		config, err := s.Store.GetDynamicSecretConfigByID(ctx, lease.ConfigID)
		if err != nil {
			continue
		}
		project, err := s.Store.GetProjectByID(ctx, config.ProjectID)
		if err != nil {
			continue
		}
		org, err := s.Store.GetOrgByID(ctx, project.OrgID)
		if err != nil {
			continue
		}
		if err := s.revokeLease(ctx, org, project, lease.ID, actor, AuditMeta{}); err != nil {
			s.Logger.Warn("revoke expired lease failed", "lease_id", lease.ID, "error", err)
		}
	}
}

// RevokeLeasesForIdentity revokes all live leases owned by an identity.
func (s *Service) RevokeLeasesForIdentity(ctx context.Context, identityID string) {
	leases, err := s.Store.ListLeasesByIdentity(ctx, identityID)
	if err != nil {
		return
	}
	actor := audit.Actor{ID: "identity-revoke", Kind: string(authz.SubjectMachine), Name: "identity-revoke"}
	for i := range leases {
		lease := &leases[i]
		config, err := s.Store.GetDynamicSecretConfigByID(ctx, lease.ConfigID)
		if err != nil {
			continue
		}
		project, err := s.Store.GetProjectByID(ctx, config.ProjectID)
		if err != nil {
			continue
		}
		org, err := s.Store.GetOrgByID(ctx, project.OrgID)
		if err != nil {
			continue
		}
		_ = s.revokeLease(ctx, org, project, lease.ID, actor, AuditMeta{})
	}
}

// DeleteDynamicConfig revokes live leases and deletes a config.
func (s *Service) DeleteDynamicConfig(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, configID string, meta AuditMeta) error {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.ProjectsManage)
	if err != nil {
		return err
	}
	leases, err := s.Store.ListLeases(ctx, configID, false)
	if err != nil {
		return err
	}
	for i := range leases {
		_ = s.revokeLease(ctx, org, project, leases[i].ID, toAuditActor(actor), meta)
	}
	if err := s.Store.DeleteDynamicSecretConfig(ctx, project.ID, configID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	return s.record(ctx, toAuditActor(actor), org.ID, "dynamic.config.delete",
		audit.Resource{Type: "project", ID: project.ID, Name: project.Slug, ProjectID: project.ID},
		audit.Details{Count: 1}, meta)
}

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

// A connection is an org-level provider credential (token, GitHub App, AWS
// keys, or GCP service account) plus the provider's base URL. Syncs that
// reference a connection carry no credential of their own: at run time the
// connection's ciphertext and base URL are overlaid on the sync config, and
// decryption is bound to the connection ID instead of the sync ID. Rotating a
// connection therefore updates every sync that uses it.
//
// Plain HTTP syncs are not supported: their signing secret belongs to one
// receiver, so there is nothing to share.

// connectionDefaultURLs lists the providers that accept connections and the
// base URL used when none is given ("" means no default).
var connectionDefaultURLs = map[string]string{
	"github":     "https://api.github.com",
	"gitlab":     "https://gitlab.com",
	"forgejo":    forgejoDefaultURL,
	"vercel":     "https://api.vercel.com",
	"netlify":    "https://api.netlify.com",
	"cloudflare": "https://api.cloudflare.com",
	"vault":      "",
	"aws":        "",
	"gcp":        "",
}

// ConnectionProviders returns the providers that support connections, sorted.
func ConnectionProviders() []string {
	out := make([]string, 0, len(connectionDefaultURLs))
	for provider := range connectionDefaultURLs {
		out = append(out, provider)
	}
	sort.Strings(out)
	return out
}

// ConnectionInput creates a connection or, on update, renames it, moves its
// base URL, or rotates its credential. On update empty fields are unchanged,
// and credential fields replace the whole credential when any is set.
type ConnectionInput struct {
	Provider           string
	Name               string
	BaseURL            string
	Token              string
	GitHubAppID        string
	GitHubInstallation string
	GitHubPrivateKey   string
	AWSAccessKey       string
	AWSSecretKey       string
	AWSSessionToken    string
	GCPServiceAccount  string
}

func (in ConnectionInput) hasCredential() bool {
	return in.Token != "" || in.GitHubAppID != "" || in.GitHubInstallation != "" ||
		in.GitHubPrivateKey != "" || in.AWSAccessKey != "" || in.AWSSecretKey != "" ||
		in.AWSSessionToken != "" || in.GCPServiceAccount != ""
}

// ConnectionView is a connection without its credential, plus where it is used.
type ConnectionView struct {
	Connection *store.SyncConnection
	// Auth names the credential kind: token, github_app, aws_keys, or
	// service_account.
	Auth string
	// Usage lists the syncs that use the connection. It is nil when the caller
	// cannot manage syncs org-wide; UsageCount is always set.
	Usage      []store.SyncConnectionUsage
	UsageCount int
}

// ConnectionTest is the result of a cheap authenticated provider call.
type ConnectionTest struct {
	OK       bool
	Detail   string
	Identity string
	// Checks lists each probe for providers that check more than the
	// credential (Forgejo checks every repository or org its syncs push to).
	Checks []ConnectionCheck
}

// ConnectionCheck is one probe in a connection test.
type ConnectionCheck struct {
	Target string
	OK     bool
	Detail string
}

// ConnectionTestTarget is an extra destination to probe, typed by the user
// before a sync exists.
type ConnectionTestTarget struct {
	ForgejoScope string
	ForgejoRepo  string
}

// ConnectionInUseError refuses to delete a connection that syncs still use.
type ConnectionInUseError struct {
	Usage []store.SyncConnectionUsage
}

func (e *ConnectionInUseError) Error() string {
	return fmt.Sprintf("service: connection is used by %d sync(s)", len(e.Usage))
}

// Is lets callers treat the error as a conflict.
func (e *ConnectionInUseError) Is(target error) bool { return target == ErrConflict }

func syncCredentialOwner(sync *store.Sync) string {
	if sync.ConnectionID != "" {
		return sync.ConnectionID
	}
	return sync.ID
}

func connectionAuditReason(conn *store.SyncConnection) string {
	if conn == nil {
		return ""
	}
	return "connection=" + conn.Name
}

// credentialCiphertexts lists the encrypted credential fields with the AAD
// field name each one is bound to.
func credentialCiphertexts(c *syncConfig) []struct {
	field string
	value *[]byte
} {
	return []struct {
		field string
		value *[]byte
	}{
		{"sync_token", &c.Token},
		{"sync_app_key", &c.GitHubPrivateKey},
		{"aws_secret", &c.AWSSecret},
		{"aws_session_token", &c.AWSSessionTokenEnc},
		{"gcp_service_account", &c.GCPServiceAccount},
	}
}

// copySyncCredentials replaces dst's credential fields with src's.
func copySyncCredentials(dst *syncConfig, src syncConfig) {
	dst.Token = src.Token
	dst.GitHubAppID = src.GitHubAppID
	dst.GitHubInstallation = src.GitHubInstallation
	dst.GitHubPrivateKey = src.GitHubPrivateKey
	dst.AWSAccessKey = src.AWSAccessKey
	dst.AWSSecret = src.AWSSecret
	dst.AWSSessionToken = src.AWSSessionToken
	dst.AWSSessionTokenEnc = src.AWSSessionTokenEnc
	dst.GCPServiceAccount = src.GCPServiceAccount
}

func connectionAuth(provider string, c syncConfig) string {
	switch {
	case provider == "aws":
		return "aws_keys"
	case provider == "gcp":
		return "service_account"
	case provider == "github" && len(c.Token) == 0 && c.GitHubAppID != "":
		return "github_app"
	default:
		return "token"
	}
}

func normalizeConnectionBaseURL(provider, raw string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if provider == "forgejo" {
		base = strings.TrimSuffix(base, "/api/v1")
	}
	if base == "" {
		base = connectionDefaultURLs[provider]
	}
	if provider == "vault" && base == "" {
		return "", fmt.Errorf("%w: Vault connections need the Vault address", ErrValidation)
	}
	if base != "" && !strings.HasPrefix(base, "https://") && !strings.HasPrefix(base, "http://") {
		return "", fmt.Errorf("%w: base URL must be http(s)", ErrValidation)
	}
	return base, nil
}

// buildConnectionCredential validates and encrypts a credential for the
// provider, binding each ciphertext to the connection ID.
func (s *Service) buildConnectionCredential(org *store.Org, id, provider string, in ConnectionInput) (syncConfig, error) {
	var config syncConfig
	encrypt := func(field, value string) ([]byte, error) {
		if value == "" {
			return nil, nil
		}
		return s.encryptOrgSecret(org, id, field, value)
	}
	var err error
	switch provider {
	case "aws":
		config.AWSAccessKey = strings.TrimSpace(in.AWSAccessKey)
		if config.AWSAccessKey == "" || in.AWSSecretKey == "" {
			return config, fmt.Errorf("%w: AWS connections need an access key ID and secret access key", ErrValidation)
		}
		if config.AWSSecret, err = encrypt("aws_secret", in.AWSSecretKey); err != nil {
			return config, err
		}
		if config.AWSSessionTokenEnc, err = encrypt("aws_session_token", strings.TrimSpace(in.AWSSessionToken)); err != nil {
			return config, err
		}
	case "gcp":
		var account gcpServiceAccount
		if err := json.Unmarshal([]byte(in.GCPServiceAccount), &account); err != nil || account.ClientEmail == "" || account.PrivateKey == "" {
			return config, fmt.Errorf("%w: GCP connections need service account JSON with client_email and private_key", ErrValidation)
		}
		if config.GCPServiceAccount, err = encrypt("gcp_service_account", in.GCPServiceAccount); err != nil {
			return config, err
		}
	case "github":
		config.GitHubAppID = strings.TrimSpace(in.GitHubAppID)
		config.GitHubInstallation = strings.TrimSpace(in.GitHubInstallation)
		app := config.GitHubAppID != "" || config.GitHubInstallation != "" || in.GitHubPrivateKey != ""
		if in.Token == "" && (config.GitHubAppID == "" || config.GitHubInstallation == "" || strings.TrimSpace(in.GitHubPrivateKey) == "") {
			return config, fmt.Errorf("%w: GitHub connections need a token or App ID, installation ID, and private key", ErrValidation)
		}
		if in.Token != "" && app {
			return config, fmt.Errorf("%w: give either a GitHub token or GitHub App details, not both", ErrValidation)
		}
		if config.Token, err = encrypt("sync_token", in.Token); err != nil {
			return config, err
		}
		if config.GitHubPrivateKey, err = encrypt("sync_app_key", in.GitHubPrivateKey); err != nil {
			return config, err
		}
	default:
		if strings.TrimSpace(in.Token) == "" {
			return config, fmt.Errorf("%w: an access token is required", ErrValidation)
		}
		if config.Token, err = encrypt("sync_token", strings.TrimSpace(in.Token)); err != nil {
			return config, err
		}
	}
	return config, nil
}

func (s *Service) connectionOrg(ctx context.Context, actor authz.Actor, orgSlug string, perm authz.Permission) (*store.Org, error) {
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := s.Authorize(actor, perm, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, err
	}
	return org, nil
}

func (s *Service) connectionView(conn *store.SyncConnection, usage []store.SyncConnectionUsage, showUsage bool) ConnectionView {
	var config syncConfig
	_ = json.Unmarshal([]byte(conn.Config), &config)
	view := ConnectionView{Connection: conn, Auth: connectionAuth(conn.Provider, config), UsageCount: len(usage)}
	if showUsage {
		view.Usage = usage
		if view.Usage == nil {
			view.Usage = []store.SyncConnectionUsage{}
		}
	}
	return view
}

// ListSyncConnections returns the org's connections, optionally for one
// provider. Anyone who can read the org sees names and providers (to pick one
// for a sync); the list of syncs using each is shown only to org-wide sync
// managers.
func (s *Service) ListSyncConnections(ctx context.Context, actor authz.Actor, orgSlug, provider string) ([]ConnectionView, error) {
	org, err := s.connectionOrg(ctx, actor, orgSlug, authz.OrgRead)
	if err != nil {
		return nil, err
	}
	showUsage := s.Authorize(actor, authz.SyncsManage, authz.Scope{OrgID: org.ID}) == nil
	conns, err := s.Store.ListSyncConnections(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	usage, err := s.Store.ListSyncConnectionUsage(ctx, org.ID, "")
	if err != nil {
		return nil, err
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	out := make([]ConnectionView, 0, len(conns))
	for i := range conns {
		if provider != "" && conns[i].Provider != provider {
			continue
		}
		out = append(out, s.connectionView(&conns[i], usage[conns[i].ID], showUsage))
	}
	return out, nil
}

// CreateSyncConnection stores a new connection.
func (s *Service) CreateSyncConnection(ctx context.Context, actor authz.Actor, orgSlug string, in ConnectionInput, meta AuditMeta) (*ConnectionView, error) {
	org, err := s.connectionOrg(ctx, actor, orgSlug, authz.SyncsManage)
	if err != nil {
		return nil, err
	}
	provider := strings.ToLower(strings.TrimSpace(in.Provider))
	if _, ok := connectionDefaultURLs[provider]; !ok {
		return nil, fmt.Errorf("%w: connections support %s", ErrValidation, strings.Join(ConnectionProviders(), ", "))
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: connection name is required", ErrValidation)
	}
	base, err := normalizeConnectionBaseURL(provider, in.BaseURL)
	if err != nil {
		return nil, err
	}
	id := uuid.NewString()
	credential, err := s.buildConnectionCredential(org, id, provider, in)
	if err != nil {
		return nil, err
	}
	conn := &store.SyncConnection{ID: id, OrgID: org.ID, Provider: provider, Name: name, BaseURL: base, CreatedBy: actor.ID}
	if err := s.saveConnection(ctx, conn, credential, true); err != nil {
		return nil, err
	}
	s.recordConnection(ctx, actor, org, "sync_connection.create", conn, "", meta)
	view := s.connectionView(conn, nil, true)
	return &view, nil
}

func (s *Service) saveConnection(ctx context.Context, conn *store.SyncConnection, credential syncConfig, create bool) error {
	encoded, err := json.Marshal(credential) //nolint:gosec // G117: credential fields hold org-key ciphertext
	if err != nil {
		return err
	}
	conn.Config = string(encoded)
	if create {
		err = s.Store.CreateSyncConnection(ctx, conn)
	} else {
		err = s.Store.UpdateSyncConnection(ctx, conn)
	}
	if errors.Is(err, store.ErrConflict) {
		return fmt.Errorf("%w: a connection named %q already exists", ErrValidation, conn.Name)
	}
	if errors.Is(err, store.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

func (s *Service) recordConnection(ctx context.Context, actor authz.Actor, org *store.Org, action string, conn *store.SyncConnection, reason string, meta AuditMeta) {
	if err := s.record(ctx, toAuditActor(actor), org.ID, action,
		audit.Resource{Type: "sync_connection", ID: conn.ID, Name: conn.Name},
		audit.Details{Reason: strings.TrimSpace(conn.Provider + " " + reason)}, meta); err != nil {
		s.Logger.Error("audit "+action+" failed", "connection_id", conn.ID, "error", err)
	}
}

// UpdateSyncConnection renames a connection, changes its base URL, or rotates
// its credential. Rotation is audited separately from metadata edits.
func (s *Service) UpdateSyncConnection(ctx context.Context, actor authz.Actor, orgSlug, id string, in ConnectionInput, meta AuditMeta) (*ConnectionView, error) {
	org, err := s.connectionOrg(ctx, actor, orgSlug, authz.SyncsManage)
	if err != nil {
		return nil, err
	}
	conn, err := s.Store.GetSyncConnection(ctx, org.ID, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if provider := strings.ToLower(strings.TrimSpace(in.Provider)); provider != "" && provider != conn.Provider {
		return nil, fmt.Errorf("%w: a connection's provider cannot change", ErrValidation)
	}
	var credential syncConfig
	if err := json.Unmarshal([]byte(conn.Config), &credential); err != nil {
		return nil, err
	}
	var changes []string
	if name := strings.TrimSpace(in.Name); name != "" && name != conn.Name {
		conn.Name = name
		changes = append(changes, "name")
	}
	if strings.TrimSpace(in.BaseURL) != "" {
		base, err := normalizeConnectionBaseURL(conn.Provider, in.BaseURL)
		if err != nil {
			return nil, err
		}
		if base != conn.BaseURL {
			conn.BaseURL = base
			changes = append(changes, "base_url")
		}
	}
	rotated := in.hasCredential()
	if rotated {
		if credential, err = s.buildConnectionCredential(org, conn.ID, conn.Provider, in); err != nil {
			return nil, err
		}
	}
	if len(changes) == 0 && !rotated {
		view := s.connectionView(conn, nil, true)
		return &view, nil
	}
	if err := s.saveConnection(ctx, conn, credential, false); err != nil {
		return nil, err
	}
	if rotated {
		s.recordConnection(ctx, actor, org, "sync_connection.rotate", conn, "", meta)
	}
	if len(changes) > 0 {
		s.recordConnection(ctx, actor, org, "sync_connection.update", conn, "changed="+strings.Join(changes, ","), meta)
	}
	usage, err := s.Store.ListSyncConnectionUsage(ctx, org.ID, conn.ID)
	if err != nil {
		return nil, err
	}
	view := s.connectionView(conn, usage[conn.ID], true)
	return &view, nil
}

// DeleteSyncConnection removes an unused connection. A connection that syncs
// still use returns *ConnectionInUseError listing them.
func (s *Service) DeleteSyncConnection(ctx context.Context, actor authz.Actor, orgSlug, id string, meta AuditMeta) error {
	org, err := s.connectionOrg(ctx, actor, orgSlug, authz.SyncsManage)
	if err != nil {
		return err
	}
	conn, err := s.Store.GetSyncConnection(ctx, org.ID, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	usage, err := s.Store.ListSyncConnectionUsage(ctx, org.ID, conn.ID)
	if err != nil {
		return err
	}
	if inUse := usage[conn.ID]; len(inUse) > 0 {
		return &ConnectionInUseError{Usage: inUse}
	}
	if err := s.Store.DeleteSyncConnection(ctx, org.ID, conn.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	s.recordConnection(ctx, actor, org, "sync_connection.delete", conn, "", meta)
	return nil
}

// connectionForSync loads the connection a new sync will use and checks that
// it matches the provider and that no inline credential was also given.
func (s *Service) connectionForSync(ctx context.Context, org *store.Org, provider, id string, in SyncInput) (*store.SyncConnection, error) {
	if provider == "http" {
		return nil, fmt.Errorf("%w: HTTP syncs cannot use a connection", ErrValidation)
	}
	if in.Token != "" || in.Secret != "" || in.GitHubAppID != "" || in.GitHubInstallation != "" ||
		in.GitHubPrivateKey != "" || in.AWSAccessKey != "" || in.AWSSecretKey != "" ||
		in.AWSSessionToken != "" || in.GCPServiceAccount != "" {
		return nil, fmt.Errorf("%w: give either a connection or an inline credential, not both", ErrValidation)
	}
	conn, err := s.Store.GetSyncConnection(ctx, org.ID, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("%w: connection %q not found in this organization", ErrValidation, id)
		}
		return nil, err
	}
	if provider != "" && provider != conn.Provider {
		return nil, fmt.Errorf("%w: connection %q is for %s, not %s", ErrValidation, conn.Name, conn.Provider, provider)
	}
	if base := strings.TrimRight(strings.TrimSpace(in.BaseURL), "/"); base != "" {
		normalized, err := normalizeConnectionBaseURL(conn.Provider, base)
		if err != nil || normalized != conn.BaseURL {
			return nil, fmt.Errorf("%w: the base URL comes from connection %q (%s)", ErrValidation, conn.Name, conn.BaseURL)
		}
	}
	return conn, nil
}

// applySyncConnection overlays the sync's connection credential and base URL
// on config. Syncs without a connection are unchanged.
func (s *Service) applySyncConnection(ctx context.Context, org *store.Org, sync *store.Sync, config *syncConfig) error {
	if sync.ConnectionID == "" {
		return nil
	}
	conn, err := s.Store.GetSyncConnection(ctx, org.ID, sync.ConnectionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("%w: this sync's connection no longer exists; attach another connection", ErrValidation)
		}
		return err
	}
	var credential syncConfig
	if err := json.Unmarshal([]byte(conn.Config), &credential); err != nil {
		return err
	}
	copySyncCredentials(config, credential)
	if conn.BaseURL != "" {
		config.BaseURL = conn.BaseURL
	}
	return nil
}

// UseSyncConnection moves an existing sync onto a connection, dropping its
// inline credential. With connectionID empty, a new connection named name is
// created from the sync's own credential first; that needs org-wide
// syncs:manage. This is the migration path for syncs created before
// connections existed.
func (s *Service) UseSyncConnection(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, syncID, connectionID, name string, meta AuditMeta) (*SyncView, *ConnectionView, error) {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.SyncsManage)
	if err != nil {
		return nil, nil, err
	}
	sync, err := s.Store.GetSync(ctx, project.ID, syncID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	if _, ok := connectionDefaultURLs[sync.Provider]; !ok {
		return nil, nil, fmt.Errorf("%w: %s syncs cannot use a connection", ErrValidation, sync.Provider)
	}
	var config syncConfig
	if err := json.Unmarshal([]byte(sync.Config), &config); err != nil {
		return nil, nil, err
	}
	var conn *store.SyncConnection
	created := false
	if connectionID = strings.TrimSpace(connectionID); connectionID != "" {
		conn, err = s.Store.GetSyncConnection(ctx, org.ID, connectionID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, nil, ErrNotFound
			}
			return nil, nil, err
		}
		if conn.Provider != sync.Provider {
			return nil, nil, fmt.Errorf("%w: connection %q is for %s, not %s", ErrValidation, conn.Name, conn.Provider, sync.Provider)
		}
	} else {
		if sync.ConnectionID != "" {
			return nil, nil, fmt.Errorf("%w: this sync already uses a connection", ErrValidation)
		}
		if name = strings.TrimSpace(name); name == "" {
			return nil, nil, fmt.Errorf("%w: give a connection ID, or a name for a new connection", ErrValidation)
		}
		if err := s.Authorize(actor, authz.SyncsManage, authz.Scope{OrgID: org.ID}); err != nil {
			return nil, nil, err
		}
		if conn, err = s.connectionFromSync(ctx, actor, org, sync, config, name); err != nil {
			return nil, nil, err
		}
		created = true
	}
	copySyncCredentials(&config, syncConfig{})
	if conn.BaseURL != "" {
		config.BaseURL = conn.BaseURL
	}
	encoded, err := json.Marshal(config) //nolint:gosec // G117: no credential remains; other secret fields hold ciphertext
	if err != nil {
		return nil, nil, err
	}
	if err := s.Store.SetSyncConnection(ctx, project.ID, sync.ID, conn.ID, string(encoded)); err != nil {
		return nil, nil, err
	}
	sync.ConnectionID = conn.ID
	sync.Config = string(encoded)
	if created {
		s.recordConnection(ctx, actor, org, "sync_connection.create", conn, "from_sync="+sync.ID, meta)
	}
	envSlug := ""
	if env, err := s.Store.GetEnvironmentByID(ctx, sync.EnvironmentID); err == nil {
		envSlug = env.Slug
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "sync.connection",
		audit.Resource{Type: "sync", ID: sync.ID, Name: sync.Provider, ProjectID: project.ID, EnvID: sync.EnvironmentID},
		audit.Details{Environment: envSlug, Reason: connectionAuditReason(conn)}, meta); err != nil {
		s.Logger.Error("audit sync.connection failed", "sync_id", sync.ID, "error", err)
	}
	view := s.connectionView(conn, nil, true)
	return &SyncView{Sync: sync, Environment: envSlug}, &view, nil
}

// connectionFromSync copies a sync's inline credential into a new
// connection, re-encrypting each field under the connection ID.
func (s *Service) connectionFromSync(ctx context.Context, actor authz.Actor, org *store.Org, sync *store.Sync, config syncConfig, name string) (*store.SyncConnection, error) {
	id := uuid.NewString()
	var credential syncConfig
	copySyncCredentials(&credential, config)
	if credential.AWSSessionToken != "" && len(credential.AWSSessionTokenEnc) == 0 {
		// Legacy plaintext session token: encrypt it on the way over.
		ciphertext, err := s.encryptOrgSecret(org, sync.ID, "aws_session_token", credential.AWSSessionToken)
		if err != nil {
			return nil, err
		}
		credential.AWSSessionTokenEnc = ciphertext
	}
	credential.AWSSessionToken = ""
	empty := true
	for _, entry := range credentialCiphertexts(&credential) {
		if len(*entry.value) == 0 {
			continue
		}
		plaintext, err := s.decryptOrgSecret(org, sync.ID, entry.field, *entry.value)
		if err != nil {
			return nil, err
		}
		if *entry.value, err = s.encryptOrgSecret(org, id, entry.field, plaintext); err != nil {
			return nil, err
		}
		empty = false
	}
	if empty {
		return nil, fmt.Errorf("%w: this sync has no stored credential to move", ErrValidation)
	}
	base, err := normalizeConnectionBaseURL(sync.Provider, config.BaseURL)
	if err != nil {
		return nil, err
	}
	conn := &store.SyncConnection{ID: id, OrgID: org.ID, Provider: sync.Provider, Name: name, BaseURL: base, CreatedBy: actor.ID}
	if err := s.saveConnection(ctx, conn, credential, true); err != nil {
		return nil, err
	}
	return conn, nil
}

// TestSyncConnection makes one cheap authenticated call to the provider. A
// rejected credential is a failed test, not an error.
func (s *Service) TestSyncConnection(ctx context.Context, actor authz.Actor, orgSlug, id string, target *ConnectionTestTarget) (*ConnectionTest, error) {
	org, err := s.connectionOrg(ctx, actor, orgSlug, authz.SyncsManage)
	if err != nil {
		return nil, err
	}
	conn, err := s.Store.GetSyncConnection(ctx, org.ID, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var config syncConfig
	if err := json.Unmarshal([]byte(conn.Config), &config); err != nil {
		return nil, err
	}
	config.BaseURL = conn.BaseURL
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if conn.Provider == "forgejo" {
		return s.testForgejoConnection(ctx, org, conn, config, target)
	}
	identity, err := s.probeConnection(ctx, org, conn, config)
	if err != nil {
		return &ConnectionTest{OK: false, Detail: err.Error()}, nil
	}
	detail := "Authenticated with " + conn.Provider
	if identity != "" {
		detail += " as " + identity
	}
	return &ConnectionTest{OK: true, Detail: detail, Identity: identity}, nil
}

func (s *Service) probeConnection(ctx context.Context, org *store.Org, conn *store.SyncConnection, config syncConfig) (string, error) {
	client := s.forgejoClient()
	base := strings.TrimRight(config.BaseURL, "/")
	owner := &store.Sync{ID: conn.ID, ConnectionID: conn.ID, Provider: conn.Provider}
	token, err := s.decryptOrgSecret(org, conn.ID, "sync_token", config.Token)
	if err != nil {
		return "", err
	}
	var identity struct {
		Login       string `json:"login"`
		Username    string `json:"username"`
		FullName    string `json:"full_name"`
		DisplayName string `json:"display_name"`
		User        struct {
			Username string `json:"username"`
		} `json:"user"`
		Data struct {
			DisplayName string `json:"display_name"`
		} `json:"data"`
		Result struct {
			Status string `json:"status"`
		} `json:"result"`
	}
	check := func(status int, body []byte, err error) (string, error) {
		if err != nil {
			return "", fmt.Errorf("%s: could not reach %s: %w; check the URL and network policy", conn.Provider, base, err)
		}
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			return "", fmt.Errorf("%s: credential rejected (HTTP %d); check that it is valid, not expired, and has the needed scopes", conn.Provider, status)
		}
		if status < 200 || status >= 300 {
			detail := strings.TrimSpace(string(body))
			if len(detail) > 200 {
				detail = detail[:200]
			}
			return "", fmt.Errorf("%s: test call returned HTTP %d: %s", conn.Provider, status, detail)
		}
		_ = json.Unmarshal(body, &identity)
		for _, candidate := range []string{identity.Login, identity.Username, identity.User.Username,
			identity.FullName, identity.DisplayName, identity.Data.DisplayName} {
			if candidate != "" {
				return candidate, nil
			}
		}
		return "", nil
	}
	switch conn.Provider {
	case "forgejo":
		return check(forgejoRequest(ctx, client, http.MethodGet, base+"/api/v1/user", token, nil))
	case "gitlab":
		return check(headerRequest(ctx, client, base+"/api/v4/user", "PRIVATE-TOKEN", token))
	case "vault":
		return check(headerRequest(ctx, client, base+"/v1/auth/token/lookup-self", "X-Vault-Token", token))
	case "vercel":
		return check(githubLikeRequest(ctx, client, http.MethodGet, base+"/v2/user", token, nil))
	case "netlify":
		return check(githubLikeRequest(ctx, client, http.MethodGet, base+"/api/v1/user", token, nil))
	case "cloudflare":
		if _, err := check(githubLikeRequest(ctx, client, http.MethodGet, base+"/client/v4/user/tokens/verify", token, nil)); err != nil {
			return "", err
		}
		if identity.Result.Status != "" && identity.Result.Status != "active" {
			return "", fmt.Errorf("cloudflare: token status is %q", identity.Result.Status)
		}
		return "", nil
	case "github":
		if token == "" {
			// A minted installation token proves the App ID, installation, and key.
			if _, err := s.githubSyncToken(ctx, org, owner, config); err != nil {
				return "", err
			}
			return "GitHub App installation " + config.GitHubInstallation, nil
		}
		return check(githubRequest(ctx, client, http.MethodGet, base+"/user", token, nil))
	case "gcp":
		raw, err := s.decryptOrgSecret(org, conn.ID, "gcp_service_account", config.GCPServiceAccount)
		if err != nil {
			return "", err
		}
		var account gcpServiceAccount
		if err := json.Unmarshal([]byte(raw), &account); err != nil || account.ClientEmail == "" || account.PrivateKey == "" {
			return "", fmt.Errorf("gcp: service account JSON must include client_email and private_key")
		}
		if account.TokenURI == "" {
			account.TokenURI = "https://oauth2.googleapis.com/token"
		}
		if _, err := gcpAccessToken(ctx, client, account); err != nil {
			return "", err
		}
		return account.ClientEmail, nil
	case "aws":
		return s.probeAWS(ctx, client, org, conn, config)
	default:
		return "", fmt.Errorf("%s connections cannot be tested", conn.Provider)
	}
}

func headerRequest(ctx context.Context, client *http.Client, endpoint, header, token string) (int, []byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set(header, token)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "clavis-sync/1")
	response, err := client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	return response.StatusCode, body, nil
}

// probeAWS calls STS GetCallerIdentity, which needs no IAM permission.
func (s *Service) probeAWS(ctx context.Context, client *http.Client, org *store.Org, conn *store.SyncConnection, config syncConfig) (string, error) {
	secretKey, err := s.decryptOrgSecret(org, conn.ID, "aws_secret", config.AWSSecret)
	if err != nil {
		return "", err
	}
	if config.AWSSessionToken, err = s.decryptOrgSecret(org, conn.ID, "aws_session_token", config.AWSSessionTokenEnc); err != nil {
		return "", err
	}
	endpoint := config.BaseURL
	if endpoint == "" {
		endpoint = "https://sts.amazonaws.com"
	}
	payload := []byte("Action=GetCallerIdentity&Version=2011-06-15")
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(endpoint, "/")+"/", strings.NewReader(string(payload)))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	signAWS(request, payload, "sts", "us-east-1", config, secretKey)
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("aws: could not reach STS: %w; check the endpoint and network policy", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("aws: credentials rejected; check the access key and secret")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("aws: GetCallerIdentity returned HTTP %d", response.StatusCode)
	}
	text := string(body)
	if start, end := strings.Index(text, "<Arn>"), strings.Index(text, "</Arn>"); start >= 0 && end > start {
		return text[start+len("<Arn>") : end], nil
	}
	return "", nil
}

package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authn"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/store"
)

var (
	// ErrSSORequired means an org requires SSO for password sign-in.
	ErrSSORequired = errors.New("service: single sign-on is required")
	// ErrSSOUnavailable means SSO is not configured or the flow failed.
	ErrSSOUnavailable = errors.New("service: single sign-on is unavailable")
)

type ssoState struct {
	OrgID      string
	OrgSlug    string
	Verifier   string
	Nonce      string
	ReturnPath string
	Expires    time.Time
}

type ssoStateStore struct {
	mu    sync.Mutex
	now   func() time.Time
	byKey map[string]ssoState
}

func newSSOStateStore(now func() time.Time) *ssoStateStore {
	return &ssoStateStore{now: now, byKey: make(map[string]ssoState)}
}

func (s *ssoStateStore) put(state ssoState) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	key := base64.RawURLEncoding.EncodeToString(raw)
	s.mu.Lock()
	defer s.mu.Unlock()
	for existing, entry := range s.byKey {
		if s.now().UTC().After(entry.Expires) {
			delete(s.byKey, existing)
		}
	}
	state.Expires = s.now().UTC().Add(10 * time.Minute)
	s.byKey[key] = state
	return key, nil
}

func (s *ssoStateStore) take(key string) (ssoState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.byKey[key]
	if !ok {
		return ssoState{}, false
	}
	delete(s.byKey, key)
	if s.now().UTC().After(state.Expires) {
		return ssoState{}, false
	}
	return state, true
}

// SSOConfig is the effective, validated SSO configuration for an org.
type SSOConfig struct {
	Org      *store.Org
	Settings OrgSettings
	Secret   string
	Issuer   string
}

func randomURLToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *Service) encryptSSOSecret(org *store.Org, secret string) ([]byte, error) {
	orgKey, err := s.Keyring.UnwrapOrgKey(org.ID, org.WrappedOrgKey)
	if err != nil {
		return nil, err
	}
	defer orgKey.Zero()
	aad := crypto.BindAAD(org.ID, "", "", "sso", "client_secret", 1)
	ciphertext, err := orgKey.Encrypt(keyVersion(org.KeyVersion), crypto.Secret(secret), aad)
	if err != nil {
		return nil, err
	}
	return ciphertext, nil
}

func (s *Service) decryptSSOSecret(org *store.Org, ciphertext []byte) (string, error) {
	if len(ciphertext) == 0 {
		return "", nil
	}
	orgKey, err := s.Keyring.UnwrapOrgKey(org.ID, org.WrappedOrgKey)
	if err != nil {
		return "", err
	}
	defer orgKey.Zero()
	aad := crypto.BindAAD(org.ID, "", "", "sso", "client_secret", 1)
	plaintext, _, err := orgKey.Decrypt(ciphertext, aad)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// SSOConfigFor loads and validates an org's SSO configuration.
func (s *Service) SSOConfigFor(ctx context.Context, orgSlug string) (*SSOConfig, error) {
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	settings := ParseOrgSettings(org.Settings)
	if !settings.SSOEnabled || settings.SSOClientID == "" {
		return nil, ErrSSOUnavailable
	}
	secret, err := s.decryptSSOSecret(org, settings.SSOClientSecret)
	if err != nil {
		return nil, err
	}
	config := &SSOConfig{Org: org, Settings: settings, Secret: secret, Issuer: settings.SSOIssuer}
	if config.Issuer == "" {
		switch settings.SSOProvider {
		case "google":
			config.Issuer = "https://accounts.google.com"
		case "microsoft":
			config.Issuer = "https://login.microsoftonline.com/common/v2.0"
		}
	}
	return config, nil
}

// BeginSSO creates a provider authorization URL and state token.
func (s *Service) BeginSSO(ctx context.Context, orgSlug, redirectURI, returnPath string) (string, error) {
	config, err := s.SSOConfigFor(ctx, orgSlug)
	if err != nil {
		return "", err
	}
	verifier, err := randomURLToken()
	if err != nil {
		return "", err
	}
	nonce, err := randomURLToken()
	if err != nil {
		return "", err
	}
	returnPath = SafeReturnPath(returnPath)
	state, err := s.ssoStates.put(ssoState{
		OrgID: config.Org.ID, OrgSlug: config.Org.Slug, Verifier: verifier, Nonce: nonce, ReturnPath: returnPath,
	})
	if err != nil {
		return "", err
	}
	if config.Settings.SSOProvider == "github" {
		oauthConfig := githubOAuthConfig(config.Settings.SSOClientID, config.Secret, redirectURI)
		return oauthConfig.AuthCodeURL(state,
			oauth2.S256ChallengeOption(verifier),
			oauth2.SetAuthURLParam("allow_signup", "true")), nil
	}
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, s.ssoHTTPClient()), config.Issuer)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrSSOUnavailable, err)
	}
	oauthConfig := &oauth2.Config{
		ClientID: config.Settings.SSOClientID, ClientSecret: config.Secret,
		Endpoint: provider.Endpoint(), RedirectURL: redirectURI,
		Scopes: []string{oidc.ScopeOpenID, "email", "profile"},
	}
	return oauthConfig.AuthCodeURL(state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("nonce", nonce)), nil
}

// FinishSSO completes a provider callback and provisions the user.
func (s *Service) FinishSSO(ctx context.Context, stateToken, code, redirectURI string, meta AuditMeta) (*store.User, *store.Org, string, error) {
	state, ok := s.ssoStates.take(stateToken)
	if !ok || code == "" {
		return nil, nil, "", ErrSSOUnavailable
	}
	org, err := s.Store.GetOrgByID(ctx, state.OrgID)
	if err != nil {
		return nil, nil, "", ErrSSOUnavailable
	}
	settings := ParseOrgSettings(org.Settings)
	if !settings.SSOEnabled {
		return nil, nil, "", ErrSSOUnavailable
	}
	secret, err := s.decryptSSOSecret(org, settings.SSOClientSecret)
	if err != nil {
		return nil, nil, "", err
	}
	var email, name string
	verified := false
	if settings.SSOProvider == "github" {
		email, name, err = s.githubIdentity(ctx, settings.SSOClientID, secret, redirectURI, code, state.Verifier)
		verified = err == nil
	} else {
		email, name, verified, err = s.oidcIdentity(ctx, settings, secret, redirectURI, code, state.Verifier, state.Nonce)
	}
	if err != nil {
		return nil, nil, "", err
	}
	if !verified {
		return nil, nil, "", fmt.Errorf("%w: provider did not verify the email address", ErrSSOUnavailable)
	}
	normalized, err := authn.NormalizeEmail(email)
	if err != nil {
		return nil, nil, "", fmt.Errorf("%w: provider returned an invalid email", ErrSSOUnavailable)
	}
	if !ssoEmailAllowed(normalized, settings.SSOAllowedDomains) {
		return nil, nil, "", fmt.Errorf("%w: email domain is not allowed for this organization", ErrForbidden)
	}
	user, err := s.provisionSSOUser(ctx, org, settings, normalized, name, settings.SSOProvider, meta)
	if err != nil {
		return nil, nil, "", err
	}
	return user, org, state.ReturnPath, nil
}

func (s *Service) provisionSSOUser(ctx context.Context, org *store.Org, settings OrgSettings, email, name, provider string, meta AuditMeta) (*store.User, error) {
	user, err := s.Store.GetUserByEmail(ctx, email)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		name = strings.TrimSpace(name)
		if name == "" {
			name = email
		}
		user = &store.User{
			ID: uuid.NewString(), Email: email, Name: name, EmailVerified: true,
			Preferences: `{"theme":"system","appearance":"classic"}`,
		}
		if err := s.Store.CreateUser(ctx, user); err != nil {
			if errors.Is(err, store.ErrConflict) {
				user, err = s.Store.GetUserByEmail(ctx, email)
				if err != nil {
					return nil, err
				}
			} else {
				return nil, err
			}
		}
	}
	roleName := settings.SSORole
	if roleName == "" {
		roleName = authz.RoleDeveloper
	}
	role, err := s.Store.GetRoleByName(ctx, org.ID, roleName)
	if err != nil {
		return nil, fmt.Errorf("%w: SSO role is not configured", ErrSSOUnavailable)
	}
	membership, err := s.Store.GetMembership(ctx, org.ID, user.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		if err := s.Store.CreateMembership(ctx, org.ID, user.ID, role.ID); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	case membership.RemovedAt != nil || membership.Status != "active":
		if err := s.Store.ActivateMembership(ctx, org.ID, user.ID, role.ID); err != nil {
			return nil, err
		}
	}
	if err := s.record(ctx, toAuditActor(authz.Actor{ID: user.ID, Kind: authz.SubjectUser, Name: user.Name}),
		org.ID, "auth.sso_login", audit.Resource{Type: "user", ID: user.ID, Name: user.Email},
		audit.Details{Reason: provider}, meta); err != nil {
		s.Logger.Error("audit auth.sso_login failed", "user_id", user.ID, "error", err)
	}
	return user, nil
}

func (s *Service) oidcIdentity(ctx context.Context, settings OrgSettings, secret, redirectURI, code, verifier, nonce string) (string, string, bool, error) {
	issuer := settings.SSOIssuer
	if issuer == "" {
		switch settings.SSOProvider {
		case "google":
			issuer = "https://accounts.google.com"
		case "microsoft":
			issuer = "https://login.microsoftonline.com/common/v2.0"
		}
	}
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, s.ssoHTTPClient()), issuer)
	if err != nil {
		return "", "", false, fmt.Errorf("%w: %w", ErrSSOUnavailable, err)
	}
	oauthConfig := &oauth2.Config{
		ClientID: settings.SSOClientID, ClientSecret: secret,
		Endpoint: provider.Endpoint(), RedirectURL: redirectURI,
		Scopes: []string{oidc.ScopeOpenID, "email", "profile"},
	}
	token, err := oauthConfig.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", "", false, fmt.Errorf("%w: token exchange failed: %w", ErrSSOUnavailable, err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return "", "", false, fmt.Errorf("%w: provider returned no ID token", ErrSSOUnavailable)
	}
	verifierConfig := &oidc.Config{ClientID: settings.SSOClientID}
	if settings.SSOProvider == "microsoft" {
		verifierConfig.SkipIssuerCheck = true
	}
	idToken, err := provider.Verifier(verifierConfig).Verify(ctx, rawIDToken)
	if err != nil {
		return "", "", false, fmt.Errorf("%w: ID token verification failed", ErrSSOUnavailable)
	}
	if idToken.Nonce != "" && idToken.Nonce != nonce {
		return "", "", false, fmt.Errorf("%w: nonce mismatch", ErrSSOUnavailable)
	}
	var claims struct {
		Email             string `json:"email"`
		EmailVerified     bool   `json:"email_verified"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return "", "", false, fmt.Errorf("%w: missing claims", ErrSSOUnavailable)
	}
	email := claims.Email
	if email == "" {
		email = claims.PreferredUsername
	}
	name := claims.Name
	if name == "" {
		name = claims.PreferredUsername
	}
	return email, name, claims.EmailVerified, nil
}

func githubOAuthConfig(clientID, secret, redirectURI string) *oauth2.Config {
	return &oauth2.Config{
		ClientID: clientID, ClientSecret: secret, RedirectURL: redirectURI,
		Scopes: []string{"read:user", "user:email"},
		Endpoint: oauth2.Endpoint{ //nolint:gosec // G101: public GitHub OAuth endpoint URLs
			AuthURL:  "https://github.com/login/oauth/authorize",
			TokenURL: "https://github.com/login/oauth/access_token",
		},
	}
}

func (s *Service) githubIdentity(ctx context.Context, clientID, secret, redirectURI, code, verifier string) (string, string, error) {
	oauthConfig := githubOAuthConfig(clientID, secret, redirectURI)
	token, err := oauthConfig.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", "", fmt.Errorf("%w: GitHub token exchange failed", ErrSSOUnavailable)
	}
	client := oauth2.NewClient(oidc.ClientContext(ctx, s.ssoHTTPClient()), oauth2.StaticTokenSource(token))
	var profile struct {
		Login string `json:"login"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := githubGet(ctx, client, "https://api.github.com/user", &profile); err != nil {
		return "", "", err
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := githubGet(ctx, client, "https://api.github.com/user/emails", &emails); err != nil {
		return "", "", err
	}
	email := ""
	for _, candidate := range emails {
		if candidate.Verified && candidate.Primary {
			email = candidate.Email
			break
		}
	}
	if email == "" {
		for _, candidate := range emails {
			if candidate.Verified {
				email = candidate.Email
				break
			}
		}
	}
	if email == "" {
		return "", "", fmt.Errorf("%w: no verified GitHub email", ErrSSOUnavailable)
	}
	name := profile.Name
	if name == "" {
		name = profile.Login
	}
	return email, name, nil
}

func githubGet(ctx context.Context, client *http.Client, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: GitHub request failed", ErrSSOUnavailable)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: GitHub returned %d", ErrSSOUnavailable, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%w: invalid GitHub response", ErrSSOUnavailable)
	}
	return nil
}

func ssoEmailAllowed(email string, domains []string) bool {
	if len(domains) == 0 {
		return true
	}
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}
	domain := strings.ToLower(email[at+1:])
	for _, allowed := range domains {
		if domain == strings.ToLower(allowed) {
			return true
		}
	}
	return false
}

// CheckSSORequired rejects password sign-in when any active org enforces SSO.
func (s *Service) CheckSSORequired(ctx context.Context, user *store.User) error {
	memberships, err := s.Store.ListOrgsForUser(ctx, user.ID)
	if err != nil {
		return err
	}
	for _, membership := range memberships {
		settings := ParseOrgSettings(membership.Org.Settings)
		if settings.SSOEnabled && settings.SSOEnforce {
			return ErrSSORequired
		}
	}
	return nil
}

func (s *Service) ssoHTTPClient() *http.Client {
	if s.SSOClient != nil {
		return s.SSOClient
	}
	return http.DefaultClient
}

// SafeReturnPath returns p if it is a same-origin absolute path, otherwise
// "/". Browsers treat a backslash like a slash, so "/\evil.example" would be
// followed as a protocol-relative URL and is rejected along with "//".
func SafeReturnPath(p string) string {
	if p == "" || !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.HasPrefix(p, `/\`) {
		return "/"
	}
	return p
}

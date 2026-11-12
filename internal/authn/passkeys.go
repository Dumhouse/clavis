package authn

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/dumhouse/clavis/internal/store"
)

var (
	// ErrPasskeyUnsupported means no Relying Party is configured.
	ErrPasskeyUnsupported = errors.New("authn: passkeys are not configured")
	// ErrPasskeyInvalid covers failed registration or assertion ceremonies.
	ErrPasskeyInvalid = errors.New("authn: passkey ceremony failed")
)

// NewWebAuthn builds a Relying Party from the public server URL.
func NewWebAuthn(rpID, displayName string, origins []string) (*webauthn.WebAuthn, error) {
	if rpID == "" || len(origins) == 0 {
		return nil, ErrPasskeyUnsupported
	}
	if displayName == "" {
		displayName = "Clavis"
	}
	return webauthn.New(&webauthn.Config{
		RPID:          rpID,
		RPDisplayName: displayName,
		RPOrigins:     origins,
	})
}

type passkeyChallenge struct {
	session *webauthn.SessionData
	expires time.Time
}

type passkeyChallenges struct {
	mu    sync.Mutex
	now   func() time.Time
	byKey map[string]passkeyChallenge
}

func newPasskeyChallenges(now func() time.Time) *passkeyChallenges {
	return &passkeyChallenges{now: now, byKey: make(map[string]passkeyChallenge)}
}

func (p *passkeyChallenges) create(session *webauthn.SessionData) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pruneLocked()
	p.byKey[token] = passkeyChallenge{session: session, expires: p.now().UTC().Add(5 * time.Minute)}
	return token, nil
}

func (p *passkeyChallenges) get(token string) (*webauthn.SessionData, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.byKey[token]
	if !ok || p.now().UTC().After(entry.expires) {
		delete(p.byKey, token)
		return nil, false
	}
	return entry.session, true
}

func (p *passkeyChallenges) delete(token string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.byKey, token)
}

func (p *passkeyChallenges) pruneLocked() {
	now := p.now().UTC()
	for key, entry := range p.byKey {
		if now.After(entry.expires) {
			delete(p.byKey, key)
		}
	}
}

// passkeyUser adapts a stored user to webauthn.User.
type passkeyUser struct {
	user    *store.User
	records []store.Passkey
	creds   []webauthn.Credential
}

func (u *passkeyUser) WebAuthnID() []byte                         { return []byte(u.user.ID) }
func (u *passkeyUser) WebAuthnName() string                       { return u.user.Email }
func (u *passkeyUser) WebAuthnDisplayName() string                { return u.user.Name }
func (u *passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

func (s *Service) loadPasskeyUser(ctx context.Context, user *store.User) (*passkeyUser, error) {
	records, err := s.store.ListPasskeys(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	out := &passkeyUser{user: user, records: records}
	for _, record := range records {
		var credential webauthn.Credential
		if err := json.Unmarshal(record.Data, &credential); err != nil {
			continue
		}
		out.creds = append(out.creds, credential)
	}
	return out, nil
}

// BeginPasskeyRegistration returns creation options and a challenge token.
func (s *Service) BeginPasskeyRegistration(ctx context.Context, user *store.User) (*protocol.CredentialCreation, string, error) {
	if s.webauthn == nil {
		return nil, "", ErrPasskeyUnsupported
	}
	target, err := s.loadPasskeyUser(ctx, user)
	if err != nil {
		return nil, "", err
	}
	creation, session, err := s.webauthn.BeginRegistration(target)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrPasskeyInvalid, err)
	}
	token, err := s.passkeys.create(session)
	if err != nil {
		return nil, "", err
	}
	return creation, token, nil
}

// FinishPasskeyRegistration validates the attestation and stores the credential.
func (s *Service) FinishPasskeyRegistration(ctx context.Context, user *store.User, challenge string, body []byte, name string) (*store.Passkey, error) {
	if s.webauthn == nil {
		return nil, ErrPasskeyUnsupported
	}
	session, ok := s.passkeys.get(challenge)
	if !ok {
		return nil, ErrPasskeyInvalid
	}
	defer s.passkeys.delete(challenge)
	target, err := s.loadPasskeyUser(ctx, user)
	if err != nil {
		return nil, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPasskeyInvalid, err)
	}
	credential, err := s.webauthn.CreateCredential(target, *session, parsed)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPasskeyInvalid, err)
	}
	encoded, err := json.Marshal(credential)
	if err != nil {
		return nil, err
	}
	passkey := &store.Passkey{
		UserID: user.ID, CredentialID: credential.ID, PublicKey: credential.PublicKey,
		SignCount: credential.Authenticator.SignCount, Data: encoded, Name: name,
	}
	if passkey.Name == "" {
		passkey.Name = "Passkey"
	}
	if err := s.store.CreatePasskey(ctx, passkey); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, fmt.Errorf("%w: credential is already registered", ErrPasskeyInvalid)
		}
		return nil, err
	}
	return passkey, nil
}

// BeginPasskeyLogin returns assertion options for a discoverable login.
func (s *Service) BeginPasskeyLogin() (*protocol.CredentialAssertion, string, error) {
	if s.webauthn == nil {
		return nil, "", ErrPasskeyUnsupported
	}
	assertion, session, err := s.webauthn.BeginDiscoverableLogin()
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrPasskeyInvalid, err)
	}
	token, err := s.passkeys.create(session)
	if err != nil {
		return nil, "", err
	}
	return assertion, token, nil
}

// FinishPasskeyLogin validates an assertion and starts a session.
func (s *Service) FinishPasskeyLogin(ctx context.Context, challenge string, body []byte, ip, userAgent string) (*SessionResult, error) {
	if s.webauthn == nil {
		return nil, ErrPasskeyUnsupported
	}
	session, ok := s.passkeys.get(challenge)
	if !ok {
		return nil, ErrPasskeyInvalid
	}
	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(body))
	if err != nil {
		s.passkeys.delete(challenge)
		return nil, fmt.Errorf("%w: %v", ErrPasskeyInvalid, err)
	}
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		var record *store.Passkey
		if len(userHandle) > 0 {
			user, err := s.store.GetUser(ctx, string(userHandle))
			if err == nil {
				return s.loadPasskeyUser(ctx, user)
			}
		}
		resolved, err := s.store.GetPasskeyByCredentialID(ctx, rawID)
		if err != nil {
			return nil, ErrPasskeyInvalid
		}
		record = resolved
		user, err := s.store.GetUser(ctx, record.UserID)
		if err != nil {
			return nil, ErrPasskeyInvalid
		}
		return s.loadPasskeyUser(ctx, user)
	}
	user, credential, err := s.webauthn.ValidatePasskeyLogin(handler, *session, parsed)
	if err != nil {
		s.passkeys.delete(challenge)
		return nil, fmt.Errorf("%w: %v", ErrPasskeyInvalid, err)
	}
	s.passkeys.delete(challenge)
	target, ok := user.(*passkeyUser)
	if !ok {
		return nil, ErrPasskeyInvalid
	}
	encoded, err := json.Marshal(credential)
	if err == nil {
		for _, record := range target.records {
			if bytes.Equal(record.CredentialID, credential.ID) {
				_ = s.store.UpdatePasskey(ctx, record.ID, encoded, credential.PublicKey,
					credential.Authenticator.SignCount, s.now().UTC())
				break
			}
		}
	}
	if target.user.MFAEnabled {
		// Passkeys satisfy the second factor.
	}
	return s.createSession(ctx, target.user, ip, userAgent)
}

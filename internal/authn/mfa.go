package authn

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/store"
)

var (
	// ErrMFANotConfigured is returned when the server has no MFA key.
	ErrMFANotConfigured = errors.New("authn: MFA is not configured")
	// ErrMFAInvalid is returned for a wrong TOTP or recovery code.
	ErrMFAInvalid = errors.New("authn: invalid MFA code")
	// ErrMFAAlreadyEnabled means MFA is already active.
	ErrMFAAlreadyEnabled = errors.New("authn: MFA is already enabled")
	// ErrMFANotEnabled means MFA is not active for the user.
	ErrMFANotEnabled = errors.New("authn: MFA is not enabled")
)

// MFARequiredError signals that a password login needs a second factor.
type MFARequiredError struct {
	Challenge string
}

// Error implements error.
func (e *MFARequiredError) Error() string { return "authn: MFA required" }

// WithMFAKey sets the key used to encrypt TOTP secrets at rest.
func WithMFAKey(key crypto.Key) Option {
	return func(s *Service) { s.mfaKey = key }
}

// MFAEnrollment is returned when TOTP enrollment starts.
type MFAEnrollment struct {
	Secret string
	URL    string
}

// MFAStatus describes a user's MFA state.
type MFAStatus struct {
	Enabled       bool
	RecoveryCodes int
}

type mfaChallengeEntry struct {
	userID   string
	expires  time.Time
	attempts int
}

type mfaChallengeStore struct {
	mu    sync.Mutex
	now   func() time.Time
	byKey map[string]*mfaChallengeEntry
}

func newMFAChallengeStore(now func() time.Time) *mfaChallengeStore {
	return &mfaChallengeStore{now: now, byKey: make(map[string]*mfaChallengeEntry)}
}

func (m *mfaChallengeStore) create(userID string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	m.byKey[token] = &mfaChallengeEntry{userID: userID, expires: m.now().UTC().Add(5 * time.Minute)}
	return token, nil
}

func (m *mfaChallengeStore) get(token string) (*mfaChallengeEntry, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.byKey[token]
	if !ok || m.now().UTC().After(entry.expires) {
		delete(m.byKey, token)
		return nil, false
	}
	return entry, true
}

func (m *mfaChallengeStore) fail(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.byKey[token]
	if !ok {
		return
	}
	entry.attempts++
	if entry.attempts >= 5 {
		delete(m.byKey, token)
	}
}

func (m *mfaChallengeStore) delete(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.byKey, token)
}

func (m *mfaChallengeStore) pruneLocked() {
	now := m.now().UTC()
	for token, entry := range m.byKey {
		if now.After(entry.expires) {
			delete(m.byKey, token)
		}
	}
}

// EnrollMFA starts TOTP enrollment. The secret is stored encrypted but not
// active until a valid code is confirmed.
func (s *Service) EnrollMFA(ctx context.Context, user *store.User) (*MFAEnrollment, error) {
	if s.mfaKey == (crypto.Key{}) {
		return nil, ErrMFANotConfigured
	}
	if user.MFAEnabled {
		return nil, ErrMFAAlreadyEnabled
	}
	secret, err := GenerateTOTPSecret()
	if err != nil {
		return nil, err
	}
	ciphertext, err := s.sealMFASecret(user.ID, secret)
	if err != nil {
		return nil, err
	}
	if err := s.store.SetMFASecret(ctx, user.ID, ciphertext, false); err != nil {
		return nil, err
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
	return &MFAEnrollment{
		Secret: encoded,
		URL:    TOTPURL("Clavis", user.Email, secret),
	}, nil
}

// ActivateMFA confirms a code and returns one-time recovery codes.
func (s *Service) ActivateMFA(ctx context.Context, user *store.User, code string) ([]string, error) {
	if s.mfaKey == (crypto.Key{}) {
		return nil, ErrMFANotConfigured
	}
	if user.MFAEnabled {
		return nil, ErrMFAAlreadyEnabled
	}
	fresh, err := s.store.GetUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	secret, err := s.openMFASecret(fresh.ID, fresh.MFASecretCiphertext)
	if err != nil {
		return nil, err
	}
	defer secret.Zero()
	if !ValidateTOTP(secret, code, s.now().UTC()) {
		return nil, ErrMFAInvalid
	}
	if err := s.store.SetMFASecret(ctx, user.ID, fresh.MFASecretCiphertext, true); err != nil {
		return nil, err
	}
	codes, hashes, err := GenerateRecoveryCodes()
	if err != nil {
		return nil, err
	}
	if err := s.store.ReplaceRecoveryCodes(ctx, user.ID, hashes); err != nil {
		return nil, err
	}
	return codes, nil
}

// DisableMFA turns off TOTP after verifying a code.
func (s *Service) DisableMFA(ctx context.Context, user *store.User, code string) error {
	if !user.MFAEnabled {
		return ErrMFANotEnabled
	}
	secret, err := s.openMFASecret(user.ID, user.MFASecretCiphertext)
	if err != nil {
		return err
	}
	defer secret.Zero()
	if !ValidateTOTP(secret, code, s.now().UTC()) {
		return ErrMFAInvalid
	}
	if err := s.store.SetMFASecret(ctx, user.ID, nil, false); err != nil {
		return err
	}
	return s.store.ReplaceRecoveryCodes(ctx, user.ID, nil)
}

// RegenerateRecoveryCodes replaces all recovery codes after verifying a TOTP.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, user *store.User, code string) ([]string, error) {
	if !user.MFAEnabled {
		return nil, ErrMFANotEnabled
	}
	secret, err := s.openMFASecret(user.ID, user.MFASecretCiphertext)
	if err != nil {
		return nil, err
	}
	defer secret.Zero()
	if !ValidateTOTP(secret, code, s.now().UTC()) {
		return nil, ErrMFAInvalid
	}
	codes, hashes, err := GenerateRecoveryCodes()
	if err != nil {
		return nil, err
	}
	if err := s.store.ReplaceRecoveryCodes(ctx, user.ID, hashes); err != nil {
		return nil, err
	}
	return codes, nil
}

// MFAStatus returns the current state for a user.
func (s *Service) MFAStatus(ctx context.Context, user *store.User) (*MFAStatus, error) {
	remaining, err := s.store.CountUnusedRecoveryCodes(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	return &MFAStatus{Enabled: user.MFAEnabled, RecoveryCodes: remaining}, nil
}

// CompleteMFA exchanges a login challenge and second factor for a session.
func (s *Service) CompleteMFA(ctx context.Context, challenge, code, recoveryCode, ip, userAgent string) (*SessionResult, error) {
	entry, ok := s.mfaChallenges.get(challenge)
	if !ok {
		return nil, ErrMFAInvalid
	}
	user, err := s.store.GetUser(ctx, entry.userID)
	if err != nil || !user.MFAEnabled {
		s.mfaChallenges.delete(challenge)
		return nil, ErrMFAInvalid
	}
	valid := false
	if recoveryCode != "" {
		valid, err = s.store.ConsumeRecoveryCode(ctx, user.ID, HashRecoveryCode(recoveryCode))
		if err != nil {
			return nil, err
		}
	} else {
		secret, err := s.openMFASecret(user.ID, user.MFASecretCiphertext)
		if err != nil {
			return nil, err
		}
		valid = ValidateTOTP(secret, code, s.now().UTC())
		secret.Zero()
	}
	if !valid {
		s.mfaChallenges.fail(challenge)
		return nil, ErrMFAInvalid
	}
	s.mfaChallenges.delete(challenge)
	return s.createSession(ctx, user, ip, userAgent)
}

// BeginMFAChallenge creates a challenge for a password-authenticated user.
func (s *Service) BeginMFAChallenge(user *store.User) (string, error) {
	if s.mfaKey == (crypto.Key{}) {
		return "", ErrMFANotConfigured
	}
	return s.mfaChallenges.create(user.ID)
}

func (s *Service) sealMFASecret(userID string, secret []byte) ([]byte, error) {
	return s.mfaKey.Encrypt(1, crypto.Secret(secret), mfaAAD(userID))
}

func (s *Service) openMFASecret(userID string, ciphertext []byte) (crypto.Secret, error) {
	plaintext, _, err := s.mfaKey.Decrypt(ciphertext, mfaAAD(userID))
	if err != nil {
		return nil, fmt.Errorf("authn: decrypt MFA secret: %w", err)
	}
	return plaintext, nil
}

func mfaAAD(userID string) []byte {
	return []byte("clavis/mfa/v1|" + userID)
}

var _ = slog.LevelInfo

// ReencryptMFASecret rotates a user's TOTP secret from one root-derived MFA
// key to another. Used by offline root key rotation.
func ReencryptMFASecret(userID string, ciphertext []byte, oldKey, newKey crypto.Key) ([]byte, error) {
	plaintext, _, err := oldKey.Decrypt(ciphertext, mfaAAD(userID))
	if err != nil {
		return nil, fmt.Errorf("authn: decrypt MFA secret: %w", err)
	}
	defer plaintext.Zero()
	return newKey.Encrypt(1, plaintext, mfaAAD(userID))
}

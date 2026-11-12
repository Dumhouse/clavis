package authn

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/store"
)

var (
	// ErrInvalidCredentials deliberately covers unknown email and bad password.
	ErrInvalidCredentials = errors.New("authn: invalid credentials")
	// ErrRateLimited is returned when too many attempts were made.
	ErrRateLimited = errors.New("authn: too many attempts")
	// ErrUnauthenticated means the session is missing, expired, or revoked.
	ErrUnauthenticated = errors.New("authn: unauthenticated")
	// ErrSetupClosed means an owner already exists.
	ErrSetupClosed = errors.New("authn: setup is closed")
	// ErrInvalidSetupToken means the one-time token did not match.
	ErrInvalidSetupToken = errors.New("authn: invalid setup token")
	// ErrEmailTaken means another account uses the address.
	ErrEmailTaken = errors.New("authn: email already registered")
)

// Principal is an authenticated user plus their current session.
type Principal struct {
	User    *store.User
	Session *store.Session
	Via     string // "cookie" or "bearer"
}

// SessionResult is returned after login or setup. The raw token and CSRF
// secret are shown to the client once and never stored.
type SessionResult struct {
	Token     string
	CSRF      string
	Session   *store.Session
	User      *store.User
	ExpiresAt time.Time
}

// Service manages users and sessions.
type Service struct {
	store         *store.Store
	logger        *slog.Logger
	setup         *SetupToken
	limiter       *Limiter
	now           func() time.Time
	idle          time.Duration
	absolute      time.Duration
	dummy         string
	setupMu       sync.Mutex
	devices       *DeviceManager
	mfaKey        crypto.Key
	mfaChallenges *mfaChallengeStore
	webauthn      *webauthn.WebAuthn
	passkeys      *passkeyChallenges
	passwordGuard func(ctx context.Context, user *store.User) error
}

// Option configures a Service.
type Option func(*Service)

// WithClock replaces the clock (tests).
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// WithSessionTimeouts sets idle and absolute session lifetimes.
func WithSessionTimeouts(idle, absolute time.Duration) Option {
	return func(s *Service) {
		if idle > 0 {
			s.idle = idle
		}
		if absolute > 0 {
			s.absolute = absolute
		}
	}
}

// WithLimiter replaces the login limiter.
func WithLimiter(l *Limiter) Option { return func(s *Service) { s.limiter = l } }

// WithPasswordGuard runs after a correct password and before a session is created.
func WithPasswordGuard(guard func(ctx context.Context, user *store.User) error) Option {
	return func(s *Service) { s.passwordGuard = guard }
}

// WithWebAuthn configures passkey ceremonies.
func WithWebAuthn(rp *webauthn.WebAuthn) Option {
	return func(s *Service) { s.webauthn = rp }
}

// NewService builds an auth service. The dummy hash equalizes timing for
// unknown accounts.
func NewService(st *store.Store, logger *slog.Logger, setupPath string, opts ...Option) (*Service, error) {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Service{
		store:    st,
		logger:   logger,
		setup:    NewSetupToken(setupPath),
		limiter:  NewLimiter(8, 15*time.Minute),
		now:      time.Now,
		idle:     12 * time.Hour,
		absolute: 7 * 24 * time.Hour,
	}
	for _, opt := range opts {
		opt(s)
	}
	s.devices = NewDeviceManager(s.now)
	s.mfaChallenges = newMFAChallengeStore(s.now)
	s.passkeys = newPasskeyChallenges(s.now)
	dummy, err := HashPassword("clavis-dummy-password-for-timing")
	if err != nil {
		return nil, fmt.Errorf("authn: dummy hash: %w", err)
	}
	s.dummy = dummy
	return s, nil
}

// Now returns the service clock.
func (s *Service) Now() time.Time { return s.now() }

// SetupTokenPath returns the token file path.
func (s *Service) SetupTokenPath() string { return s.setup.Path() }

// SetupRequired reports whether no account exists yet.
func (s *Service) SetupRequired(ctx context.Context) (bool, error) {
	n, err := s.store.CountUsers(ctx)
	if err != nil {
		return false, err
	}
	return n == 0, nil
}

// EnsureSetupToken creates or reads the one-time token when setup is open.
func (s *Service) EnsureSetupToken(ctx context.Context) (token string, created bool, err error) {
	required, err := s.SetupRequired(ctx)
	if err != nil || !required {
		return "", false, err
	}
	return s.setup.Ensure()
}

// LockSetup verifies the token and serializes setup. The returned unlock must
// always be called.
func (s *Service) LockSetup(ctx context.Context, token string) (unlock func(), err error) {
	s.setupMu.Lock()
	required, err := s.SetupRequired(ctx)
	if err != nil || !required {
		s.setupMu.Unlock()
		if err != nil {
			return nil, err
		}
		return nil, ErrSetupClosed
	}
	if !s.setup.Verify(token) {
		s.setupMu.Unlock()
		return nil, ErrInvalidSetupToken
	}
	return s.setupMu.Unlock, nil
}

// ConsumeSetupToken deletes the token file after a successful setup.
func (s *Service) ConsumeSetupToken() error { return s.setup.Consume() }

// NormalizeEmail validates and lowercases an email address.
func NormalizeEmail(raw string) (string, error) {
	addr, err := mail.ParseAddress(strings.TrimSpace(raw))
	if err != nil || addr.Address == "" {
		return "", errors.New("authn: invalid email address")
	}
	email := strings.ToLower(addr.Address)
	if len(email) > 254 {
		return "", errors.New("authn: email address is too long")
	}
	return email, nil
}

// RegisterUser creates an account with a password. It does not create an org or
// session.
func (s *Service) RegisterUser(ctx context.Context, email, name, password string) (*store.User, error) {
	user, err := s.PrepareUser(email, name, password)
	if err != nil {
		return nil, err
	}
	if err := s.store.CreateUser(ctx, user); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrEmailTaken
		}
		return nil, err
	}
	return user, nil
}

// PrepareUser validates credentials and returns an unsaved user with a password
// hash. Setup uses it to insert the first user atomically with the org.
func (s *Service) PrepareUser(email, name, password string) (*store.User, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return nil, errors.New("authn: name is required and must be under 100 characters")
	}
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	return &store.User{
		ID:           uuid.NewString(),
		Email:        email,
		Name:         name,
		PasswordHash: hash,
	}, nil
}

// Login verifies a password and starts a session. Unknown emails take the same
// path and time as wrong passwords.
func (s *Service) Login(ctx context.Context, email, password, ip, userAgent string) (*SessionResult, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		// Still burn a verification to avoid a fast rejection oracle.
		_, _ = VerifyPassword(s.dummy, password)
		return nil, ErrInvalidCredentials
	}
	key := "login|" + ip + "|" + email
	if !s.limiter.Allow(key) {
		return nil, ErrRateLimited
	}

	user, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		_, _ = VerifyPassword(s.dummy, password)
		return nil, ErrInvalidCredentials
	}
	if user.PasswordHash == "" {
		_, _ = VerifyPassword(s.dummy, password)
		return nil, ErrInvalidCredentials
	}
	ok, err := VerifyPassword(user.PasswordHash, password)
	if err != nil || !ok {
		return nil, ErrInvalidCredentials
	}
	s.limiter.Reset(key)
	if s.passwordGuard != nil {
		if err := s.passwordGuard(ctx, user); err != nil {
			return nil, err
		}
	}
	if user.MFAEnabled {
		challenge, err := s.BeginMFAChallenge(user)
		if err != nil {
			return nil, err
		}
		return nil, &MFARequiredError{Challenge: challenge}
	}
	return s.createSession(ctx, user, ip, userAgent)
}

// CreateSession starts a session for an already-authenticated user.
func (s *Service) CreateSession(ctx context.Context, user *store.User, ip, userAgent string) (*SessionResult, error) {
	return s.createSession(ctx, user, ip, userAgent)
}

func (s *Service) createSession(ctx context.Context, user *store.User, ip, userAgent string) (*SessionResult, error) {
	token, tokenHash, err := randomToken()
	if err != nil {
		return nil, err
	}
	csrf, csrfHash, err := randomToken()
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	session := &store.Session{
		ID:         uuid.NewString(),
		UserID:     user.ID,
		TokenHash:  tokenHash,
		CSRFHash:   csrfHash,
		IP:         ip,
		UserAgent:  userAgent,
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  now.Add(s.absolute),
	}
	if err := s.store.CreateSession(ctx, session); err != nil {
		return nil, err
	}
	return &SessionResult{
		Token:     token,
		CSRF:      csrf,
		Session:   session,
		User:      user,
		ExpiresAt: session.ExpiresAt,
	}, nil
}

// Authenticate resolves a raw cookie token into a principal, enforcing idle and
// absolute timeouts.
func (s *Service) Authenticate(ctx context.Context, rawToken string) (*Principal, error) {
	return s.authenticate(ctx, rawToken, "cookie")
}

// AuthenticateBearer resolves a CLI/API bearer token.
func (s *Service) AuthenticateBearer(ctx context.Context, rawToken string) (*Principal, error) {
	return s.authenticate(ctx, rawToken, "bearer")
}

func (s *Service) authenticate(ctx context.Context, rawToken, via string) (*Principal, error) {
	if rawToken == "" || len(rawToken) > 256 {
		return nil, ErrUnauthenticated
	}
	sum := sha256.Sum256([]byte(rawToken))
	session, user, err := s.store.GetSessionByTokenHash(ctx, sum[:])
	if err != nil {
		return nil, ErrUnauthenticated
	}
	now := s.now().UTC()
	if !now.Before(session.ExpiresAt) || !now.Before(session.LastSeenAt.Add(s.idle)) {
		_ = s.store.DeleteSession(ctx, session.ID)
		return nil, ErrUnauthenticated
	}
	session.SeenBefore = session.LastSeenAt
	// Throttle last-seen writes to once a minute.
	if now.Sub(session.LastSeenAt) > time.Minute {
		if err := s.store.TouchSession(ctx, session.ID, now); err != nil {
			s.logger.Warn("touch session failed", "session_id", session.ID, "error", err)
		}
		session.LastSeenAt = now
	}
	return &Principal{User: user, Session: session, Via: via}, nil
}

// Logout revokes one session.
func (s *Service) Logout(ctx context.Context, sessionID string) error {
	if err := s.store.DeleteSession(ctx, sessionID); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return nil
}

// RevokeAllSessions logs a user out everywhere.
func (s *Service) RevokeAllSessions(ctx context.Context, userID string) (int64, error) {
	return s.store.DeleteUserSessions(ctx, userID)
}

// ListSessions returns a user's active sessions.
func (s *Service) ListSessions(ctx context.Context, userID string) ([]store.Session, error) {
	return s.store.ListSessionsForUser(ctx, userID)
}

// ValidateCSRF checks a header token against the session's stored hash.
func ValidateCSRF(session *store.Session, candidate string) bool {
	if session == nil || candidate == "" || len(session.CSRFHash) == 0 {
		return false
	}
	sum := sha256.Sum256([]byte(candidate))
	return subtle.ConstantTimeCompare(sum[:], session.CSRFHash) == 1
}

// Cleanup removes sessions past their absolute expiry.
func (s *Service) Cleanup(ctx context.Context) (int64, error) {
	return s.store.DeleteExpiredSessions(ctx, s.now().UTC())
}

// StartDevice begins a CLI login. Unknown callers are rate limited per IP.
func (s *Service) StartDevice(_ context.Context, ip string) (*DeviceGrant, error) {
	if !s.limiter.Allow("device-start|" + ip) {
		return nil, ErrRateLimited
	}
	return s.devices.Create()
}

// ApproveDevice links an authenticated human to a pending device code.
func (s *Service) ApproveDevice(userCode, userID string) error {
	return s.devices.Approve(userCode, userID)
}

// DenyDevice rejects a pending device code.
func (s *Service) DenyDevice(userCode string) error {
	return s.devices.Deny(userCode)
}

// ExchangeDevice collects an approved code and creates the CLI session.
func (s *Service) ExchangeDevice(ctx context.Context, deviceCode, ip, userAgent string) (*SessionResult, error) {
	userID, err := s.devices.Exchange(deviceCode)
	if err != nil {
		return nil, err
	}
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return nil, ErrDeviceNotFound
	}
	return s.createSession(ctx, user, ip, userAgent)
}

func randomToken() (raw string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, fmt.Errorf("authn: random token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(raw))
	return raw, sum[:], nil
}

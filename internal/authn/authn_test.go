package authn

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dumhouse/clavis/internal/store/storetest"
)

func TestPasswordHashAndVerify(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := VerifyPassword(hash, "correct horse battery staple")
	if err != nil || !ok {
		t.Fatalf("verify: %v %v", ok, err)
	}
	ok, err = VerifyPassword(hash, "wrong password")
	if err != nil || ok {
		t.Fatalf("wrong password accepted: %v %v", ok, err)
	}
	m, it, p, err := ParseParams(hash)
	if err != nil || m != argonMemory || it != argonTime || p != argonThreads {
		t.Fatalf("params = %d %d %d %v", m, it, p, err)
	}
}

func TestPasswordPolicy(t *testing.T) {
	if err := ValidatePassword("short"); !errors.Is(err, ErrInvalidPassword) {
		t.Fatal("short password accepted")
	}
	if err := ValidatePassword("123456789012"); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePassword(string(make([]byte, MaxPasswordLength+1))); !errors.Is(err, ErrInvalidPassword) {
		t.Fatal("long password accepted")
	}
}

func TestVerifyMalformedHash(t *testing.T) {
	cases := []string{
		"",
		"plaintext",
		"$argon2i$v=19$m=65536,t=3,p=2$c2FsdHNhbHQ$YWJjZGVmZ2hpamtsbW5vcA",
		"$argon2id$v=99$m=65536,t=3,p=2$c2FsdHNhbHQ$YWJjZGVmZ2hpamtsbW5vcA",
		"$argon2id$v=19$m=1,t=3,p=2$c2FsdHNhbHQ$YWJjZGVmZ2hpamtsbW5vcA",
		"$argon2id$v=19$m=65536,t=3,p=2$!!!$YWJjZGVmZ2hpamtsbW5vcA",
		"$argon2id$v=19$m=65536,t=3,p=2$c2FsdHNhbHQ$!!!",
	}
	for _, hash := range cases {
		if _, err := VerifyPassword(hash, "x"); !errors.Is(err, ErrInvalidHash) {
			t.Errorf("hash %q: err = %v", hash, err)
		}
	}
}

func TestNormalizeEmail(t *testing.T) {
	got, err := NormalizeEmail("  Owner@Example.COM ")
	if err != nil || got != "owner@example.com" {
		t.Fatalf("got %q err %v", got, err)
	}
	if _, err := NormalizeEmail("not-an-email"); err == nil {
		t.Fatal("invalid email accepted")
	}
}

func TestSetupTokenLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "setup-token")
	token := NewSetupToken(path)
	value, created, err := token.Ensure()
	if err != nil || !created || value == "" {
		t.Fatalf("ensure: %q %v %v", value, created, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions = %o", info.Mode().Perm())
	}
	again, created, err := token.Ensure()
	if err != nil || created || again != value {
		t.Fatalf("second ensure: %q %v %v", again, created, err)
	}
	if !token.Verify(value) {
		t.Fatal("valid token rejected")
	}
	if token.Verify("wrong") || token.Verify("") {
		t.Fatal("invalid token accepted")
	}
	if err := token.Consume(); err != nil {
		t.Fatal(err)
	}
	if token.Verify(value) {
		t.Fatal("token still valid after consume")
	}
	if err := token.Consume(); err != nil {
		t.Fatal("consume not idempotent")
	}
}

func TestLimiter(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := NewLimiter(2, time.Minute)
	l.now = func() time.Time { return now }
	if !l.Allow("k") || !l.Allow("k") {
		t.Fatal("first two attempts rejected")
	}
	if l.Allow("k") {
		t.Fatal("third attempt allowed")
	}
	if l.RetryAfter("k") <= 0 {
		t.Fatal("no retry window")
	}
	now = now.Add(2 * time.Minute)
	if !l.Allow("k") {
		t.Fatal("window did not reset")
	}
	l.Reset("k")
	if !l.Allow("k") {
		t.Fatal("reset did not clear")
	}
}

func newAuthService(t *testing.T) (*Service, *time.Time) {
	t.Helper()
	st := storetest.Open(t)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	svc, err := NewService(st, nil, filepath.Join(t.TempDir(), "setup-token"),
		WithClock(func() time.Time { return now }),
		WithLimiter(NewLimiter(3, time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	return svc, &now
}

func TestRegisterLoginAndSessions(t *testing.T) {
	ctx := context.Background()
	svc, now := newAuthService(t)

	user, err := svc.RegisterUser(ctx, "owner@example.com", "Owner", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RegisterUser(ctx, "owner@example.com", "Owner", "correct horse battery staple"); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("duplicate email: %v", err)
	}

	if _, err := svc.Login(ctx, "owner@example.com", "wrong password", "127.0.0.1", "test"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err := svc.Login(ctx, "unknown@example.com", "wrong password", "127.0.0.1", "test"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown user: %v", err)
	}

	result, err := svc.Login(ctx, "owner@example.com", "correct horse battery staple", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	if result.Token == "" || result.CSRF == "" || result.User.ID != user.ID {
		t.Fatalf("result = %+v", result)
	}
	if !ValidateCSRF(result.Session, result.CSRF) {
		t.Fatal("csrf validation failed")
	}
	if ValidateCSRF(result.Session, "wrong") {
		t.Fatal("wrong csrf accepted")
	}

	principal, err := svc.Authenticate(ctx, result.Token)
	if err != nil {
		t.Fatal(err)
	}
	if principal.User.ID != user.ID || principal.Session.ID != result.Session.ID {
		t.Fatalf("principal = %+v", principal)
	}
	if _, err := svc.Authenticate(ctx, "bogus"); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("bogus token: %v", err)
	}

	sessions, err := svc.ListSessions(ctx, user.ID)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions = %d %v", len(sessions), err)
	}

	// Idle timeout.
	*now = now.Add(13 * time.Hour)
	if _, err := svc.Authenticate(ctx, result.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("idle expiry not enforced: %v", err)
	}

	second, err := svc.CreateSession(ctx, user, "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Logout(ctx, second.Session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, second.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("logout not enforced: %v", err)
	}
}

func TestAbsoluteSessionExpiry(t *testing.T) {
	ctx := context.Background()
	svc, now := newAuthService(t)
	user, _ := svc.RegisterUser(ctx, "owner@example.com", "Owner", "correct horse battery staple")
	result, err := svc.CreateSession(ctx, user, "", "")
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(7 * 24 * time.Hour)
	if _, err := svc.Authenticate(ctx, result.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("absolute expiry not enforced: %v", err)
	}
}

func TestLoginRateLimit(t *testing.T) {
	ctx := context.Background()
	svc, _ := newAuthService(t)
	_, _ = svc.RegisterUser(ctx, "owner@example.com", "Owner", "correct horse battery staple")

	for i := 0; i < 3; i++ {
		_, _ = svc.Login(ctx, "owner@example.com", "wrong password", "10.0.0.1", "")
	}
	_, err := svc.Login(ctx, "owner@example.com", "wrong password", "10.0.0.1", "")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("rate limit: %v", err)
	}
	// A different IP has its own budget.
	if _, err := svc.Login(ctx, "owner@example.com", "correct horse battery staple", "10.0.0.2", ""); err != nil {
		t.Fatalf("separate ip: %v", err)
	}
}

func TestSetupLock(t *testing.T) {
	ctx := context.Background()
	svc, _ := newAuthService(t)

	token, created, err := svc.EnsureSetupToken(ctx)
	if err != nil || !created || token == "" {
		t.Fatalf("ensure: %q %v %v", token, created, err)
	}
	if _, err := svc.LockSetup(ctx, "wrong"); !errors.Is(err, ErrInvalidSetupToken) {
		t.Fatalf("wrong token: %v", err)
	}
	unlock, err := svc.LockSetup(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	if err := svc.ConsumeSetupToken(); err != nil {
		t.Fatal(err)
	}

	// Once an account exists, setup is closed.
	_, _ = svc.RegisterUser(ctx, "owner@example.com", "Owner", "correct horse battery staple")
	if _, err := svc.LockSetup(ctx, token); !errors.Is(err, ErrSetupClosed) {
		t.Fatalf("setup should be closed: %v", err)
	}
	if required, _ := svc.SetupRequired(ctx); required {
		t.Fatal("setup still required")
	}
}

func TestSessionCleanup(t *testing.T) {
	ctx := context.Background()
	svc, now := newAuthService(t)
	user, _ := svc.RegisterUser(ctx, "owner@example.com", "Owner", "correct horse battery staple")
	_, _ = svc.CreateSession(ctx, user, "", "")
	*now = now.Add(8 * 24 * time.Hour)
	n, err := svc.Cleanup(ctx)
	if err != nil || n != 1 {
		t.Fatalf("cleanup = %d %v", n, err)
	}
}

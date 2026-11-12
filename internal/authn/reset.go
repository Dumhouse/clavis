package authn

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/store"
)

// ErrResetInvalid covers unknown, expired, or already-used reset tokens.
var ErrResetInvalid = errors.New("authn: reset token invalid")

// ResetTokenTTL is how long a password reset link stays valid.
const ResetTokenTTL = 30 * time.Minute

// ForgotPassword creates a reset token when the account exists. It returns an
// empty token for unknown accounts so callers cannot enumerate users.
func (s *Service) ForgotPassword(ctx context.Context, email, ip string) (string, *store.User, error) {
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return "", nil, nil
	}
	if !s.limiter.Allow("forgot|" + ip) {
		return "", nil, ErrRateLimited
	}
	user, err := s.store.GetUserByEmail(ctx, normalized)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", nil, nil
		}
		return "", nil, err
	}
	token, hash, err := randomToken()
	if err != nil {
		return "", nil, err
	}
	reset := &store.PasswordReset{
		ID:        uuid.NewString(),
		UserID:    user.ID,
		TokenHash: hash,
		CreatedAt: s.now().UTC(),
		ExpiresAt: s.now().UTC().Add(ResetTokenTTL),
	}
	if err := s.store.CreatePasswordReset(ctx, reset); err != nil {
		return "", nil, err
	}
	return token, user, nil
}

// ResetPassword validates the token, sets a new password, and revokes sessions.
// It returns the affected user ID.
func (s *Service) ResetPassword(ctx context.Context, token, password string) (string, error) {
	if token == "" || len(token) > 256 {
		return "", ErrResetInvalid
	}
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	hash := HashToken(token)
	reset, err := s.store.GetPasswordResetByHash(ctx, hash)
	if err != nil || reset.UsedAt != nil || !s.now().UTC().Before(reset.ExpiresAt) {
		return "", ErrResetInvalid
	}
	newHash, err := HashPassword(password)
	if err != nil {
		return "", err
	}
	if err := s.store.CompletePasswordReset(ctx, reset.ID, reset.UserID, newHash); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return "", ErrResetInvalid
		}
		return "", err
	}
	return reset.UserID, nil
}

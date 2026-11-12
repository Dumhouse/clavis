package authn

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/store"
)

var (
	// ErrVerificationInvalid covers unknown, expired, or used tokens.
	ErrVerificationInvalid = errors.New("authn: verification token invalid")
	// ErrAlreadyVerified means the address is already confirmed.
	ErrAlreadyVerified = errors.New("authn: email already verified")
)

// VerificationTokenTTL is how long a verification link stays valid.
const VerificationTokenTTL = 24 * time.Hour

// CreateEmailVerification issues a verification token.
func (s *Service) CreateEmailVerification(ctx context.Context, user *store.User) (string, error) {
	if user.EmailVerified {
		return "", ErrAlreadyVerified
	}
	token, hash, err := randomToken()
	if err != nil {
		return "", err
	}
	verification := &store.EmailVerification{
		ID:        uuid.NewString(),
		UserID:    user.ID,
		TokenHash: hash,
		CreatedAt: s.now().UTC(),
		ExpiresAt: s.now().UTC().Add(VerificationTokenTTL),
	}
	if err := s.store.CreateEmailVerification(ctx, verification); err != nil {
		return "", err
	}
	return token, nil
}

// VerifyEmail confirms an address with a token and returns the user ID.
func (s *Service) VerifyEmail(ctx context.Context, token string) (string, error) {
	if token == "" || len(token) > 256 {
		return "", ErrVerificationInvalid
	}
	verification, err := s.store.GetEmailVerificationByHash(ctx, HashToken(token))
	if err != nil || verification.UsedAt != nil || !s.now().UTC().Before(verification.ExpiresAt) {
		return "", ErrVerificationInvalid
	}
	if err := s.store.CompleteEmailVerification(ctx, verification.ID, verification.UserID); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return "", ErrVerificationInvalid
		}
		return "", err
	}
	return verification.UserID, nil
}

package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/pkg/problem"
)

const reauthTTL = 5 * time.Minute

type reauthInput struct {
	Body struct {
		Password string `json:"password" minLength:"1" maxLength:"1024"`
	}
}

type reauthOutput struct {
	Body struct {
		Token     string    `json:"reauth_token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
}

func (s *Server) registerReauthRoutes() error {
	if err := s.registry.DeclareAuthenticated(http.MethodPost, "/api/v1/auth/reauthenticate", "confirm a password before sensitive actions"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "reauthenticate", Method: http.MethodPost, Path: "/api/v1/auth/reauthenticate",
		Summary: "Re-authenticate for a sensitive action", Tags: []string{"auth"},
	}, func(ctx context.Context, in *reauthInput) (*reauthOutput, error) {
		actor, ok := actorFrom(ctx)
		if !ok {
			return nil, withRequestID(problem.New(http.StatusUnauthorized, "unauthenticated", "Not signed in", "Sign in first."), ctx)
		}
		if err := s.svc.VerifyActorPassword(ctx, actor, in.Body.Password); err != nil {
			return nil, s.apiError(ctx, err)
		}
		expires := time.Now().UTC().Add(reauthTTL)
		out := &reauthOutput{}
		out.Body.Token = s.signReauth(actor.ID, expires)
		out.Body.ExpiresAt = expires
		return out, nil
	})
	return nil
}

func (s *Server) signReauth(userID string, expires time.Time) string {
	payload := userID + "|" + strconv.FormatInt(expires.Unix(), 10)
	mac := hmac.New(sha256.New, s.rootKey[:])
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// requireReauth accepts a token issued by /auth/reauthenticate for this actor.
func (s *Server) requireReauth(ctx context.Context, token string) error {
	actor, ok := actorFrom(ctx)
	if !ok {
		return reauthRequired(ctx)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return reauthRequired(ctx)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return reauthRequired(ctx)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return reauthRequired(ctx)
	}
	mac := hmac.New(sha256.New, s.rootKey[:])
	mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return reauthRequired(ctx)
	}
	fields := strings.Split(string(payload), "|")
	if len(fields) != 2 || fields[0] != actor.ID {
		return reauthRequired(ctx)
	}
	expires, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || !time.Now().UTC().Before(time.Unix(expires, 0)) {
		return reauthRequired(ctx)
	}
	return nil
}

func reauthRequired(ctx context.Context) error {
	return withRequestID(problem.New(http.StatusForbidden, "reauth_required",
		"Re-authentication required", "Confirm your password to continue."), ctx)
}

func unauthenticated(ctx context.Context) error {
	return withRequestID(problem.New(http.StatusUnauthorized, "unauthenticated", "Not signed in", "Sign in first."), ctx)
}

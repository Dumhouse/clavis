package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/store"
)

type passkeyBody struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

type passkeysOutput struct {
	Body struct {
		Passkeys []passkeyBody `json:"passkeys"`
	}
}

type passkeyOptionsOutput struct {
	Body struct {
		ChallengeID string          `json:"challenge_id"`
		Options     json.RawMessage `json:"options"`
	}
}

type passkeyLoginOptionsOutput struct {
	Body struct {
		ChallengeID string          `json:"challenge_id"`
		Options     json.RawMessage `json:"options"`
	}
}

type passkeyRegisterFinishInput struct {
	Body struct {
		ChallengeID string          `json:"challenge_id" minLength:"1"`
		Name        string          `json:"name,omitempty" maxLength:"100"`
		Credential  json.RawMessage `json:"credential"`
	}
}

type passkeyLoginFinishInput struct {
	Body struct {
		ChallengeID string          `json:"challenge_id" minLength:"1"`
		Credential  json.RawMessage `json:"credential"`
	}
}

type passkeyOutput struct {
	Body passkeyBody
}

type passkeyPath struct {
	ID string `path:"id"`
}

func (s *Server) registerPasskeyRoutes() error {
	if err := s.registry.DeclareAuthenticated(http.MethodPost, "/api/v1/auth/passkeys/register/begin", "starts a passkey enrollment for the signed-in user"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "begin-passkey-registration", Method: http.MethodPost, Path: "/api/v1/auth/passkeys/register/begin",
		Summary: "Begin passkey enrollment", Tags: []string{"auth"},
	}, func(ctx context.Context, _ *struct{}) (*passkeyOptionsOutput, error) {
		principal, ok := principalFrom(ctx)
		if !ok {
			return nil, unauthenticated(ctx)
		}
		creation, challenge, err := s.auth.BeginPasskeyRegistration(ctx, principal.User)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		raw, err := json.Marshal(creation)
		if err != nil {
			return nil, err
		}
		out := &passkeyOptionsOutput{}
		out.Body.ChallengeID = challenge
		out.Body.Options = raw
		return out, nil
	})

	if err := s.registry.DeclareAuthenticated(http.MethodPost, "/api/v1/auth/passkeys/register/finish", "finishes passkey enrollment"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "finish-passkey-registration", Method: http.MethodPost, Path: "/api/v1/auth/passkeys/register/finish",
		Summary: "Finish passkey enrollment", Tags: []string{"auth"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *passkeyRegisterFinishInput) (*passkeyOutput, error) {
		principal, ok := principalFrom(ctx)
		if !ok {
			return nil, unauthenticated(ctx)
		}
		passkey, err := s.auth.FinishPasskeyRegistration(ctx, principal.User, in.Body.ChallengeID, in.Body.Credential, in.Body.Name)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &passkeyOutput{Body: passkeyFromStore(passkey)}, nil
	})

	if err := s.registry.DeclareAuthenticated(http.MethodGet, "/api/v1/auth/passkeys", "lists the caller's passkeys"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-passkeys", Method: http.MethodGet, Path: "/api/v1/auth/passkeys",
		Summary: "List passkeys", Tags: []string{"auth"},
	}, func(ctx context.Context, _ *struct{}) (*passkeysOutput, error) {
		principal, ok := principalFrom(ctx)
		if !ok {
			return nil, unauthenticated(ctx)
		}
		passkeys, err := s.store.ListPasskeys(ctx, principal.User.ID)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &passkeysOutput{}
		out.Body.Passkeys = []passkeyBody{}
		for i := range passkeys {
			out.Body.Passkeys = append(out.Body.Passkeys, passkeyFromStore(&passkeys[i]))
		}
		return out, nil
	})

	if err := s.registry.DeclareAuthenticated(http.MethodDelete, "/api/v1/auth/passkeys/{id}", "removes one of the caller's passkeys"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "delete-passkey", Method: http.MethodDelete, Path: "/api/v1/auth/passkeys/{id}",
		Summary: "Delete a passkey", Tags: []string{"auth"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *passkeyPath) (*emptyOutput, error) {
		principal, ok := principalFrom(ctx)
		if !ok {
			return nil, unauthenticated(ctx)
		}
		if err := s.store.DeletePasskey(ctx, principal.User.ID, in.ID); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})

	if err := s.registry.DeclarePublic(http.MethodPost, "/api/v1/auth/passkeys/login/begin", "starts a discoverable passkey login"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "begin-passkey-login", Method: http.MethodPost, Path: "/api/v1/auth/passkeys/login/begin",
		Summary: "Begin passkey sign-in", Tags: []string{"auth"},
	}, func(ctx context.Context, _ *struct{}) (*passkeyLoginOptionsOutput, error) {
		assertion, challenge, err := s.auth.BeginPasskeyLogin()
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		raw, err := json.Marshal(assertion)
		if err != nil {
			return nil, err
		}
		out := &passkeyLoginOptionsOutput{}
		out.Body.ChallengeID = challenge
		out.Body.Options = raw
		return out, nil
	})

	if err := s.registry.DeclarePublic(http.MethodPost, "/api/v1/auth/passkeys/login/finish", "finishes a discoverable passkey login"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "finish-passkey-login", Method: http.MethodPost, Path: "/api/v1/auth/passkeys/login/finish",
		Summary: "Finish passkey sign-in", Tags: []string{"auth"},
	}, func(ctx context.Context, in *passkeyLoginFinishInput) (*sessionOutput, error) {
		meta := metaFrom(ctx)
		session, err := s.auth.FinishPasskeyLogin(ctx, in.Body.ChallengeID, in.Body.Credential, meta.IP, meta.UserAgent)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &sessionOutput{SetCookie: s.sessionCookies(session)}
		body, err := s.sessionBody(ctx, session)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out.Body = body
		return out, nil
	})
	return nil
}

func passkeyFromStore(passkey *store.Passkey) passkeyBody {
	return passkeyBody{
		ID: passkey.ID, Name: passkey.Name, CreatedAt: passkey.CreatedAt, LastUsedAt: passkey.LastUsedAt,
	}
}

package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/dumhouse/clavis/internal/authn"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/service"
	"github.com/dumhouse/clavis/internal/store"
	"github.com/dumhouse/clavis/pkg/problem"
)

type userBody struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	EmailVerified bool   `json:"email_verified"`
}

func userFromStore(u *store.User) userBody {
	return userBody{ID: u.ID, Email: u.Email, Name: u.Name, EmailVerified: u.EmailVerified}
}

type orgBody struct {
	ID                string `json:"id"`
	Slug              string `json:"slug"`
	Name              string `json:"name"`
	Role              string `json:"role,omitempty"`
	DefaultAppearance string `json:"default_appearance,omitempty"`
}

func orgFromStore(o *store.Org, role string) orgBody {
	return orgBody{
		ID: o.ID, Slug: o.Slug, Name: o.Name, Role: role,
		DefaultAppearance: service.ParseOrgSettings(o.Settings).DefaultAppearance,
	}
}

type envBody struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Color     string `json:"color"`
	Protected bool   `json:"protected"`
	Position  int    `json:"position"`
	ParentID  string `json:"parent_id,omitempty"`
}

func envFromStore(e *store.Environment) envBody {
	parentID := ""
	if e.ParentID != nil {
		parentID = *e.ParentID
	}
	return envBody{ID: e.ID, Slug: e.Slug, Name: e.Name, Type: e.Type, Color: e.Color,
		Protected: e.IsProtected, Position: e.Position, ParentID: parentID}
}

type projectBody struct {
	ID           string    `json:"id"`
	Slug         string    `json:"slug"`
	Name         string    `json:"name"`
	Environments []envBody `json:"environments,omitempty"`
}

func projectFromStore(p *store.Project, envs []store.Environment) projectBody {
	body := projectBody{ID: p.ID, Slug: p.Slug, Name: p.Name}
	for i := range envs {
		body.Environments = append(body.Environments, envFromStore(&envs[i]))
	}
	return body
}

type secretBody struct {
	Key               string    `json:"key"`
	Kind              string    `json:"kind"`
	Comment           string    `json:"comment,omitempty"`
	Folder            string    `json:"folder,omitempty"`
	Notes             string    `json:"notes,omitempty"`
	ProviderURL       string    `json:"provider_url,omitempty"`
	OwnerGroupID      string    `json:"owner_group_id,omitempty"`
	OwnerGroupName    string    `json:"owner_group_name,omitempty"`
	SourceEnvironment string    `json:"source_environment,omitempty"`
	Inherited         bool      `json:"inherited,omitempty"`
	Overridden        bool      `json:"overridden,omitempty"`
	Tags              []string  `json:"tags,omitempty"`
	Value             string    `json:"value,omitempty"`
	Version           int       `json:"version"`
	Revealed          bool      `json:"revealed"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func secretFromService(s *service.Secret) secretBody {
	return secretBody{Key: s.Key, Kind: s.Kind, Comment: s.Comment, Folder: s.Folder,
		Notes: s.Notes, ProviderURL: s.ProviderURL, OwnerGroupID: s.OwnerGroupID,
		OwnerGroupName: s.OwnerGroupName, SourceEnvironment: s.SourceEnvironment,
		Inherited: s.Inherited, Overridden: s.Overridden, Tags: s.Tags, Value: s.Value,
		Version: s.Version, Revealed: s.Revealed, UpdatedAt: s.UpdatedAt}
}

// apiError maps domain errors to RFC 9457 problems without leaking internals.
func (s *Server) apiError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	var p *problem.Problem
	if errors.As(err, &p) {
		if p.RequestID == "" {
			p.RequestID = requestID(ctx)
		}
		return p
	}

	switch {
	case errors.Is(err, service.ErrValidation):
		detail := strings.TrimPrefix(err.Error(), "service: validation failed: ")
		return withRequestID(problem.New(http.StatusUnprocessableEntity, "validation_failed", "Validation failed", detail), ctx)
	case errors.Is(err, service.ErrCLIBlocked):
		p := problem.Forbidden("This organization blocks CLI writes to production. Use the web UI or ask an Owner.")
		p.Code = "cli_writes_blocked"
		return withRequestID(p, ctx)
	case errors.Is(err, service.ErrProtected):
		p := problem.Forbidden("This environment is protected. Ask an Admin or Owner to make the change.")
		p.Code = "protected_environment"
		return withRequestID(p, ctx)
	case errors.Is(err, service.ErrForbidden):
		return withRequestID(problem.Forbidden("You do not have permission to perform this action."), ctx)
	case errors.Is(err, service.ErrNotFound):
		return withRequestID(problem.NotFound("The requested resource does not exist."), ctx)
	case errors.Is(err, service.ErrPrecondition):
		return withRequestID(problem.New(http.StatusPreconditionFailed, "precondition_failed", "Precondition failed", "Someone changed this environment. Refresh and try again."), ctx)
	case errors.Is(err, service.ErrConflict):
		return withRequestID(problem.New(http.StatusConflict, "conflict", "Conflict", "That name is already in use."), ctx)
	case errors.Is(err, authn.ErrInvalidCredentials):
		return withRequestID(problem.Unauthorized("Invalid email or password."), ctx)
	case errors.Is(err, authn.ErrRateLimited):
		return withRequestID(problem.New(http.StatusTooManyRequests, "rate_limited", "Too many attempts", "Wait a few minutes and try again."), ctx)
	case errors.Is(err, authn.ErrDevicePending):
		return withRequestID(problem.New(http.StatusPreconditionRequired, "authorization_pending", "Authorization pending", "Approve the code in your browser, then try again."), ctx)
	case errors.Is(err, authn.ErrDeviceNotFound):
		return withRequestID(problem.New(http.StatusGone, "device_code_invalid", "Code expired or invalid", "Start `clavis login` again."), ctx)
	case errors.Is(err, authn.ErrDeviceDenied):
		return withRequestID(problem.Forbidden("This login request was denied."), ctx)
	case errors.Is(err, authn.ErrDeviceConsumed):
		return withRequestID(problem.New(http.StatusConflict, "device_code_used", "Code already used", "Start `clavis login` again."), ctx)
	case errors.Is(err, authn.ErrSetupClosed):
		return withRequestID(problem.New(http.StatusGone, "setup_closed", "Setup is closed", "This instance already has an owner."), ctx)
	case errors.Is(err, authn.ErrInvalidSetupToken):
		return withRequestID(problem.Forbidden("That setup token is not valid."), ctx)
	case errors.Is(err, authn.ErrEmailTaken):
		return withRequestID(problem.New(http.StatusConflict, "email_taken", "Email already registered", "Sign in instead, or use another address."), ctx)
	case errors.Is(err, service.ErrInviteInvalid):
		return withRequestID(problem.New(http.StatusGone, "invite_invalid", "Invite expired or invalid", "Ask an owner for a new invite link."), ctx)
	case errors.Is(err, service.ErrInviteEmailMismatch):
		return withRequestID(problem.Forbidden("This invite was issued for a different email address."), ctx)
	case errors.Is(err, authn.ErrMFANotConfigured):
		return withRequestID(problem.New(http.StatusServiceUnavailable, "mfa_unavailable", "MFA is not configured", "Set a root key and restart the server."), ctx)
	case errors.Is(err, authn.ErrMFAInvalid):
		return withRequestID(problem.New(http.StatusUnauthorized, "mfa_invalid", "Invalid code", "Check the authenticator code or use a recovery code."), ctx)
	case errors.Is(err, authn.ErrMFAAlreadyEnabled):
		return withRequestID(problem.New(http.StatusConflict, "mfa_enabled", "MFA is already enabled", "Disable it first to enroll again."), ctx)
	case errors.Is(err, authn.ErrMFANotEnabled):
		return withRequestID(problem.New(http.StatusConflict, "mfa_not_enabled", "MFA is not enabled", "Enroll an authenticator first."), ctx)
	case errors.Is(err, authn.ErrVerificationInvalid):
		return withRequestID(problem.New(http.StatusGone, "verification_invalid", "Verification link expired or invalid", "Request a new verification email."), ctx)
	case errors.Is(err, authn.ErrAlreadyVerified):
		return withRequestID(problem.New(http.StatusConflict, "already_verified", "Email already verified", "No further action is needed."), ctx)
	case errors.Is(err, authn.ErrResetInvalid):
		return withRequestID(problem.New(http.StatusGone, "reset_invalid", "Reset link expired or invalid", "Request a new password reset link."), ctx)
	case errors.Is(err, service.ErrLastOwner):
		return withRequestID(problem.New(http.StatusConflict, "last_owner", "Cannot remove the last owner", "Promote another member to Owner first."), ctx)
	case errors.Is(err, service.ErrSyncUnconfirmed):
		return withRequestID(problem.New(http.StatusConflict, "sync_unconfirmed", "Confirm the first sync", "Run a dry-run preview and retry with confirm=true."), ctx)
	case errors.Is(err, service.ErrSSORequired):
		return withRequestID(problem.New(http.StatusForbidden, "sso_required", "Single sign-on is required", "Sign in with your organization's SSO provider."), ctx)
	case errors.Is(err, service.ErrSSOUnavailable):
		return withRequestID(problem.New(http.StatusBadRequest, "sso_failed", "Single sign-on failed", "Check the organization's SSO configuration and try again."), ctx)
	case errors.Is(err, service.ErrReauth):
		return withRequestID(problem.New(http.StatusForbidden, "reauth_required", "Re-authentication required", "Confirm your password to continue."), ctx)
	case errors.Is(err, service.ErrBreachedPassword):
		return withRequestID(problem.New(http.StatusUnprocessableEntity, "breached_password", "Password appears in a breach corpus", "Choose a password that has not appeared in known data breaches."), ctx)
	case errors.Is(err, authn.ErrPasskeyUnsupported):
		return withRequestID(problem.New(http.StatusNotImplemented, "passkeys_unavailable", "Passkeys are not configured", "Set a public server URL to enable WebAuthn."), ctx)
	case errors.Is(err, authn.ErrPasskeyInvalid):
		return withRequestID(problem.New(http.StatusUnprocessableEntity, "passkey_failed", "Passkey verification failed", "Try again or use another sign-in method."), ctx)
	case errors.Is(err, authn.ErrUnauthenticated):
		return withRequestID(problem.New(http.StatusUnauthorized, "unauthenticated", "Not signed in", "Sign in first."), ctx)
	case errors.Is(err, authn.ErrInvalidPassword):
		return withRequestID(problem.New(http.StatusUnprocessableEntity, "weak_password", "Password is too short", "Use at least 12 characters."), ctx)
	default:
		s.logger.Error("api error", "error", err, "request_id", requestID(ctx))
		return withRequestID(problem.Internal("An unexpected error occurred. Include the request ID when reporting this."), ctx)
	}
}

func withRequestID(p *problem.Problem, ctx context.Context) *problem.Problem {
	p.RequestID = requestID(ctx)
	return p
}

func (s *Server) sessionCookies(result *authn.SessionResult) []http.Cookie {
	maxAge := int(time.Until(result.ExpiresAt).Seconds())
	session := http.Cookie{
		Name:     s.sessionCookieName(),
		Value:    result.Token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cookieSecure(),
		SameSite: http.SameSiteStrictMode,
		Expires:  result.ExpiresAt,
		MaxAge:   maxAge,
	}
	csrf := http.Cookie{
		Name:     s.csrfCookieName(),
		Value:    result.CSRF,
		Path:     "/",
		HttpOnly: false,
		Secure:   s.cookieSecure(),
		SameSite: http.SameSiteStrictMode,
		Expires:  result.ExpiresAt,
		MaxAge:   maxAge,
	}
	return []http.Cookie{session, csrf}
}

func (s *Server) clearedCookies() []http.Cookie {
	expired := time.Unix(1, 0)
	return []http.Cookie{
		{Name: s.sessionCookieName(), Value: "", Path: "/", HttpOnly: true, Secure: s.cookieSecure(), SameSite: http.SameSiteStrictMode, Expires: expired, MaxAge: -1},
		{Name: s.csrfCookieName(), Value: "", Path: "/", HttpOnly: false, Secure: s.cookieSecure(), SameSite: http.SameSiteStrictMode, Expires: expired, MaxAge: -1},
	}
}

func principalFrom(ctx context.Context) (*authn.Principal, bool) {
	return authn.PrincipalFromContext(ctx)
}

func actorFrom(ctx context.Context) (authz.Actor, bool) {
	return authz.ActorFromContext(ctx)
}

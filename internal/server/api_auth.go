package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authn"
	"github.com/dumhouse/clavis/internal/config"
	"github.com/dumhouse/clavis/internal/email"
	"github.com/dumhouse/clavis/internal/service"
	"github.com/dumhouse/clavis/pkg/problem"
)

type setupStatusOutput struct {
	Body struct {
		Required   bool   `json:"required"`
		OwnerEmail string `json:"owner_email,omitempty" doc:"Preset owner email (CLAVIS_SETUP_OWNER_EMAIL); only returned while setup is open"`
		TLSMode    string `json:"tls_mode,omitempty" doc:"off, self-signed, file, or acme; only returned while setup is open"`
		Secure     bool   `json:"secure" doc:"Whether the configured base URL is https"`
		// Anyone can read the certificate from the TLS handshake, so this
		// reveals nothing new. The web app shows it next to `clavis login`.
		SelfSignedFingerprint string `json:"self_signed_fingerprint,omitempty" doc:"SHA-256 of the self-signed certificate (CLAVIS_TLS=self-signed), as the CLI shows it when asking to trust the server"`
	}
}

type setupInput struct {
	Body struct {
		Token       string `json:"token" minLength:"1" doc:"One-time token from the server log or <data-dir>/setup-token"`
		Email       string `json:"email" format:"email"`
		Name        string `json:"name" minLength:"1" maxLength:"100"`
		Password    string `json:"password" minLength:"12" maxLength:"1024"`
		OrgName     string `json:"org_name,omitempty" maxLength:"100" doc:"Defaults to \"My organization\""`
		OrgSlug     string `json:"org_slug,omitempty" maxLength:"60"`
		ProjectName string `json:"project_name,omitempty" maxLength:"100"`
		ProjectSlug string `json:"project_slug,omitempty" maxLength:"60"`
		SkipProject bool   `json:"skip_project,omitempty" doc:"Create no project; the web app asks for one after sign-in"`
	}
}

type loginInput struct {
	Body struct {
		Email    string `json:"email" format:"email"`
		Password string `json:"password" minLength:"1" maxLength:"1024"`
	}
}

// preferencesBody is the caller's own display settings. The date, time and
// font choices are empty until the user picks one ("auto" and "default" keep
// the browser's and the style's own).
type preferencesBody struct {
	Theme      string `json:"theme"`
	Appearance string `json:"appearance"`
	DateFormat string `json:"date_format,omitempty"`
	TimeFormat string `json:"time_format,omitempty"`
	Font       string `json:"font,omitempty"`
	FontMono   string `json:"font_mono,omitempty"`
}

type sessionBody struct {
	User        userBody        `json:"user"`
	Orgs        []orgBody       `json:"orgs"`
	CSRF        string          `json:"csrf_token"`
	ExpiresAt   time.Time       `json:"expires_at"`
	Preferences preferencesBody `json:"preferences"`
}

type sessionOutput struct {
	SetCookie []http.Cookie `header:"Set-Cookie"`
	Body      sessionBody
}

type meOutput struct {
	Body sessionBody
}

type emptyOutput struct{}

type logoutOutput struct {
	SetCookie []http.Cookie `header:"Set-Cookie"`
}

type sessionsOutput struct {
	Body struct {
		Sessions []sessionInfo `json:"sessions"`
	}
}

type sessionInfo struct {
	ID         string    `json:"id"`
	IP         string    `json:"ip"`
	UserAgent  string    `json:"user_agent"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Current    bool      `json:"current"`
}

func (s *Server) registerAuthRoutes() error {
	if err := s.registry.DeclarePublic(http.MethodGet, "/api/v1/setup/status", "the login screen must know whether to show the setup wizard"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "setup-status",
		Method:      http.MethodGet,
		Path:        "/api/v1/setup/status",
		Summary:     "Whether first-run setup is required",
		Tags:        []string{"auth"},
	}, func(ctx context.Context, _ *struct{}) (*setupStatusOutput, error) {
		required, err := s.auth.SetupRequired(ctx)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &setupStatusOutput{}
		out.Body.Required = required
		out.Body.Secure = strings.HasPrefix(s.cfg.Server.BaseURL, "https://")
		if s.cfg.Server.TLS.Mode == config.TLSSelfSigned {
			if info, err := DescribeTLS(s.cfg); err == nil && info.Leaf != nil {
				out.Body.SelfSignedFingerprint = info.Fingerprint()
			}
		}
		if required {
			out.Body.OwnerEmail = s.cfg.Server.Setup.OwnerEmail
			out.Body.TLSMode = s.cfg.Server.TLS.Mode
		}
		return out, nil
	})

	if err := s.registry.DeclarePublic(http.MethodPost, "/api/v1/setup", "the one-time setup token is the credential for first-run setup"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "setup",
		Method:      http.MethodPost,
		Path:        "/api/v1/setup",
		Summary:     "Create the first owner, org, and project",
		Tags:        []string{"auth"},
	}, func(ctx context.Context, in *setupInput) (*sessionOutput, error) {
		unlock, err := s.auth.LockSetup(ctx, in.Body.Token)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		defer unlock()

		if want := s.cfg.Server.Setup.OwnerEmail; want != "" && !strings.EqualFold(strings.TrimSpace(in.Body.Email), want) {
			return nil, withRequestID(ctx, problem.BadRequest("The owner email for this server is preset to "+want+"."))
		}
		if err := s.svc.CheckPassword(ctx, in.Body.Password); err != nil {
			return nil, s.apiError(ctx, err)
		}
		user, err := s.auth.PrepareUser(in.Body.Email, in.Body.Name, in.Body.Password)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		user.EmailVerified = true
		user.Preferences = `{"theme":"system","appearance":"classic"}`
		_, _, _, err = s.svc.Setup(ctx, user, service.SetupInput{
			OrgName:     in.Body.OrgName,
			OrgSlug:     in.Body.OrgSlug,
			ProjectName: in.Body.ProjectName,
			ProjectSlug: in.Body.ProjectSlug,
			SkipProject: in.Body.SkipProject,
		})
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		if err := s.auth.ConsumeSetupToken(); err != nil {
			s.logger.Error("consume setup token failed", "error", err)
		}
		session, err := s.auth.CreateSession(ctx, user, metaFrom(ctx).IP, metaFrom(ctx).UserAgent)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		body, err := s.sessionBody(ctx, session)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &sessionOutput{SetCookie: s.sessionCookies(session), Body: body}, nil
	})

	if err := s.registry.DeclarePublic(http.MethodPost, "/api/v1/auth/login", "login is the entry point for unauthenticated users; it is rate limited and CSRF-origin checked"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "login",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/login",
		Summary:     "Sign in with email and password",
		Tags:        []string{"auth"},
	}, func(ctx context.Context, in *loginInput) (*loginOutput, error) {
		meta := metaFrom(ctx)
		session, err := s.auth.Login(ctx, in.Body.Email, in.Body.Password, meta.IP, meta.UserAgent)
		var mfaErr *authn.MFARequiredError
		if errors.As(err, &mfaErr) {
			out := &loginOutput{}
			out.Body.MFARequired = true
			out.Body.Challenge = mfaErr.Challenge
			return out, nil
		}
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		s.svc.RecordAuthEvent(ctx, session.User, "auth.login", "password", meta)
		body, err := s.sessionBody(ctx, session)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &loginOutput{SetCookie: s.sessionCookies(session)}
		user := body.User
		prefs := body.Preferences
		expires := body.ExpiresAt
		out.Body.User = &user
		out.Body.Orgs = body.Orgs
		out.Body.CSRF = body.CSRF
		out.Body.ExpiresAt = &expires
		out.Body.Preferences = &prefs
		return out, nil
	})

	if err := s.registry.DeclareAuthenticated(http.MethodPost, "/api/v1/auth/verify/request", "sends a verification email to the caller's own address"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "request-email-verification", Method: http.MethodPost, Path: "/api/v1/auth/verify/request",
		Summary: "Send a verification email", Tags: []string{"auth"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, _ *struct{}) (*emptyOutput, error) {
		principal, ok := principalFrom(ctx)
		if !ok {
			return nil, withRequestID(ctx, problem.Unauthorized("Sign in to continue."))
		}
		token, err := s.auth.CreateEmailVerification(ctx, principal.User)
		if err != nil {
			if errors.Is(err, authn.ErrAlreadyVerified) {
				return &emptyOutput{}, nil
			}
			return nil, s.apiError(ctx, err)
		}
		verifyURL := s.cfg.Server.BaseURL + "/verify?token=" + url.QueryEscape(token)
		switch {
		case s.mailer != nil:
			if err := s.mailer.Send(ctx, email.VerifyMessage(principal.User.Email, verifyURL)); err != nil {
				s.logger.Error("send verification email failed", "user_id", principal.User.ID, "error", err)
			}
		case s.cfg.Server.Dev:
			s.logger.Warn("SMTP is disabled; verification link printed for development", "email", principal.User.Email, "url", verifyURL)
		default:
			s.logger.Warn("SMTP is disabled; verification email could not be delivered", "user_id", principal.User.ID)
		}
		return &emptyOutput{}, nil
	})

	if err := s.registry.DeclarePublic(http.MethodPost, "/api/v1/auth/verify", "the verification token is the credential for confirming an email"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "verify-email", Method: http.MethodPost, Path: "/api/v1/auth/verify",
		Summary: "Confirm an email address", Tags: []string{"auth"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *verifyEmailInput) (*emptyOutput, error) {
		userID, err := s.auth.VerifyEmail(ctx, in.Body.Token)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		if user, err := s.store.GetUser(ctx, userID); err == nil {
			s.svc.RecordAuthEvent(ctx, user, "auth.email_verified", "email", metaFrom(ctx))
		}
		return &emptyOutput{}, nil
	})

	if err := s.registry.DeclarePublic(http.MethodPost, "/api/v1/auth/mfa/verify", "the login challenge and second factor authorize this exchange"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "verify-mfa", Method: http.MethodPost, Path: "/api/v1/auth/mfa/verify",
		Summary: "Complete an MFA login", Tags: []string{"auth"},
	}, func(ctx context.Context, in *mfaVerifyInput) (*sessionOutput, error) {
		meta := metaFrom(ctx)
		session, err := s.auth.CompleteMFA(ctx, in.Body.Challenge, in.Body.Code, in.Body.RecoveryCode, meta.IP, meta.UserAgent)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		s.svc.RecordAuthEvent(ctx, session.User, "auth.login", "mfa", meta)
		body, err := s.sessionBody(ctx, session)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &sessionOutput{SetCookie: s.sessionCookies(session), Body: body}, nil
	})

	if err := s.registry.DeclareAuthenticated(http.MethodGet, "/api/v1/auth/mfa", "returns the caller's own MFA state"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "mfa-status", Method: http.MethodGet, Path: "/api/v1/auth/mfa",
		Summary: "MFA status", Tags: []string{"auth"},
	}, func(ctx context.Context, _ *struct{}) (*mfaStatusOutput, error) {
		principal, ok := principalFrom(ctx)
		if !ok {
			return nil, withRequestID(ctx, problem.Unauthorized("Sign in to continue."))
		}
		status, err := s.auth.MFAStatus(ctx, principal.User)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &mfaStatusOutput{}
		out.Body.Enabled = status.Enabled
		out.Body.RecoveryCodesRemaining = status.RecoveryCodes
		return out, nil
	})

	if err := s.registry.DeclareAuthenticated(http.MethodPost, "/api/v1/auth/mfa/totp/enroll", "starts TOTP enrollment for the caller"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "enroll-mfa", Method: http.MethodPost, Path: "/api/v1/auth/mfa/totp/enroll",
		Summary: "Start TOTP enrollment", Tags: []string{"auth"},
	}, func(ctx context.Context, _ *struct{}) (*mfaEnrollOutput, error) {
		principal, ok := principalFrom(ctx)
		if !ok {
			return nil, withRequestID(ctx, problem.Unauthorized("Sign in to continue."))
		}
		enrollment, err := s.auth.EnrollMFA(ctx, principal.User)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &mfaEnrollOutput{}
		out.Body.Secret = enrollment.Secret
		out.Body.URL = enrollment.URL
		return out, nil
	})

	if err := s.registry.DeclareAuthenticated(http.MethodPost, "/api/v1/auth/mfa/totp/activate", "confirms TOTP enrollment for the caller"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "activate-mfa", Method: http.MethodPost, Path: "/api/v1/auth/mfa/totp/activate",
		Summary: "Activate TOTP and return recovery codes", Tags: []string{"auth"},
	}, func(ctx context.Context, in *mfaCodeInput) (*mfaRecoveryOutput, error) {
		principal, ok := principalFrom(ctx)
		if !ok {
			return nil, withRequestID(ctx, problem.Unauthorized("Sign in to continue."))
		}
		codes, err := s.auth.ActivateMFA(ctx, principal.User, in.Body.Code)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &mfaRecoveryOutput{}
		out.Body.RecoveryCodes = codes
		return out, nil
	})

	if err := s.registry.DeclareAuthenticated(http.MethodPost, "/api/v1/auth/mfa/totp/disable", "disables TOTP after code verification"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "disable-mfa", Method: http.MethodPost, Path: "/api/v1/auth/mfa/totp/disable",
		Summary: "Disable TOTP", Tags: []string{"auth"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *mfaCodeInput) (*emptyOutput, error) {
		principal, ok := principalFrom(ctx)
		if !ok {
			return nil, withRequestID(ctx, problem.Unauthorized("Sign in to continue."))
		}
		if err := s.auth.DisableMFA(ctx, principal.User, in.Body.Code); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})

	if err := s.registry.DeclareAuthenticated(http.MethodPost, "/api/v1/auth/mfa/recovery-codes", "regenerates recovery codes after code verification"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "regenerate-recovery-codes", Method: http.MethodPost, Path: "/api/v1/auth/mfa/recovery-codes",
		Summary: "Regenerate recovery codes", Tags: []string{"auth"},
	}, func(ctx context.Context, in *mfaCodeInput) (*mfaRecoveryOutput, error) {
		principal, ok := principalFrom(ctx)
		if !ok {
			return nil, withRequestID(ctx, problem.Unauthorized("Sign in to continue."))
		}
		codes, err := s.auth.RegenerateRecoveryCodes(ctx, principal.User, in.Body.Code)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &mfaRecoveryOutput{}
		out.Body.RecoveryCodes = codes
		return out, nil
	})

	if err := s.registry.DeclarePublic(http.MethodPost, "/api/v1/auth/password/forgot", "starts a password reset; the response is identical for known and unknown emails"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "forgot-password", Method: http.MethodPost, Path: "/api/v1/auth/password/forgot",
		Summary: "Request a password reset", Tags: []string{"auth"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *forgotPasswordInput) (*emptyOutput, error) {
		meta := metaFrom(ctx)
		token, user, err := s.auth.ForgotPassword(ctx, in.Body.Email, meta.IP)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		if token != "" && user != nil {
			s.svc.RecordAuthEvent(ctx, user, "auth.password_reset_requested", "email", meta)
			resetURL := s.cfg.Server.BaseURL + "/reset?token=" + url.QueryEscape(token)
			switch {
			case s.mailer != nil:
				msg := email.ResetMessage(user.Email, resetURL)
				if err := s.mailer.Send(ctx, msg); err != nil {
					s.logger.Error("send password reset email failed", "user_id", user.ID, "error", err)
				}
			case s.cfg.Server.Dev:
				s.logger.Warn("SMTP is disabled; password reset link printed for development", "email", user.Email, "url", resetURL)
			default:
				s.logger.Warn("SMTP is disabled; password reset requested but no email could be delivered", "user_id", user.ID)
			}
		}
		return &emptyOutput{}, nil
	})

	if err := s.registry.DeclarePublic(http.MethodPost, "/api/v1/auth/password/reset", "the reset token is the credential for setting a new password"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "reset-password", Method: http.MethodPost, Path: "/api/v1/auth/password/reset",
		Summary: "Set a new password with a reset token", Tags: []string{"auth"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *resetPasswordInput) (*emptyOutput, error) {
		if err := s.svc.CheckPassword(ctx, in.Body.Password); err != nil {
			return nil, s.apiError(ctx, err)
		}
		userID, err := s.auth.ResetPassword(ctx, in.Body.Token, in.Body.Password)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		if user, err := s.store.GetUser(ctx, userID); err == nil {
			s.svc.RecordAuthEvent(ctx, user, "auth.password_reset", "password_reset", metaFrom(ctx))
		}
		return &emptyOutput{}, nil
	})

	if err := s.registry.DeclareAuthenticated(http.MethodGet, "/api/v1/auth/me", "returns the signed-in user and their orgs"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "me",
		Method:      http.MethodGet,
		Path:        "/api/v1/auth/me",
		Summary:     "Current user and organizations",
		Tags:        []string{"auth"},
	}, func(ctx context.Context, _ *struct{}) (*meOutput, error) {
		principal, ok := principalFrom(ctx)
		if !ok {
			return nil, withRequestID(ctx, problem.Unauthorized("Sign in to continue."))
		}
		body, err := s.sessionBody(ctx, &authn.SessionResult{
			Session:   principal.Session,
			User:      principal.User,
			ExpiresAt: principal.Session.ExpiresAt,
		})
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &meOutput{Body: body}, nil
	})

	if err := s.registry.DeclareAuthenticated(http.MethodPost, "/api/v1/auth/logout", "revokes the caller's own session"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID:   "logout",
		Method:        http.MethodPost,
		Path:          "/api/v1/auth/logout",
		Summary:       "Sign out",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, _ *struct{}) (*logoutOutput, error) {
		principal, ok := principalFrom(ctx)
		if !ok {
			return nil, withRequestID(ctx, problem.Unauthorized("Sign in to continue."))
		}
		if err := s.auth.Logout(ctx, principal.Session.ID); err != nil {
			return nil, s.apiError(ctx, err)
		}
		s.svc.RecordAuthEvent(ctx, principal.User, "auth.logout", "session", metaFrom(ctx))
		return &logoutOutput{SetCookie: s.clearedCookies()}, nil
	})

	if err := s.registry.DeclareAuthenticated(http.MethodGet, "/api/v1/auth/sessions", "lists the caller's own sessions"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-sessions",
		Method:      http.MethodGet,
		Path:        "/api/v1/auth/sessions",
		Summary:     "List signed-in devices",
		Tags:        []string{"auth"},
	}, func(ctx context.Context, _ *struct{}) (*sessionsOutput, error) {
		principal, ok := principalFrom(ctx)
		if !ok {
			return nil, withRequestID(ctx, problem.Unauthorized("Sign in to continue."))
		}
		sessions, err := s.auth.ListSessions(ctx, principal.User.ID)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &sessionsOutput{}
		for _, session := range sessions {
			out.Body.Sessions = append(out.Body.Sessions, sessionInfo{
				ID: session.ID, IP: session.IP, UserAgent: session.UserAgent,
				CreatedAt: session.CreatedAt, LastSeenAt: session.LastSeenAt,
				ExpiresAt: session.ExpiresAt, Current: session.ID == principal.Session.ID,
			})
		}
		return out, nil
	})

	type sessionPath struct {
		ID string `path:"id"`
	}
	if err := s.registry.DeclareAuthenticated(http.MethodDelete, "/api/v1/auth/sessions/{id}", "revokes one of the caller's own sessions"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID:   "revoke-session",
		Method:        http.MethodDelete,
		Path:          "/api/v1/auth/sessions/{id}",
		Summary:       "Revoke a session",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *sessionPath) (*logoutOutput, error) {
		principal, ok := principalFrom(ctx)
		if !ok {
			return nil, withRequestID(ctx, problem.Unauthorized("Sign in to continue."))
		}
		sessions, err := s.auth.ListSessions(ctx, principal.User.ID)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		found := false
		for _, session := range sessions {
			if session.ID == in.ID {
				found = true
				break
			}
		}
		if !found {
			return nil, withRequestID(ctx, problem.NotFound("Session not found."))
		}
		if err := s.auth.Logout(ctx, in.ID); err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &logoutOutput{}
		if in.ID == principal.Session.ID {
			out.SetCookie = s.clearedCookies()
		}
		return out, nil
	})

	if err := s.registry.DeclareAuthenticated(http.MethodPost, "/api/v1/auth/sessions/revoke-all", "revokes all of the caller's own sessions"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID:   "revoke-all-sessions",
		Method:        http.MethodPost,
		Path:          "/api/v1/auth/sessions/revoke-all",
		Summary:       "Sign out everywhere",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, _ *struct{}) (*logoutOutput, error) {
		principal, ok := principalFrom(ctx)
		if !ok {
			return nil, withRequestID(ctx, problem.Unauthorized("Sign in to continue."))
		}
		if _, err := s.auth.RevokeAllSessions(ctx, principal.User.ID); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &logoutOutput{SetCookie: s.clearedCookies()}, nil
	})

	if err := s.registry.DeclarePublic(http.MethodPost, "/api/v1/auth/device/code", "starting a device login is safe and rate limited"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "device-code", Method: http.MethodPost, Path: "/api/v1/auth/device/code",
		Summary: "Start a CLI device login", Tags: []string{"auth"},
	}, func(ctx context.Context, _ *struct{}) (*deviceCodeOutput, error) {
		meta := metaFrom(ctx)
		grant, err := s.auth.StartDevice(ctx, meta.IP)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &deviceCodeOutput{}
		out.Body.DeviceCode = grant.DeviceCode
		out.Body.UserCode = grant.UserCode
		out.Body.VerificationURI = s.cfg.Server.BaseURL + "/device?code=" + url.QueryEscape(grant.UserCode)
		out.Body.ExpiresIn = int(time.Until(grant.ExpiresAt).Seconds())
		out.Body.Interval = int(grant.Interval.Seconds())
		return out, nil
	})

	if err := s.registry.DeclarePublic(http.MethodPost, "/api/v1/auth/device/token", "the device code is the bearer secret for this exchange"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "device-token", Method: http.MethodPost, Path: "/api/v1/auth/device/token",
		Summary: "Poll a device login", Tags: []string{"auth"},
	}, func(ctx context.Context, in *deviceTokenInput) (*deviceTokenOutput, error) {
		meta := metaFrom(ctx)
		result, err := s.auth.ExchangeDevice(ctx, in.Body.DeviceCode, meta.IP, meta.UserAgent)
		if errors.Is(err, authn.ErrDevicePending) {
			out := &deviceTokenOutput{}
			out.Body.Status = "pending"
			return out, nil
		}
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		s.svc.RecordAuthEvent(ctx, result.User, "auth.login", "device", meta)
		body, err := s.sessionBody(ctx, result)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &deviceTokenOutput{}
		out.Body.Status = "approved"
		out.Body.Token = result.Token
		out.Body.CSRF = result.CSRF
		out.Body.ExpiresAt = result.ExpiresAt
		u := userFromStore(result.User)
		out.Body.User = &u
		out.Body.Orgs = body.Orgs
		return out, nil
	})

	if err := s.registry.DeclareAuthenticated(http.MethodPost, "/api/v1/auth/device/approve", "a signed-in human approves or denies a CLI login"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "device-approve", Method: http.MethodPost, Path: "/api/v1/auth/device/approve",
		Summary: "Approve a CLI login", Tags: []string{"auth"},
	}, func(ctx context.Context, in *deviceApproveInput) (*deviceApproveOutput, error) {
		principal, ok := principalFrom(ctx)
		if !ok {
			return nil, withRequestID(ctx, problem.Unauthorized("Sign in to continue."))
		}
		code := authn.NormalizeUserCode(in.Body.UserCode)
		if len(code) != 8 {
			return nil, withRequestID(ctx, problem.New(http.StatusUnprocessableEntity, "invalid_code", "Invalid code", "Enter the 8-character code shown by the CLI."))
		}
		var err error
		switch in.Body.Action {
		case "deny":
			err = s.auth.DenyDevice(in.Body.UserCode)
		default:
			err = s.auth.ApproveDevice(in.Body.UserCode, principal.User.ID)
		}
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &deviceApproveOutput{}
		out.Body.Status = "approved"
		if in.Body.Action == "deny" {
			out.Body.Status = "denied"
		}
		return out, nil
	})

	if err := s.registry.DeclareAuthenticated(http.MethodPatch, "/api/v1/auth/preferences", "update the caller's own appearance preferences"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "update-preferences", Method: http.MethodPatch, Path: "/api/v1/auth/preferences",
		Summary: "Update appearance preferences", Tags: []string{"auth"},
	}, func(ctx context.Context, in *preferencesInput) (*preferencesOutput, error) {
		principal, ok := principalFrom(ctx)
		if !ok {
			return nil, withRequestID(ctx, problem.Unauthorized("Sign in to continue."))
		}
		// Fields left out keep their stored value, so each setting can be saved on
		// its own.
		body := preferencesBody{Theme: "dark", Appearance: "classic"}
		if principal.User.Preferences != "" {
			_ = json.Unmarshal([]byte(principal.User.Preferences), &body)
		}
		if in.Body.Theme != "" {
			body.Theme = in.Body.Theme
		}
		if in.Body.Appearance != "" {
			body.Appearance = in.Body.Appearance
		}
		if in.Body.DateFormat != "" {
			body.DateFormat = in.Body.DateFormat
		}
		if in.Body.TimeFormat != "" {
			body.TimeFormat = in.Body.TimeFormat
		}
		if in.Body.Font != "" {
			body.Font = in.Body.Font
		}
		if in.Body.FontMono != "" {
			body.FontMono = in.Body.FontMono
		}
		if body.Theme != "dark" && body.Theme != "light" && body.Theme != "system" {
			return nil, withRequestID(ctx, problem.New(http.StatusUnprocessableEntity, "invalid_theme", "Invalid theme", "Use system, dark, or light."))
		}
		if body.Appearance != "classic" && body.Appearance != "warm" {
			return nil, withRequestID(ctx, problem.New(http.StatusUnprocessableEntity, "invalid_appearance", "Invalid appearance", "Use classic or warm."))
		}
		raw, _ := json.Marshal(body)
		if err := s.store.UpdateUserPreferences(ctx, principal.User.ID, string(raw)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &preferencesOutput{Body: body}, nil
	})

	return nil
}

type verifyEmailInput struct {
	Body struct {
		Token string `json:"token" minLength:"1" maxLength:"256"`
	}
}

type forgotPasswordInput struct {
	Body struct {
		Email string `json:"email" format:"email"`
	}
}

type resetPasswordInput struct {
	Body struct {
		Token    string `json:"token" minLength:"1" maxLength:"256"`
		Password string `json:"password" minLength:"12" maxLength:"1024"`
	}
}

type loginOutput struct {
	SetCookie []http.Cookie `header:"Set-Cookie"`
	Body      struct {
		MFARequired bool             `json:"mfa_required,omitempty"`
		Challenge   string           `json:"challenge,omitempty"`
		User        *userBody        `json:"user,omitempty"`
		Orgs        []orgBody        `json:"orgs,omitempty"`
		CSRF        string           `json:"csrf_token,omitempty"`
		ExpiresAt   *time.Time       `json:"expires_at,omitempty"`
		Preferences *preferencesBody `json:"preferences,omitempty"`
	}
}

type mfaVerifyInput struct {
	Body struct {
		Challenge    string `json:"challenge" minLength:"1" maxLength:"256"`
		Code         string `json:"code,omitempty" maxLength:"10"`
		RecoveryCode string `json:"recovery_code,omitempty" maxLength:"32"`
	}
}

type mfaCodeInput struct {
	Body struct {
		Code string `json:"code" minLength:"6" maxLength:"10"`
	}
}

type mfaStatusOutput struct {
	Body struct {
		Enabled                bool `json:"enabled"`
		RecoveryCodesRemaining int  `json:"recovery_codes_remaining"`
	}
}

type mfaEnrollOutput struct {
	Body struct {
		Secret string `json:"secret"`
		URL    string `json:"url"`
	}
}

type mfaRecoveryOutput struct {
	Body struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
}

type preferencesInput struct {
	Body struct {
		Theme      string `json:"theme,omitempty" enum:"system,dark,light"`
		Appearance string `json:"appearance,omitempty" enum:"classic,warm"`
		DateFormat string `json:"date_format,omitempty" enum:"auto,dmy-dot,dmy-slash,mdy,iso" doc:"Date format."`
		TimeFormat string `json:"time_format,omitempty" enum:"auto,24h,12h" doc:"Time format."`
		Font       string `json:"font,omitempty" maxLength:"32" pattern:"^[a-z0-9-]*$" doc:"Interface font id."`
		FontMono   string `json:"font_mono,omitempty" maxLength:"32" pattern:"^[a-z0-9-]*$" doc:"Code font id."`
	}
}

type preferencesOutput struct {
	Body preferencesBody
}

type deviceCodeOutput struct {
	Body struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
}

type deviceTokenInput struct {
	Body struct {
		DeviceCode string `json:"device_code" minLength:"1" maxLength:"256"`
	}
}

type deviceTokenOutput struct {
	Body struct {
		Status    string    `json:"status"`
		Token     string    `json:"token,omitempty"`
		CSRF      string    `json:"csrf_token,omitempty"`
		ExpiresAt time.Time `json:"expires_at,omitempty"`
		User      *userBody `json:"user,omitempty"`
		Orgs      []orgBody `json:"orgs,omitempty"`
	}
}

type deviceApproveInput struct {
	Body struct {
		UserCode string `json:"user_code" minLength:"1" maxLength:"20"`
		Action   string `json:"action,omitempty" enum:"approve,deny" default:"approve"`
	}
}

type deviceApproveOutput struct {
	Body struct {
		Status string `json:"status"`
	}
}

// sessionBody builds the login/me payload.
func (s *Server) sessionBody(ctx context.Context, result *authn.SessionResult) (sessionBody, error) {
	body := sessionBody{
		User:      userFromStore(result.User),
		CSRF:      result.CSRF,
		ExpiresAt: result.ExpiresAt,
	}
	memberships, err := s.svc.ListOrgs(ctx, result.User.ID)
	if err != nil {
		return body, err
	}
	for _, m := range memberships {
		body.Orgs = append(body.Orgs, orgFromStore(&m.Org, m.RoleName))
	}
	body.Preferences = preferencesBody{Theme: "dark", Appearance: "classic"}
	if result.User.Preferences != "" {
		_ = json.Unmarshal([]byte(result.User.Preferences), &body.Preferences)
	}
	if body.Preferences.Theme == "" {
		body.Preferences.Theme = "dark"
	}
	if body.Preferences.Appearance == "" {
		body.Preferences.Appearance = "classic"
	}
	return body, nil
}

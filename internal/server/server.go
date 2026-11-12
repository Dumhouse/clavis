// Package server wires the HTTP API, middleware, health endpoints, metrics,
// and the embedded single-page app.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/auditexport"
	"github.com/dumhouse/clavis/internal/authn"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/breach"
	"github.com/dumhouse/clavis/internal/config"
	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/email"
	"github.com/dumhouse/clavis/internal/events"
	"github.com/dumhouse/clavis/internal/metrics"
	"github.com/dumhouse/clavis/internal/netguard"
	"github.com/dumhouse/clavis/internal/ratelimit"
	"github.com/dumhouse/clavis/internal/service"
	"github.com/dumhouse/clavis/internal/store"
	"github.com/dumhouse/clavis/internal/version"
	"github.com/dumhouse/clavis/pkg/problem"
	"github.com/dumhouse/clavis/web"
)

// Server is the Clavis HTTP server.
type Server struct {
	cfg        *config.Config
	store      *store.Store
	logger     *slog.Logger
	rootKey    crypto.Key
	keyring    *crypto.Keyring
	auth       *authn.Service
	svc        *service.Service
	bus        *events.Bus
	mailer     email.Sender
	limiter    *ratelimit.Limiter
	registry   *authz.Registry
	metrics    *metrics.Metrics
	router     chi.Router
	api        huma.API
	http       *http.Server
	webFS      fs.FS
	awsSTSURL  string
	instanceID string
}

// Option customizes the server.
type Option func(*Server)

// WithMailer injects a transactional email sender (tests and embedders).
func WithMailer(mailer email.Sender) Option {
	return func(s *Server) { s.mailer = mailer }
}

// WithWebFS replaces the embedded SPA filesystem (used by tests and
// alternative distributions).
// WithAWSSTSEndpoint overrides the STS endpoint (tests and private endpoints).
func WithAWSSTSEndpoint(endpoint string) Option {
	return func(s *Server) { s.awsSTSURL = endpoint }
}

func WithWebFS(fsys fs.FS) Option {
	return func(s *Server) {
		if fsys != nil {
			s.webFS = fsys
		}
	}
}

// New constructs a server. All routes must declare their authorization before
// the server can be created, so an undeclared route fails at startup (and in
// CI).
func New(cfg *config.Config, st *store.Store, logger *slog.Logger, rootKey crypto.Key, opts ...Option) (*Server, error) {
	if cfg == nil || st == nil || logger == nil {
		return nil, errors.New("server: config, store, and logger are required")
	}
	s := &Server{
		cfg:      cfg,
		store:    st,
		logger:   logger,
		rootKey:  rootKey,
		keyring:  crypto.NewKeyring(rootKey),
		registry: authz.NewRegistry(),
		metrics:  metrics.New(),
		limiter:  ratelimit.New(cfg.Server.RateLimitPerMinute, time.Minute),
		router:   chi.NewRouter(),
		webFS:    web.Dist(),
	}
	for _, opt := range opts {
		opt(s)
	}
	s.bus = events.NewBus()
	s.instanceID = uuid.NewString()
	s.svc = service.New(st, s.keyring, logger, s.bus)
	if st.IsPostgres() {
		s.bus.SetNotifier(func(orgID string, event events.Event) {
			payload, err := json.Marshal(struct {
				Origin string       `json:"origin"`
				OrgID  string       `json:"org_id"`
				Event  events.Event `json:"event"`
			}{Origin: s.instanceID, OrgID: orgID, Event: event})
			if err != nil {
				return
			}
			if err := st.NotifyEvent(context.Background(), string(payload)); err != nil {
				s.logger.Warn("publish event notification failed", "error", err)
			}
		})
	}
	signingKey, err := crypto.DeriveAuditSigningKey(rootKey)
	if err != nil {
		return nil, err
	}
	s.svc.SigningKey = signingKey
	s.svc.AWSSTSEndpoint = s.awsSTSURL
	ssoAllow := []string(nil)
	if cfg.Server.Dev {
		ssoAllow = []string{"127.0.0.1/32", "::1/128"}
	}
	ssoGuardian, ssoErr := netguard.New(ssoAllow)
	if ssoErr == nil {
		s.svc.SSOClient = ssoGuardian.Client()
		s.svc.WebhookClient = ssoGuardian.Client()
	}
	if len(cfg.Audit.Exporters) > 0 {
		exportGuardian, guardErr := netguard.New(ssoAllow)
		if guardErr != nil {
			return nil, guardErr
		}
		s.svc.AuditSink = auditexport.New(cfg.Audit.Exporters, exportGuardian.Client(), logger)
		logger.Info("audit exporters enabled", "count", len(cfg.Audit.Exporters))
	}
	if cfg.Security.BreachCheck {
		guardian, err := netguard.New(nil)
		if err != nil {
			return nil, err
		}
		checker := breach.New(cfg.Security.BreachCheckURL, guardian.Client())
		s.svc.BreachCheck = checker.Breached
		logger.Info("breached-password check enabled", "provider", cfg.Security.BreachCheckURL)
	}
	if cfg.Email.Enabled {
		s.mailer = &email.SMTPSender{
			Host: cfg.Email.Host, Port: cfg.Email.Port, Username: cfg.Email.Username,
			Password: cfg.Email.Password, From: cfg.Email.From, TLS: cfg.Email.TLS,
		}
		logger.Info("transactional email enabled", "host", cfg.Email.Host, "from", cfg.Email.From)
	}

	setupPath := filepath.Join(filepath.Dir(cfg.Database.Path), "setup-token")
	mfaKey, err := crypto.DeriveMFAKey(rootKey)
	if err != nil {
		return nil, err
	}
	authOpts := []authn.Option{authn.WithMFAKey(mfaKey), authn.WithPasswordGuard(s.svc.CheckSSORequired)}
	if parsed, parseErr := url.Parse(cfg.Server.BaseURL); parseErr == nil && parsed.Hostname() != "" {
		rpID := parsed.Hostname()
		if net.ParseIP(rpID) != nil {
			rpID = "localhost"
		}
		origins := []string{parsed.Scheme + "://" + parsed.Host}
		if cfg.Server.Dev {
			origins = append(origins, "http://localhost:5173", "http://127.0.0.1:5173")
		}
		rp, waErr := authn.NewWebAuthn(rpID, "Clavis", origins)
		if waErr != nil {
			logger.Warn("passkeys disabled", "error", waErr)
		} else {
			authOpts = append(authOpts, authn.WithWebAuthn(rp))
			logger.Info("passkeys enabled", "rp_id", rpID, "origins", origins)
		}
	}
	authSvc, err := authn.NewService(st, logger, setupPath, authOpts...)
	if err != nil {
		return nil, err
	}
	s.auth = authSvc
	if err := s.initSetupToken(context.Background()); err != nil {
		return nil, err
	}

	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.RealIP)
	s.router.Use(middleware.GetHead)
	s.router.Use(withRequestInfo)
	s.router.Use(s.observe)
	s.router.Use(s.recoverer)
	s.router.Use(s.securityHeaders)
	s.router.Use(s.bodyLimit)
	s.router.Use(s.authenticate)
	s.router.Use(s.csrf)
	s.router.Use(s.registry.Middleware(authz.New(), func(req *http.Request) (authz.Actor, bool) {
		if actor, ok := authz.ActorFromContext(req.Context()); ok {
			return actor, true
		}
		principal, ok := authn.PrincipalFromContext(req.Context())
		if !ok {
			return authz.Actor{}, false
		}
		actor, err := s.svc.ActorForUser(req.Context(), principal.User)
		if err != nil {
			s.logger.Error("build actor failed", "user_id", principal.User.ID, "error", err)
			return authz.Actor{}, false
		}
		return actor, true
	}, s.scopeFor))
	s.router.Use(s.rateLimit)

	if err := s.registerRoutes(); err != nil {
		return nil, err
	}
	if err := s.validateRouteCoverage(); err != nil {
		return nil, err
	}

	s.http = &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           s.router,
		ReadHeaderTimeout: cfg.Server.ReadTimeout.Duration(),
		ReadTimeout:       cfg.Server.ReadTimeout.Duration(),
		WriteTimeout:      cfg.Server.WriteTimeout.Duration(),
		IdleTimeout:       cfg.Server.IdleTimeout.Duration(),
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	return s, nil
}

func (s *Server) initSetupToken(ctx context.Context) error {
	required, err := s.auth.SetupRequired(ctx)
	if err != nil {
		return err
	}
	if !required {
		return nil
	}
	token, created, err := s.auth.EnsureSetupToken(ctx)
	if err != nil {
		return err
	}
	attrs := []any{
		"setup_url", s.cfg.Server.BaseURL + "/setup",
		"token_file", s.auth.SetupTokenPath(),
	}
	if created {
		attrs = append(attrs, "setup_token", token)
	}
	s.logger.Warn("first-run setup is required", attrs...)
	return nil
}

// scopeFor resolves org/project slugs to their stable IDs before authorization.
// A nil result denies access, which also hides whether a resource exists.
func (s *Server) scopeFor(req *http.Request, route authz.Route) (authz.Scope, bool) {
	params := authz.PathParams(route.Pattern, req.URL.Path)
	scope := authz.Scope{Environment: params["env"]}
	if slug := params["org"]; slug != "" {
		org, err := s.store.GetOrgBySlug(req.Context(), slug)
		if err != nil {
			return authz.Scope{}, false
		}
		settings := service.ParseOrgSettings(org.Settings)
		if !orgAllowsRequest(req, settings) {
			return authz.Scope{}, false
		}
		scope.OrgID = org.ID
		if projectSlug := params["project"]; projectSlug != "" {
			project, err := s.store.GetProjectBySlug(req.Context(), org.ID, projectSlug)
			if err != nil {
				return authz.Scope{}, false
			}
			scope.ProjectID = project.ID
		}
	}
	return scope, true
}

// orgAllowsRequest enforces org security policy: MFA requirement, IP
// allowlist, and per-org session lifetimes.
func orgAllowsRequest(req *http.Request, settings service.OrgSettings) bool {
	if len(settings.IPAllowlist) > 0 {
		addr, err := netip.ParseAddr(metaFrom(req.Context()).IP)
		if err != nil {
			return false
		}
		allowed := false
		for _, entry := range settings.IPAllowlist {
			prefix, err := netip.ParsePrefix(entry)
			if err == nil && prefix.Contains(addr) {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}
	principal, ok := authn.PrincipalFromContext(req.Context())
	if !ok {
		// Machine tokens have their own expiry and are not subject to MFA.
		return true
	}
	if settings.RequireMFA && !principal.User.MFAEnabled {
		return false
	}
	now := time.Now().UTC()
	lastSeen := principal.Session.LastSeenAt
	if !principal.Session.SeenBefore.IsZero() {
		lastSeen = principal.Session.SeenBefore
	}
	if settings.SessionIdleHours > 0 &&
		now.Sub(lastSeen) > time.Duration(settings.SessionIdleHours)*time.Hour {
		return false
	}
	if settings.SessionAbsoluteDays > 0 &&
		now.Sub(principal.Session.CreatedAt) > time.Duration(settings.SessionAbsoluteDays)*24*time.Hour {
		return false
	}
	return true
}

// Handler exposes the router for tests.
func (s *Server) Handler() http.Handler { return s.router }

// Registry exposes route declarations for tests and tooling.
func (s *Server) Registry() *authz.Registry { return s.registry }

func (s *Server) registerRoutes() error {
	s.api = humachi.New(s.router, apiConfig(s.cfg))

	for _, path := range []string{
		"/api/v1/openapi.json",
		"/api/v1/openapi-3.0.json",
		"/api/v1/openapi.yaml",
		"/api/v1/openapi-3.0.yaml",
	} {
		if err := s.registry.DeclarePublic(http.MethodGet, path, "the API contract is public documentation"); err != nil {
			return err
		}
	}

	// Public system endpoints.
	if err := s.registry.DeclarePublic(http.MethodGet, "/healthz", "liveness probe must work before authentication"); err != nil {
		return err
	}
	if err := s.registry.DeclarePublic(http.MethodGet, "/readyz", "readiness probe must work before authentication"); err != nil {
		return err
	}
	if s.cfg.Server.MetricsPublic {
		if err := s.registry.DeclarePublic(http.MethodGet, "/metrics", "Prometheus scraping is enabled by default in single-binary mode"); err != nil {
			return err
		}
	}
	if err := s.registry.DeclarePublic(http.MethodGet, "/*", "static SPA assets and client-side routes"); err != nil {
		return err
	}

	s.router.Get("/healthz", s.handleHealth)
	s.router.Get("/readyz", s.handleReady)
	if s.cfg.Server.MetricsPublic {
		s.router.Get("/metrics", s.metrics.Handler().ServeHTTP)
	}

	if err := s.registry.DeclarePublic(http.MethodGet, "/api/v1/version", "version disclosure is safe and helps clients detect drift"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "get-version",
		Method:      http.MethodGet,
		Path:        "/api/v1/version",
		Summary:     "Server and API version",
		Tags:        []string{"system"},
	}, func(_ context.Context, _ *struct{}) (*VersionOutput, error) {
		return &VersionOutput{Body: VersionBody{
			Name:       "clavis",
			Version:    version.Version,
			Commit:     version.Commit,
			APIVersion: "v1",
		}}, nil
	})

	// Everything else is the SPA. Unknown API paths get a JSON 404 instead of
	// index.html.
	if err := s.registerAuthRoutes(); err != nil {
		return err
	}
	if err := s.registerPasskeyRoutes(); err != nil {
		return err
	}
	if err := s.registerOrgRoutes(); err != nil {
		return err
	}
	if err := s.registerSecretRoutes(); err != nil {
		return err
	}
	if err := s.registerSchemaRoutes(); err != nil {
		return err
	}
	if err := s.registerPromotionRoutes(); err != nil {
		return err
	}
	if err := s.registerBranchRoutes(); err != nil {
		return err
	}
	if err := s.registerShareRoutes(); err != nil {
		return err
	}
	if err := s.registerOverlayRoutes(); err != nil {
		return err
	}
	if err := s.registerDynamicRoutes(); err != nil {
		return err
	}
	if err := s.registerRotationRoutes(); err != nil {
		return err
	}
	if err := s.registerAccessRequestRoutes(); err != nil {
		return err
	}
	if err := s.registerFreezeRoutes(); err != nil {
		return err
	}
	if err := s.registerHealthRoutes(); err != nil {
		return err
	}
	if err := s.registerOffboardingRoutes(); err != nil {
		return err
	}
	if err := s.registerAuditRoutes(); err != nil {
		return err
	}
	if err := s.registerMemberRoutes(); err != nil {
		return err
	}
	if err := s.registerIdentityRoutes(); err != nil {
		return err
	}
	if err := s.registerPersonalTokenRoutes(); err != nil {
		return err
	}
	if err := s.registerGroupRoutes(); err != nil {
		return err
	}
	if err := s.registerRoleRoutes(); err != nil {
		return err
	}
	if err := s.registerReauthRoutes(); err != nil {
		return err
	}
	if err := s.registerSnapshotRoutes(); err != nil {
		return err
	}
	if err := s.registerScimTokenRoutes(); err != nil {
		return err
	}
	if err := s.registerWebhookRoutes(); err != nil {
		return err
	}
	if err := s.registerSyncRoutes(); err != nil {
		return err
	}
	if err := s.registerSharedSetRoutes(); err != nil {
		return err
	}
	if err := s.registerAccessRoutes(); err != nil {
		return err
	}
	if err := s.registerAccessMatrixRoutes(); err != nil {
		return err
	}
	if err := s.registerFolderRoutes(); err != nil {
		return err
	}
	if err := s.registerEventRoute(); err != nil {
		return err
	}

	if err := s.registerScimRoutes(); err != nil {
		return err
	}
	if err := s.registerSSORoutes(); err != nil {
		return err
	}
	if err := s.registerSAMLRoutes(); err != nil {
		return err
	}
	if err := s.registerPreviewRoutes(); err != nil {
		return err
	}
	if err := s.registerESORoutes(); err != nil {
		return err
	}
	s.router.Get("/*", spaHandler(s.cfg, s.webFS).ServeHTTP)

	s.router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		p := problem.NotFound("No such resource.")
		p.RequestID = middleware.GetReqID(r.Context())
		problem.Write(w, p)
	})
	s.router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		p := problem.New(http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed", "")
		p.RequestID = middleware.GetReqID(r.Context())
		problem.Write(w, p)
	})

	return s.registry.Validate()
}

// VersionBody is returned by GET /api/v1/version.
type VersionBody struct {
	Name       string `json:"name" example:"clavis"`
	Version    string `json:"version" example:"0.1.0"`
	Commit     string `json:"commit,omitempty"`
	APIVersion string `json:"api_version" example:"v1"`
}

// VersionOutput wraps the version response body.
type VersionOutput struct {
	Body VersionBody
}

func apiConfig(cfg *config.Config) huma.Config {
	c := huma.DefaultConfig("Clavis API", version.Version)
	c.OpenAPIPath = "/api/v1/openapi"
	c.DocsPath = ""
	c.SchemasPath = ""
	c.CreateHooks = nil // no $schema links to a schemas endpoint we do not serve
	c.Servers = []*huma.Server{{URL: cfg.Server.BaseURL, Description: "This Clavis instance"}}
	c.Info.Description = "Secrets management for humans, machines, and agents."
	return c
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthBody{Status: "ok", Version: version.Version})
}

type healthBody struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

type readyBody struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	checks := map[string]string{
		"database": "ok",
		"root_key": "ok",
	}
	status := http.StatusOK
	if err := s.store.Ping(ctx); err != nil {
		checks["database"] = "unavailable"
		status = http.StatusServiceUnavailable
	}
	if s.rootKey == (crypto.Key{}) {
		checks["root_key"] = "unavailable"
		status = http.StatusServiceUnavailable
	}
	ready := "ok"
	if status != http.StatusOK {
		ready = "unavailable"
	}
	writeJSON(w, status, readyBody{Status: ready, Checks: checks})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (s *Server) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		elapsed := time.Since(start)

		route := ""
		if matched, ok := s.registry.Match(r.Method, r.URL.Path); ok {
			route = matched.Pattern
		}
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}
		s.metrics.Observe(r.Method, route, status, elapsed)
		s.logger.Info("http request",
			"request_id", middleware.GetReqID(r.Context()),
			"method", r.Method,
			"route", route,
			"status", status,
			"bytes", ww.BytesWritten(),
			"duration_ms", elapsed.Milliseconds(),
			"remote_addr", r.RemoteAddr,
		)
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.logger.Error("panic recovered",
					"request_id", middleware.GetReqID(r.Context()),
					"panic_type", fmt.Sprintf("%T", rec),
					"stack", string(debug.Stack()),
				)
				problem.Write(w, problem.Internal("An unexpected error occurred. Include the request ID when reporting this."))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		scriptSrc := "'self'"
		connectSrc := "'self'"
		if s.cfg.Server.Dev {
			// Vite injects its HMR preamble inline and talks over a WebSocket.
			scriptSrc = "'self' 'unsafe-inline'"
			connectSrc = "'self' ws://localhost:5173 http://localhost:5173"
		}
		h.Set("Content-Security-Policy", "default-src 'self'; script-src "+scriptSrc+"; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src "+connectSrc+"; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if r.TLS != nil || strings.HasPrefix(s.cfg.Server.BaseURL, "https://") {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) bodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, s.cfg.Server.MaxBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}

// listenEvents mirrors Postgres notifications into the local SSE bus.
func (s *Server) listenEvents(ctx context.Context) {
	for {
		err := s.store.ListenEvents(ctx, func(payload string) {
			var envelope struct {
				Origin string       `json:"origin"`
				OrgID  string       `json:"org_id"`
				Event  events.Event `json:"event"`
			}
			if json.Unmarshal([]byte(payload), &envelope) != nil || envelope.OrgID == "" {
				return
			}
			if envelope.Origin == s.instanceID {
				return
			}
			s.bus.PublishLocal(envelope.OrgID, envelope.Event)
		})
		if ctx.Err() != nil {
			return
		}
		s.logger.Warn("event listener stopped; reconnecting", "error", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

// Run serves until ctx is cancelled, then shuts down gracefully.
func (s *Server) Run(ctx context.Context) error {
	go s.expiryLoop(ctx)
	go s.leaseLoop(ctx)
	go s.rotationLoop(ctx)
	go s.rotationReminderLoop(ctx)
	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("server listening", "addr", s.cfg.Server.Addr, "base_url", s.cfg.Server.BaseURL, "version", version.Version)
		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.Server.ShutdownTimeout.Duration())
	defer cancel()
	s.logger.Info("shutting down")
	if err := s.http.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server: shutdown: %w", err)
	}
	return nil
}

func (s *Server) rotationReminderLoop(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		if time.Now().UTC().Weekday() == time.Monday {
			s.sendRotationReminders(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) sendRotationReminders(ctx context.Context) {
	if s.mailer == nil {
		return
	}
	reminders := s.svc.ListManualRotationReminders(ctx, 90)
	byEmail := map[string][]service.ManualRotationReminder{}
	for _, reminder := range reminders {
		for _, address := range reminder.Emails {
			byEmail[address] = append(byEmail[address], reminder)
		}
	}
	for address, items := range byEmail {
		var text strings.Builder
		text.WriteString("These secrets have no rotation policy and have not changed recently:\n\n")
		for _, item := range items {
			fmt.Fprintf(&text, "- %s in %s/%s/%s: %d days old\n",
				item.Key, item.OrgSlug, item.ProjectSlug, item.EnvSlug, item.AgeDays)
		}
		text.WriteString("\nAdd a rotation policy or rotate and mark the secret safe.\n")
		msg := email.Message{To: address, Subject: "Clavis secrets need rotation", Text: text.String()}
		if err := s.mailer.Send(ctx, msg); err != nil {
			s.logger.Warn("rotation reminder email failed", "to", address, "error", err)
		}
	}
}

func (s *Server) rotationLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		s.svc.RunDueRotations(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) leaseLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		s.svc.RevokeExpiredLeases(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) expiryLoop(ctx context.Context) {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		s.sendExpiryReminders(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) sendExpiryReminders(ctx context.Context) {
	if s.mailer == nil {
		return
	}
	now := time.Now().UTC()
	reminders, err := s.svc.ListExpiringSecrets(ctx, now.Add(31*24*time.Hour))
	if err != nil {
		s.logger.Error("expiry reminder scan failed", "error", err)
		return
	}
	byEmail := map[string][]service.ExpiringSecret{}
	for _, reminder := range reminders {
		days := int(math.Ceil(reminder.ExpiresAt.Sub(now).Hours() / 24))
		if days != 30 && days != 7 && days != 1 {
			continue
		}
		for _, address := range reminder.Emails {
			byEmail[address] = append(byEmail[address], reminder)
		}
	}
	for address, items := range byEmail {
		var text strings.Builder
		text.WriteString("These secrets are approaching their expiry date:\n\n")
		for _, item := range items {
			days := int(math.Ceil(item.ExpiresAt.Sub(now).Hours() / 24))
			fmt.Fprintf(&text, "- %s in %s/%s expires in %d day(s) (%s)\n",
				item.Key, item.ProjectSlug, item.EnvSlug, days, item.ExpiresAt.Format("2006-01-02"))
		}
		text.WriteString("\nRotate or extend them, or remove the expiry date if they are permanent.\n")
		msg := email.Message{
			To:      address,
			Subject: "Clavis secrets expiring soon",
			Text:    text.String(),
		}
		if err := s.mailer.Send(ctx, msg); err != nil {
			s.logger.Warn("expiry reminder email failed", "to", address, "error", err)
		}
	}
}

// Close immediately closes the listener. Prefer Run's graceful shutdown.
func (s *Server) Close() error { return s.http.Close() }

// Addr returns the listener address.
func (s *Server) Addr() string { return s.http.Addr }

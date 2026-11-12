package server

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/dumhouse/clavis/internal/service"
)

// registerSSORoutes mounts browser redirect endpoints for organization SSO.
func (s *Server) registerSSORoutes() error {
	if err := s.registry.DeclarePublic(http.MethodGet, "/api/v1/auth/sso/{org}/start", "SSO sign-in starts with a browser redirect"); err != nil {
		return err
	}
	if err := s.registry.DeclarePublic(http.MethodGet, "/api/v1/auth/sso/{org}/callback", "the provider callback carries state and a one-time code"); err != nil {
		return err
	}
	s.router.Get("/api/v1/auth/sso/{org}/start", s.handleSSOStart)
	s.router.Get("/api/v1/auth/sso/{org}/callback", s.handleSSOCallback)
	return nil
}

func (s *Server) ssoRedirectURI(org string) string {
	return strings.TrimRight(s.cfg.Server.BaseURL, "/") + "/api/v1/auth/sso/" + url.PathEscape(org) + "/callback"
}

func (s *Server) handleSSOStart(w http.ResponseWriter, r *http.Request) {
	org := chi.URLParam(r, "org")
	orgRecord, err := s.store.GetOrgBySlug(r.Context(), org)
	if err != nil {
		s.redirectSSOError(w, r, service.ErrSSOUnavailable)
		return
	}
	settings := service.ParseOrgSettings(orgRecord.Settings)
	var target string
	if settings.SSOProvider == "saml" {
		target, err = s.svc.BeginSAML(r.Context(), org, s.cfg.Server.BaseURL, r.URL.Query().Get("return"))
	} else {
		target, err = s.svc.BeginSSO(r.Context(), org, s.ssoRedirectURI(org), r.URL.Query().Get("return"))
	}
	if err != nil {
		s.redirectSSOError(w, r, err)
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (s *Server) handleSSOCallback(w http.ResponseWriter, r *http.Request) {
	org := chi.URLParam(r, "org")
	user, _, returnPath, err := s.svc.FinishSSO(r.Context(), r.URL.Query().Get("state"),
		r.URL.Query().Get("code"), s.ssoRedirectURI(org), metaFrom(r.Context()))
	if err != nil {
		s.redirectSSOError(w, r, err)
		return
	}
	meta := metaFrom(r.Context())
	session, err := s.auth.CreateSession(r.Context(), user, meta.IP, meta.UserAgent)
	if err != nil {
		s.redirectSSOError(w, r, err)
		return
	}
	for _, cookie := range s.sessionCookies(session) {
		http.SetCookie(w, &cookie)
	}
	if returnPath == "" {
		returnPath = "/"
	}
	http.Redirect(w, r, returnPath, http.StatusFound)
}

func (s *Server) redirectSSOError(w http.ResponseWriter, r *http.Request, err error) {
	code := "sso_failed"
	if err != nil && strings.Contains(err.Error(), "required") {
		code = "sso_required"
	}
	if err != nil && strings.Contains(err.Error(), "unavailable") {
		code = "sso_unavailable"
	}
	s.logger.Warn("sso error", "code", code, "error", err, "path", r.URL.Path)
	http.Redirect(w, r, "/?sso_error="+url.QueryEscape(code), http.StatusFound)
}

package server

import (
	"encoding/xml"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// registerSAMLRoutes mounts SP metadata, login, and assertion consumer
// endpoints for organizations using SAML.
func (s *Server) registerSAMLRoutes() error {
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/saml/{org}/metadata"},
		{http.MethodGet, "/saml/{org}/login"},
		{http.MethodPost, "/saml/{org}/acs"},
	} {
		if err := s.registry.DeclarePublic(route.method, route.path, "SAML endpoints are authenticated by signatures or are public metadata"); err != nil {
			return err
		}
	}
	s.router.Get("/saml/{org}/metadata", s.handleSAMLMetadata)
	s.router.Get("/saml/{org}/login", s.handleSAMLLogin)
	s.router.Post("/saml/{org}/acs", s.handleSAMLACS)
	return nil
}

func (s *Server) handleSAMLMetadata(w http.ResponseWriter, r *http.Request) {
	org := chi.URLParam(r, "org")
	sp, _, _, err := s.svc.SAMLServiceProvider(r.Context(), org, s.cfg.Server.BaseURL)
	if err != nil {
		http.Error(w, "SAML metadata is unavailable", http.StatusNotFound)
		return
	}
	encoded, err := xml.MarshalIndent(sp.Metadata(), "", "  ")
	if err != nil {
		http.Error(w, "could not render metadata", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/samlmetadata+xml")
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(encoded)
}

func (s *Server) handleSAMLLogin(w http.ResponseWriter, r *http.Request) {
	org := chi.URLParam(r, "org")
	target, err := s.svc.BeginSAML(r.Context(), org, s.cfg.Server.BaseURL, r.URL.Query().Get("return"))
	if err != nil {
		s.logger.Warn("SAML start failed", "org", org, "error", err)
		http.Redirect(w, r, "/?sso_error=saml_unavailable", http.StatusFound)
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (s *Server) handleSAMLACS(w http.ResponseWriter, r *http.Request) {
	org := chi.URLParam(r, "org")
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/?sso_error=saml_failed", http.StatusFound)
		return
	}
	user, _, returnPath, err := s.svc.FinishSAML(r.Context(), org, s.cfg.Server.BaseURL, r, metaFrom(r.Context()))
	if err != nil {
		s.logger.Warn("SAML assertion failed", "org", org, "error", err)
		http.Redirect(w, r, "/?sso_error=saml_failed", http.StatusFound)
		return
	}
	meta := metaFrom(r.Context())
	session, err := s.auth.CreateSession(r.Context(), user, meta.IP, meta.UserAgent)
	if err != nil {
		http.Redirect(w, r, "/?sso_error=saml_failed", http.StatusFound)
		return
	}
	for _, cookie := range s.sessionCookies(session) {
		http.SetCookie(w, &cookie)
	}
	if returnPath == "" || !strings.HasPrefix(returnPath, "/") || strings.HasPrefix(returnPath, "//") {
		returnPath = "/"
	}
	http.Redirect(w, r, returnPath, http.StatusFound)
}

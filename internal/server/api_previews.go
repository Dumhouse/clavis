package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/dumhouse/clavis/internal/service"
)

func (s *Server) registerPreviewRoutes() error {
	path := "/api/v1/orgs/{org}/projects/{project}/integrations/github/webhook"
	if err := s.registry.DeclarePublic(http.MethodPost, path, "GitHub webhooks authenticate with an HMAC signature"); err != nil {
		return err
	}
	s.router.Post(path, s.handleGitHubWebhook)
	return nil
}

func (s *Server) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		http.Error(w, "could not read body", http.StatusBadRequest)
		return
	}
	result, err := s.svc.HandleGitHubWebhook(r.Context(), chi.URLParam(r, "org"), chi.URLParam(r, "project"),
		r.Header.Get("X-GitHub-Event"), body, r.Header.Get("X-Hub-Signature-256"), metaFrom(r.Context()))
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, service.ErrForbidden):
			status = http.StatusUnauthorized
		case errors.Is(err, service.ErrNotFound):
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"result": result})
}

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dumhouse/clavis/internal/authz"
)

// registerEventRoute adds the per-org SSE stream used by the web app.
func (s *Server) registerEventRoute() error {
	if err := s.registry.Declare(http.MethodGet, "/api/v1/orgs/{org}/events", authz.OrgRead, "subscribe to live updates for an organization"); err != nil {
		return err
	}
	s.router.Get("/api/v1/orgs/{org}/events", s.handleEvents)
	return nil
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	orgSlug := chi.URLParam(r, "org")
	org, err := s.store.GetOrgBySlug(r.Context(), orgSlug)
	if err != nil {
		http.Error(w, "organization not found", http.StatusNotFound)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, ": connected\n\n"); err != nil {
		return
	}
	flusher.Flush()

	events, cancel := s.bus.Subscribe(org.ID)
	defer cancel()

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case event, ok := <-events:
			if !ok {
				return
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, payload); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

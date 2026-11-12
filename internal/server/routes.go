package server

import (
	"fmt"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
)

// validateRouteCoverage fails if any registered route lacks an explicit
// authorization declaration. This is the enforcement behind M0-23: a route
// without a declared permission cannot reach production, and CI calls this via
// the server tests.
func (s *Server) validateRouteCoverage() error {
	var missing []string
	err := chi.Walk(s.router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if _, ok := s.registry.Lookup(method, route); !ok {
			missing = append(missing, method+" "+route)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("server: walk routes: %w", err)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("server: routes without an authorization declaration: %v", missing)
	}
	return nil
}

// RouteCoverage reports every route that lacks a declaration. It is used by
// tests and `clavis admin` diagnostics.
func (s *Server) RouteCoverage() []string {
	var missing []string
	_ = chi.Walk(s.router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if _, ok := s.registry.Lookup(method, route); !ok {
			missing = append(missing, method+" "+route)
		}
		return nil
	})
	sort.Strings(missing)
	return missing
}

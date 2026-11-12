package authz

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// Route describes one API route's authorization requirement.
type Route struct {
	Method      string
	Pattern     string
	Permission  Permission
	Public      bool
	AuthOnly    bool   // signed in, but no org-scoped permission required
	Reason      string // why a public route is safe to expose
	Description string
}

// Registry is the single source of truth for route permissions. The server
// refuses to start if a registered route has no declaration, which makes
// "deny by default" a build-time property rather than a convention.
type Registry struct {
	mu     sync.RWMutex
	routes map[string]Route
}

// NewRegistry returns an empty route registry.
func NewRegistry() *Registry {
	return &Registry{routes: make(map[string]Route)}
}

func routeKey(method, pattern string) string { return method + " " + pattern }

// Declare registers a protected route.
func (r *Registry) Declare(method, pattern string, perm Permission, description string) error {
	if !perm.Valid() {
		return fmt.Errorf("authz: route %s %s declares unknown permission %q", method, pattern, perm)
	}
	return r.add(Route{Method: method, Pattern: pattern, Permission: perm, Description: description})
}

// DeclarePublic registers a route that is intentionally reachable without
// authentication. A reason is required.
func (r *Registry) DeclarePublic(method, pattern, reason string) error {
	if reason == "" {
		return fmt.Errorf("authz: public route %s %s must explain why it is safe", method, pattern)
	}
	return r.add(Route{Method: method, Pattern: pattern, Public: true, Reason: reason})
}

// DeclareAuthenticated registers a route that requires a signed-in actor but no
// org-scoped permission (for example "list my organizations").
func (r *Registry) DeclareAuthenticated(method, pattern, description string) error {
	return r.add(Route{Method: method, Pattern: pattern, AuthOnly: true, Description: description})
}

func (r *Registry) add(route Route) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := routeKey(route.Method, route.Pattern)
	if existing, ok := r.routes[key]; ok {
		return fmt.Errorf("authz: duplicate route %s (declared as %q)", key, existing.Permission)
	}
	r.routes[key] = route
	return nil
}

// Lookup returns the declaration for method+pattern.
func (r *Registry) Lookup(method, pattern string) (Route, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	route, ok := r.routes[routeKey(method, pattern)]
	return route, ok
}

// Routes returns all declarations sorted by pattern then method.
func (r *Registry) Routes() []Route {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.snapshot()
}

func (r *Registry) snapshot() []Route {
	out := make([]Route, 0, len(r.routes))
	for _, route := range r.routes {
		out = append(out, route)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pattern == out[j].Pattern {
			return out[i].Method < out[j].Method
		}
		return out[i].Pattern < out[j].Pattern
	})
	return out
}

// Match finds the declaration for a concrete method and path. It understands
// chi patterns ({param} and /*) and prefers the most specific match, so a
// static route always wins over the SPA catch-all.
func (r *Registry) Match(method, urlPath string) (Route, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var (
		best      Route
		bestScore = -1
		found     bool
	)
	for _, route := range r.routes {
		if route.Method != method && !(method == http.MethodHead && route.Method == http.MethodGet) {
			continue
		}
		score, ok := matchRoute(route.Pattern, urlPath)
		if !ok {
			continue
		}
		if score > bestScore {
			best, bestScore, found = route, score, true
		}
	}
	return best, found
}

// MatchAnyMethod reports whether any declared route pattern matches urlPath,
// regardless of method. Used to let the router answer 405 for a known path
// rather than deny-by-default.
func (r *Registry) MatchAnyMethod(urlPath string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, route := range r.routes {
		if _, ok := matchRoute(route.Pattern, urlPath); ok {
			return true
		}
	}
	return false
}

// PathParams extracts {name} values from a concrete path using a route pattern.
func PathParams(pattern, urlPath string) map[string]string {
	pp := strings.Split(strings.Trim(pattern, "/"), "/")
	vv := strings.Split(strings.Trim(urlPath, "/"), "/")
	if pattern == "/*" {
		return nil
	}
	out := make(map[string]string)
	for i := 0; i < len(pp) && i < len(vv); i++ {
		if strings.HasPrefix(pp[i], "{") && strings.HasSuffix(pp[i], "}") {
			out[pp[i][1:len(pp[i])-1]] = vv[i]
		}
	}
	return out
}

func matchRoute(pattern, urlPath string) (int, bool) {
	if pattern == "/*" {
		return 0, true
	}
	if pattern == urlPath {
		return 1000 + len(pattern), true
	}
	pp := strings.Split(strings.Trim(pattern, "/"), "/")
	vv := strings.Split(strings.Trim(urlPath, "/"), "/")
	if pattern == "/" {
		return 0, urlPath == "/"
	}
	score := 0
	for i := 0; i < len(pp); i++ {
		if pp[i] == "*" {
			// Only valid as the final segment.
			if i != len(pp)-1 {
				return 0, false
			}
			return score, true
		}
		if i >= len(vv) {
			return 0, false
		}
		switch {
		case strings.HasPrefix(pp[i], "{") && strings.HasSuffix(pp[i], "}"):
			score += 2
		case pp[i] == vv[i]:
			score += 3
		default:
			return 0, false
		}
	}
	if len(pp) != len(vv) {
		return 0, false
	}
	return score, true
}

// Validate checks the registry's internal consistency.
func (r *Registry) Validate() error {
	for _, route := range r.Routes() {
		if route.Public {
			if route.Reason == "" {
				return fmt.Errorf("authz: public route %s %s has no reason", route.Method, route.Pattern)
			}
			continue
		}
		if route.AuthOnly {
			continue
		}
		if !route.Permission.Valid() {
			return fmt.Errorf("authz: route %s %s has invalid permission %q", route.Method, route.Pattern, route.Permission)
		}
	}
	return nil
}

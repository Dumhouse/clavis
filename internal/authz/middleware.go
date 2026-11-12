package authz

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/dumhouse/clavis/pkg/problem"
)

type actorKey struct{}

// WithActor stores an authenticated actor on the request context.
func WithActor(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

// ActorFromContext retrieves the actor set by authentication middleware.
func ActorFromContext(ctx context.Context) (Actor, bool) {
	actor, ok := ctx.Value(actorKey{}).(Actor)
	return actor, ok
}

// ActorResolver extracts an actor from a request. It returns false when the
// request is unauthenticated.
type ActorResolver func(*http.Request) (Actor, bool)

// ScopeResolver maps a concrete request to an authorization scope. Returning
// false denies the request (for example when the org in the URL does not
// exist). A nil resolver uses the raw URL params.
type ScopeResolver func(*http.Request, Route) (Scope, bool)

// Middleware enforces that every route has a declaration and that the caller
// holds its permission. It matches the concrete URL against declared patterns,
// so it does not depend on chi having run route matching yet (chi's global
// middleware runs before routeHTTP). Unmatched paths are denied by default.
func (r *Registry) Middleware(engine *Engine, resolve ActorResolver, scopeFor ScopeResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			route, ok := r.Match(req.Method, req.URL.Path)
			if !ok {
				if r.MatchAnyMethod(req.URL.Path) {
					// The path exists but not for this method; let the router
					// produce a 405 without exposing any data.
					next.ServeHTTP(w, req)
					return
				}
				p := problem.Forbidden("this route has no permission declaration and was blocked by default")
				p.Code = "route_not_declared"
				p.RequestID = middleware.GetReqID(req.Context())
				problem.Write(w, p)
				return
			}
			if route.Public {
				next.ServeHTTP(w, req)
				return
			}

			actor, ok := resolve(req)
			if !ok {
				p := problem.Unauthorized("Sign in to continue.")
				p.RequestID = middleware.GetReqID(req.Context())
				problem.Write(w, p)
				return
			}
			req = req.WithContext(WithActor(req.Context(), actor))
			if route.AuthOnly {
				next.ServeHTTP(w, req)
				return
			}

			params := PathParams(route.Pattern, req.URL.Path)
			scope := Scope{OrgID: params["org"], ProjectID: params["project"], Environment: params["env"]}
			if scopeFor != nil {
				resolved, ok := scopeFor(req, route)
				if !ok {
					p := problem.Forbidden("You do not have access to this resource.")
					p.RequestID = middleware.GetReqID(req.Context())
					problem.Write(w, p)
					return
				}
				scope = resolved
			}
			decision := engine.Authorize(actor, route.Permission, scope, time.Now())
			if !decision.Allowed {
				p := problem.Forbidden("You do not have permission to perform this action.")
				p.Code = "forbidden"
				p.RequestID = middleware.GetReqID(req.Context())
				problem.Write(w, p)
				return
			}
			next.ServeHTTP(w, req)
		})
	}
}

// IsAPIPath reports whether p belongs to the versioned API surface.
func IsAPIPath(p string) bool {
	return strings.HasPrefix(p, "/api/")
}

package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dumhouse/clavis/internal/authn"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/service"
	"github.com/dumhouse/clavis/pkg/problem"
)

type requestInfoKey struct{}

type requestInfo struct {
	IP        string
	UserAgent string
	RequestID string
}

func withRequestInfo(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		if host, _, err := net.SplitHostPort(ip); err == nil {
			ip = host
		}
		info := requestInfo{
			IP:        ip,
			UserAgent: r.UserAgent(),
			RequestID: requestID(r.Context()),
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestInfoKey{}, info)))
	})
}

func metaFrom(ctx context.Context) service.AuditMeta {
	info, _ := ctx.Value(requestInfoKey{}).(requestInfo)
	return service.AuditMeta{IP: info.IP, UserAgent: info.UserAgent, RequestID: info.RequestID}
}

func (s *Server) sessionCookieName() string {
	if s.cfg.Server.Dev {
		return "clavis_session"
	}
	return "__Host-clavis_session"
}

func (s *Server) csrfCookieName() string {
	if s.cfg.Server.Dev {
		return "clavis_csrf"
	}
	return "__Host-clavis_csrf"
}

func (s *Server) cookieSecure() bool {
	// __Host- cookies require Secure; production deployments terminate TLS at
	// the server or a reverse proxy. Dev mode uses a non-prefixed cookie.
	return !s.cfg.Server.Dev
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, cookieErr := r.Cookie(s.sessionCookieName())
		switch {
		case cookieErr == nil && cookie.Value != "":
			principal, authErr := s.auth.Authenticate(r.Context(), cookie.Value)
			switch {
			case authErr == nil:
				r = r.WithContext(authn.WithPrincipal(r.Context(), principal))
			case !errors.Is(authErr, authn.ErrUnauthenticated):
				s.logger.Error("session authentication failed", "error", authErr, "request_id", requestID(r.Context()))
			}
		default:
			// CLI and machine clients use bearer tokens.
			if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
				raw := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
				if strings.HasPrefix(raw, "clv_pat_") {
					if actor, tokenErr := s.svc.AuthenticatePersonalToken(r.Context(), raw, metaFrom(r.Context()).IP); tokenErr == nil {
						r = r.WithContext(authz.WithActor(r.Context(), actor))
					}
				} else if authn.IsClavisToken(raw) {
					if actor, tokenErr := s.svc.AuthenticateMachineToken(r.Context(), raw, metaFrom(r.Context()).IP); tokenErr == nil {
						r = r.WithContext(authz.WithActor(r.Context(), actor))
					}
				} else if principal, authErr := s.auth.AuthenticateBearer(r.Context(), raw); authErr == nil {
					r = r.WithContext(authn.WithPrincipal(r.Context(), principal))
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

var safeMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
	http.MethodTrace:   true,
}

func (s *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if safeMethods[r.Method] {
			next.ServeHTTP(w, r)
			return
		}
		// SAML assertions are posted cross-origin by the identity provider and
		// are authenticated by their XML signature, not by a CSRF token.
		if strings.HasPrefix(r.URL.Path, "/saml/") {
			next.ServeHTTP(w, r)
			return
		}
		// Origin checking protects login/setup too, before any session exists.
		if origin := r.Header.Get("Origin"); origin != "" && !originAllowed(origin, r.Host) {
			p := problem.Forbidden("Cross-origin request rejected.")
			p.Code = "csrf_origin"
			p.RequestID = requestID(r.Context())
			problem.Write(w, p)
			return
		}

		principal, ok := authn.PrincipalFromContext(r.Context())
		if !ok || principal.Via == "bearer" {
			next.ServeHTTP(w, r)
			return
		}
		header := r.Header.Get("X-CSRF-Token")
		cookie, err := r.Cookie(s.csrfCookieName())
		if err != nil || header == "" || cookie.Value == "" ||
			subtle.ConstantTimeCompare([]byte(header), []byte(cookie.Value)) != 1 ||
			!authn.ValidateCSRF(principal.Session, header) {
			p := problem.Forbidden("Missing or invalid CSRF token.")
			p.Code = "csrf_failed"
			p.RequestID = requestID(r.Context())
			problem.Write(w, p)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requestID(ctx context.Context) string {
	if info, ok := ctx.Value(requestInfoKey{}).(requestInfo); ok {
		return info.RequestID
	}
	return ""
}

// originAllowed reports whether origin matches the request host. Exposed for
// tests.
func originAllowed(origin, host string) bool {
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, host)
}

func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := "ip:" + metaFrom(r.Context()).IP
		if actor, ok := authz.ActorFromContext(r.Context()); ok {
			key = "actor:" + actor.ID
		}
		result := s.limiter.Allow(key)
		if result.Limit > 0 {
			w.Header().Set("RateLimit-Limit", strconv.Itoa(result.Limit))
			w.Header().Set("RateLimit-Remaining", strconv.Itoa(result.Remaining))
			if reset := int(time.Until(result.Reset).Seconds()); reset > 0 {
				w.Header().Set("RateLimit-Reset", strconv.Itoa(reset))
			}
		}
		if !result.Allowed {
			p := problem.New(http.StatusTooManyRequests, "rate_limited", "Too many requests", "Try again shortly.")
			p.RequestID = requestID(r.Context())
			problem.Write(w, p)
			return
		}
		next.ServeHTTP(w, r)
	})
}

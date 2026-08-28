package auth

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/ZephyrLeeX/RelayShelf/internal/platform/httpx"
)

type authContextKey struct{}

type AuthContext struct{ Authentication }

func FromContext(ctx context.Context) (AuthContext, bool) {
	value, ok := ctx.Value(authContextKey{}).(AuthContext)
	return value, ok
}

// ContextWithAuthentication is used by module routers and focused handler
// tests to install an already verified authentication. It does not perform or
// bypass authentication; callers must only pass a value verified by Middleware.
func ContextWithAuthentication(ctx context.Context, authentication Authentication) context.Context {
	return context.WithValue(ctx, authContextKey{}, AuthContext{Authentication: authentication})
}

type Middleware struct {
	service  *Service
	cookies  CookiePolicy
	csrf     *CSRF
	origin   *url.URL
	resolver *httpx.Resolver
}

func NewMiddleware(service *Service, cookies CookiePolicy, csrf *CSRF, origin *url.URL, resolver *httpx.Resolver) *Middleware {
	return &Middleware{service: service, cookies: cookies, csrf: csrf, origin: origin, resolver: resolver}
}

func (m *Middleware) Host(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, err := m.resolver.Resolve(r)
		if err != nil || !equalHost(info.Host, m.origin.Host) || info.Scheme != m.origin.Scheme {
			WriteError(w, r, http.StatusForbidden, "ORIGIN_INVALID", "request origin is not allowed")
			return
		}
		ctx := context.WithValue(r.Context(), requestInfoKey{}, info)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
func equalHost(a, b string) bool { return strings.EqualFold(a, b) }

type requestInfoKey struct{}

func RequestInfo(ctx context.Context) (httpx.RequestInfo, bool) {
	info, ok := ctx.Value(requestInfoKey{}).(httpx.RequestInfo)
	return info, ok
}

func (m *Middleware) Authenticate(touch bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(m.cookies.Name)
			if err != nil {
				WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
				return
			}
			info, _ := RequestInfo(r.Context())
			authn, err := m.service.Authenticate(r.Context(), cookie.Value, touch, info.ClientIP)
			if err != nil {
				code := "AUTH_REQUIRED"
				if err == ErrSessionExpired {
					code = "AUTH_SESSION_EXPIRED"
				}
				WriteError(w, r, http.StatusUnauthorized, code, "authentication required")
				return
			}
			ctx := context.WithValue(r.Context(), authContextKey{}, AuthContext{Authentication: authn})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func isSafe(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

func (m *Middleware) RequireSameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.validOrigin(r) {
			WriteError(w, r, http.StatusForbidden, "ORIGIN_INVALID", "request origin is not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (m *Middleware) CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authn, ok := FromContext(r.Context())
		if !ok {
			WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
			return
		}
		if !m.csrf.Verify(authn.Session.ID, r.Header.Get("X-CSRF-Token")) {
			WriteError(w, r, http.StatusForbidden, "CSRF_INVALID", "csrf validation failed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (m *Middleware) Touch(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authn, ok := FromContext(r.Context())
		if !ok {
			WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
			return
		}
		info, _ := RequestInfo(r.Context())
		updated, err := m.service.TouchAuthenticated(r.Context(), authn.Authentication, info.ClientIP)
		if err != nil {
			code := "AUTH_REQUIRED"
			if err == ErrSessionExpired {
				code = "AUTH_SESSION_EXPIRED"
			}
			WriteError(w, r, http.StatusUnauthorized, code, "authentication required")
			return
		}
		ctx := context.WithValue(r.Context(), authContextKey{}, AuthContext{Authentication: updated})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdmin is the single server-side authorization boundary for every
// operational admin endpoint. Authentication has already re-read ACTIVE user
// state, so disabled administrators are denied before this policy runs.
func (m *Middleware) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authn, ok := FromContext(r.Context())
		if !ok {
			WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
			return
		}
		if !authn.User.IsAdmin {
			WriteError(w, r, http.StatusForbidden, "ADMIN_REQUIRED", "administrator access required")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (m *Middleware) validOrigin(r *http.Request) bool {
	raw := r.Header.Get("Origin")
	if raw == "" {
		raw = r.Header.Get("Referer")
	}
	if raw == "" {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.User != nil {
		return false
	}
	return parsed.Scheme == m.origin.Scheme && strings.EqualFold(parsed.Host, m.origin.Host)
}

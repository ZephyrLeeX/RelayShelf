package auth

import (
	"net/http"
	"strings"

	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
)

func Router(handler any, middleware *Middleware) http.Handler {
	server, ok := handler.(httpapi.ServerInterface)
	if !ok {
		server = authServer{Handler: handler.(*Handler)}
	}
	api := httpapi.HandlerWithOptions(server, httpapi.ChiServerOptions{BaseURL: "/api/v1", ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
		WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "invalid request")
	}})
	safe := middleware.Authenticate(true)(api)
	stream := middleware.Authenticate(false)(api)
	unsafe := middleware.Authenticate(false)(middleware.RequireSameOrigin(middleware.CSRF(middleware.Touch(api))))
	adminSafe := middleware.Authenticate(true)(middleware.RequireAdmin(api))
	adminUnsafe := middleware.Authenticate(false)(middleware.RequireSameOrigin(middleware.CSRF(middleware.Touch(middleware.RequireAdmin(api)))))
	login := middleware.RequireSameOrigin(api)
	dispatch := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/admin/") {
			if isSafe(r.Method) {
				adminSafe.ServeHTTP(w, r)
			} else {
				adminUnsafe.ServeHTTP(w, r)
			}
			return
		}
		if r.URL.Path == "/api/v1/search" {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Pragma", "no-cache")
		}
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/login" {
			login.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/events" {
			stream.ServeHTTP(w, r)
			return
		}
		if isSafe(r.Method) {
			safe.ServeHTTP(w, r)
			return
		}
		unsafe.ServeHTTP(w, r)
	})
	return dispatch
}

// authServer keeps focused auth tests independent from later API modules.
type authServer struct {
	httpapi.Unimplemented
	Handler *Handler
}

func (s authServer) Login(w http.ResponseWriter, r *http.Request)  { s.Handler.Login(w, r) }
func (s authServer) Logout(w http.ResponseWriter, r *http.Request) { s.Handler.Logout(w, r) }
func (s authServer) GetAuthSession(w http.ResponseWriter, r *http.Request) {
	s.Handler.GetAuthSession(w, r)
}
func (s authServer) ChangePassword(w http.ResponseWriter, r *http.Request) {
	s.Handler.ChangePassword(w, r)
}
func (s authServer) ListSessions(w http.ResponseWriter, r *http.Request) {
	s.Handler.ListSessions(w, r)
}
func (s authServer) RevokeSession(w http.ResponseWriter, r *http.Request, id httpapi.SessionId) {
	s.Handler.RevokeSession(w, r, id)
}
func (s authServer) ListDevices(w http.ResponseWriter, r *http.Request) { s.Handler.ListDevices(w, r) }
func (s authServer) RenameDevice(w http.ResponseWriter, r *http.Request, id httpapi.DeviceId) {
	s.Handler.RenameDevice(w, r, id)
}

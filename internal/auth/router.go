package auth

import (
	"net/http"

	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
)

func Router(handler *Handler, middleware *Middleware) http.Handler {
	api := httpapi.HandlerWithOptions(handler, httpapi.ChiServerOptions{BaseURL: "/api/v1", ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
		WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "invalid request")
	}})
	safe := middleware.Authenticate(true)(api)
	unsafe := middleware.Authenticate(false)(middleware.RequireSameOrigin(middleware.CSRF(middleware.Touch(api))))
	login := middleware.RequireSameOrigin(api)
	dispatch := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/login" {
			login.ServeHTTP(w, r)
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

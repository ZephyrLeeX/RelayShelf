package auth

import (
	"net/http"

	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
)

func Router(handler *Handler, middleware *Middleware) http.Handler {
	api := httpapi.HandlerWithOptions(handler, httpapi.ChiServerOptions{BaseURL: "/api/v1", ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
		WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "invalid request")
	}})
	private := middleware.Authenticate(true)(middleware.CSRF(api))
	dispatch := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/login" {
			api.ServeHTTP(w, r)
			return
		}
		private.ServeHTTP(w, r)
	})
	return dispatch
}

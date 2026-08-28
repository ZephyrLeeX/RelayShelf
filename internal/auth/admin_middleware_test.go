package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireAdminPolicy(t *testing.T) {
	middleware := &Middleware{}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	for _, test := range []struct {
		name           string
		authentication *Authentication
		want           int
	}{
		{name: "unauthenticated", want: http.StatusUnauthorized},
		{name: "normal user", authentication: &Authentication{User: User{IsAdmin: false}}, want: http.StatusForbidden},
		{name: "administrator", authentication: &Authentication{User: User{IsAdmin: true}}, want: http.StatusNoContent},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/status", nil)
			if test.authentication != nil {
				req = req.WithContext(ContextWithAuthentication(req.Context(), *test.authentication))
			}
			response := httptest.NewRecorder()
			middleware.RequireAdmin(next).ServeHTTP(response, req)
			if response.Code != test.want {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

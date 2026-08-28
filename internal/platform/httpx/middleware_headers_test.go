package httpx

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeadersPolicyShape(t *testing.T) {
	directives := map[string]string{}
	for _, part := range strings.Split(ContentSecurityPolicy, ";") {
		fields := strings.Fields(part)
		if len(fields) < 2 {
			t.Fatalf("directive %q must have a name and at least one source", part)
		}
		name := fields[0]
		if _, exists := directives[name]; exists {
			t.Fatalf("directive %s appears twice", name)
		}
		directives[name] = strings.Join(fields[1:], " ")
	}
	for _, name := range []string{
		"default-src", "script-src", "style-src", "img-src", "font-src", "connect-src",
		"media-src", "frame-src", "worker-src", "manifest-src", "object-src", "base-uri",
		"form-action", "frame-ancestors",
	} {
		if _, ok := directives[name]; !ok {
			t.Fatalf("policy is missing %s", name)
		}
	}
	for _, name := range []string{"script-src", "style-src", "default-src"} {
		sources := directives[name]
		if strings.Contains(sources, "unsafe-inline") || strings.Contains(sources, "unsafe-eval") {
			t.Fatalf("%s must not be relaxed: %s", name, sources)
		}
	}
	if sources := directives["object-src"]; sources != "'none'" {
		t.Fatalf("object-src must be 'none', got %s", sources)
	}
	if sources := directives["frame-ancestors"]; sources != "'none'" {
		t.Fatalf("frame-ancestors must be 'none', got %s", sources)
	}
	if sources := directives["base-uri"]; sources != "'none'" {
		t.Fatalf("base-uri must be 'none', got %s", sources)
	}
	for name, sources := range directives {
		for _, source := range strings.Fields(sources) {
			if strings.HasPrefix(source, "http:") || strings.HasPrefix(source, "https:") || strings.HasPrefix(source, "//") || source == "*" {
				t.Fatalf("%s must not allow remote origins or wildcards: %s", name, source)
			}
		}
	}
}

func TestSecurityHeadersAppliedToEveryResponse(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/anything", nil))
	headers := response.Header()
	if got := headers.Get("Content-Security-Policy"); got != ContentSecurityPolicy {
		t.Fatalf("csp=%q", got)
	}
	if got := headers.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("nosniff=%q", got)
	}
	if got := headers.Get("Referrer-Policy"); got != ReferrerPolicy {
		t.Fatalf("referrer=%q", got)
	}
	if got := headers.Get("Permissions-Policy"); got != PermissionsPolicy {
		t.Fatalf("permissions=%q", got)
	}
	if response.Code != http.StatusTeapot {
		t.Fatalf("middleware must not alter status, got %d", response.Code)
	}
}

func TestRecoveryLogsPanicWithoutRequestData(t *testing.T) {
	var logged strings.Builder
	logger := log.New(&logged, "", 0)
	captured := ""
	handler := Recovery(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		captured = "handler ran"
		panic("boom with secret BODY_SENTINEL_xyz password=letmein")
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/messages?body=secret", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d", response.Code)
	}
	if !strings.Contains(response.Body.String(), "INTERNAL_ERROR") {
		t.Fatalf("body=%q", response.Body.String())
	}
	if captured != "handler ran" {
		t.Fatal("handler did not run")
	}
	output := logged.String()
	if !strings.Contains(output, "panic recovered") || !strings.Contains(output, "path=/api/v1/messages") {
		t.Fatalf("log=%q", output)
	}
	for _, secret := range []string{"BODY_SENTINEL_xyz", "letmein", "secret"} {
		if strings.Contains(output, secret) {
			t.Fatalf("panic log leaked %q: %q", secret, output)
		}
	}
}

// TestRequestLogPreservesFlusher pins the SSE regression: the request-log
// wrapper must keep implementing http.Flusher, or the events endpoint 500s
// behind it in a real server.
func TestRequestLogPreservesFlusher(t *testing.T) {
	handler := RequestLog(log.New(io.Discard, "", 0))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("response writer lost the Flusher interface")
		}
		flusher.Flush()
		w.WriteHeader(http.StatusOK)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/events", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d", recorder.Code)
	}
	if !recorder.Flushed {
		t.Fatal("flush did not reach the underlying recorder")
	}
}

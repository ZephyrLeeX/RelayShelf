package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"
)

type traceKey struct{}

func TraceID(r *http.Request) string {
	value, _ := r.Context().Value(traceKey{}).(string)
	return value
}
func Trace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := r.Header.Get("X-Request-ID")
		if value == "" || len(value) > 128 {
			b := make([]byte, 16)
			_, _ = rand.Read(b)
			value = hex.EncodeToString(b)
		}
		w.Header().Set("X-Request-ID", value)
		next.ServeHTTP(w, r.WithContext(contextWithTrace(r, value)))
	})
}
func contextWithTrace(r *http.Request, value string) context.Context {
	return context.WithValue(r.Context(), traceKey{}, value)
}

// ContentSecurityPolicy is the Phase 11 baseline policy. It was derived from
// the pinned Vue production build, not guessed: the build emits external
// module scripts and styles only (no inline script/style), registers the PWA
// service worker from a same-origin script, previews PDFs in a same-origin
// iframe, and plays audio/video from same-origin attachment URLs. Directive
// notes:
//   - worker-src/manifest-src cover the PWA service worker and webmanifest.
//   - media-src/frame-src cover the attachment viewer's <audio>/<video> and
//     the PDF preview iframe; active content (HTML/SVG/XML) is excluded from
//     inline preview by the server MIME allowlist, not by CSP alone.
//   - base-uri 'none': the app never uses <base>, so any injection is a bug.
//
// Any future relaxation must be minimal, justified here, and covered by a test.
const ContentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; media-src 'self'; frame-src 'self'; worker-src 'self'; manifest-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

const ReferrerPolicy = "same-origin"

const PermissionsPolicy = "camera=(), microphone=(), geolocation=(), payment=(), usb=(), bluetooth=()"

func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", ReferrerPolicy)
		w.Header().Set("Permissions-Policy", PermissionsPolicy)
		w.Header().Set("Content-Security-Policy", ContentSecurityPolicy)
		next.ServeHTTP(w, r)
	})
}

// Recovery converts handler panics into structured 500 responses. It logs
// only the request line, trace ID, and the panic value's type — never the
// request body, headers, or cookies, which may hold secrets.
func Recovery(logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				value := recover()
				if value == nil {
					return
				}
				if errors.Is(r.Context().Err(), context.Canceled) {
					// The client went away mid-handler; nothing to report.
					return
				}
				logger.Printf("panic recovered trace_id=%s method=%s path=%s value_type=%T", TraceID(r), r.Method, r.URL.Path, value)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"code": "INTERNAL_ERROR", "message": "internal server error", "traceId": TraceID(r)})
			}()
			next.ServeHTTP(w, r)
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Flush keeps the http.Flusher contract alive through this wrapper. The SSE
// handler asserts Flusher directly, so an embedding-only wrapper would turn
// every browser event stream into a 500 — the recorder used in unit tests
// implements Flusher, which is why only real-browser E2E caught this.
func (w *statusWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *statusWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func RequestLog(logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			recorder := &statusWriter{ResponseWriter: w}
			next.ServeHTTP(recorder, r)
			status := recorder.status
			if status == 0 {
				status = http.StatusOK
			}
			logger.Printf("trace_id=%s method=%s path=%s status=%d duration_ms=%d", TraceID(r), r.Method, r.URL.Path, status, time.Since(started).Milliseconds())
		})
	}
}

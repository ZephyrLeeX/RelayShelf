package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
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

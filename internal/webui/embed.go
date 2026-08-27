package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// dist is produced by the pinned Vue build and embedded into the Go binary.
//
//go:embed dist
var embedded embed.FS

type spaHandler struct {
	files http.Handler
	dist  fs.FS
}

// Handler serves immutable Vite assets and falls back to index.html for SPA
// routes. API and health routes are registered before this handler.
func Handler() http.Handler {
	dist, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic("embedded web distribution is unavailable")
	}
	return spaHandler{files: http.FileServer(http.FS(dist)), dist: dist}
}

func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if info, err := fs.Stat(h.dist, path); err == nil && !info.IsDir() {
		if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		h.files.ServeHTTP(w, r)
		return
	}
	if strings.HasPrefix(path, "assets/") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	clone := r.Clone(r.Context())
	clone.URL.Path = "/"
	h.files.ServeHTTP(w, clone)
}

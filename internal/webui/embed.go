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

// AssetNames lists the hashed Vite asset paths from the embedded build. It
// lets tests and verification tooling exercise real production artifacts
// instead of guessing what the frontend emits.
func AssetNames() ([]string, error) {
	dist, err := fs.Sub(embedded, "dist")
	if err != nil {
		return nil, err
	}
	out := []string{}
	err = fs.WalkDir(dist, "assets", func(path string, _ fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !strings.Contains(path, "/") || strings.HasSuffix(path, "/") {
			return nil
		}
		out = append(out, "/"+path)
		return nil
	})
	return out, err
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

package api

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// NewFrontendHandler serves the embedded React SPA from `dist/`.
// Static asset requests (e.g., /assets/foo.js) resolve directly.
// Everything else falls back to index.html so the client-side router
// can take over.
//
// If `dist/` is empty (the developer hasn't run `pnpm build`), all
// requests return 404 and the test suite skips the embed checks.
func NewFrontendHandler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return http.NotFoundHandler()
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return http.NotFoundHandler()
	}

	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := path.Clean(r.URL.Path)
		if clean == "/" {
			serveIndex(w, sub)
			return
		}
		rel := strings.TrimPrefix(clean, "/")
		if f, err := sub.Open(rel); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// SPA fallback.
		serveIndex(w, sub)
	})
}

func serveIndex(w http.ResponseWriter, fsys fs.FS) {
	data, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		http.NotFound(w, &http.Request{})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

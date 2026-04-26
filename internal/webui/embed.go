// Package webui embeds the built React/Vite web UI and serves it under /ui/.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed dist
var distFS embed.FS

// Handler returns an http.Handler that serves the embedded web UI.
// All paths under /ui/ are served; unknown paths fall back to index.html
// for client-side routing.
func Handler() http.Handler {
	// Strip the "dist" prefix so the embed FS root maps to /ui/
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// dist doesn't exist yet (pre-build); return empty handler
		return http.NotFoundHandler()
	}

	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the actual file
		f, err := sub.Open(r.URL.Path)
		if err != nil {
			// File not found — serve index.html for SPA client-side routing
			r.URL.Path = "/"
		} else {
			f.Close()
		}
		fileServer.ServeHTTP(w, r)
	})
}

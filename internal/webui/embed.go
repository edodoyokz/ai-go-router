// Package webui embeds the built React/Vite web UI and serves it under /ui/.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed dist
var distFS embed.FS

// Handler returns an http.Handler that serves the embedded web UI.
// All paths under /ui/ are served; unknown paths fall back to index.html
// for client-side routing.
func Handler() http.Handler {
	// Create a sub-filesystem rooted at dist/
	distSubFS, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strip /ui prefix from the full path
		path := strings.TrimPrefix(r.URL.Path, "/ui")
		path = strings.TrimPrefix(path, "/")

		if path == "" {
			path = "index.html"
		}

		// Try to read the file
		data, err := fs.ReadFile(distSubFS, path)
		if err != nil {
			// File not found — serve index.html for SPA routing
			data, err = fs.ReadFile(distSubFS, "index.html")
			if err != nil {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(data)
			return
		}

		// Set appropriate content type based on file extension
		var ext string
		if idx := strings.LastIndex(path, "."); idx >= 0 {
			ext = strings.ToLower(path[idx+1:])
		}
		switch ext {
		case "html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		case "css":
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		case "js":
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		case "json":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
		case "svg":
			w.Header().Set("Content-Type", "image/svg+xml")
		case "png":
			w.Header().Set("Content-Type", "image/png")
		case "jpg", "jpeg":
			w.Header().Set("Content-Type", "image/jpeg")
		case "gif":
			w.Header().Set("Content-Type", "image/gif")
		case "webp":
			w.Header().Set("Content-Type", "image/webp")
		case "woff":
			w.Header().Set("Content-Type", "font/woff")
		case "woff2":
			w.Header().Set("Content-Type", "font/woff2")
		case "ttf":
			w.Header().Set("Content-Type", "font/ttf")
		}

		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write(data)
	})
}

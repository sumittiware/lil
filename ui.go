package main

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
)

//go:embed ui/dist
var uiFiles embed.FS

// getAdminUI returns a http.Handler that serves the admin UI from embedded static files.
// It handles both static file serving and SPA routing by:
// 1. Attempting to serve static files directly from the embedded filesystem
// 2. Falling back to serving index.html for all other routes to support client-side routing
func getAdminUI() http.Handler {
	// Create a sub-filesystem pointing to the built UI files
	fsys, err := fs.Sub(uiFiles, "ui/dist")
	if err != nil {
		panic(err)
	}

	return http.StripPrefix("/admin/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// First try to serve static files directly from the filesystem
		// This handles JS, CSS, images and other assets
		if _, err := fsys.Open(r.URL.Path); err == nil {
			http.FileServer(http.FS(fsys)).ServeHTTP(w, r)
			return
		}

		// If the path doesn't match a static file, serve index.html
		// This enables client-side routing to work properly in the SPA
		index, err := fsys.Open("index.html")
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		defer index.Close()

		// Set content type and stream the index.html file
		w.Header().Set("Content-Type", "text/html")
		io.Copy(w, index)
	}))
}

// Package httpserver provides SlideTalk's HTTP API and static asset server.
package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
)

// ServerOptions configures the SlideTalk HTTP server.
type ServerOptions struct {
	StaticDir string
}

// New returns a configured HTTP handler for the SlideTalk server.
func New(options ServerOptions) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)

	var staticHandler http.Handler
	if options.StaticDir != "" && dirExists(options.StaticDir) {
		staticHandler = http.FileServer(http.Dir(options.StaticDir))
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			mux.ServeHTTP(w, r)
			return
		}

		if isAPIPath(r.URL.Path) {
			writeProblem(w, http.StatusNotFound, "Not Found", "No API route matches "+r.URL.Path+".")
			return
		}

		if staticHandler != nil {
			serveStatic(staticHandler, options.StaticDir, w, r)
			return
		}

		http.NotFound(w, r)
	})
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func isAPIPath(path string) bool {
	return path == "/api" || len(path) > len("/api/") && path[:len("/api/")] == "/api/"
}

func writeProblem(w http.ResponseWriter, status int, title string, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem{
		Type:   "about:blank",
		Title:  title,
		Status: status,
		Detail: detail,
	})
}

type problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

func serveStatic(staticHandler http.Handler, staticDir string, w http.ResponseWriter, r *http.Request) {
	requested := filepath.Clean(r.URL.Path)
	if requested == "." || requested == "/" {
		requested = "index.html"
	}

	fullPath := filepath.Join(staticDir, requested)
	if _, err := os.Stat(fullPath); errors.Is(err, os.ErrNotExist) {
		r = r.Clone(r.Context())
		r.URL.Path = "/"
	}

	staticHandler.ServeHTTP(w, r)
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

package server

import (
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
)

// tokenPlaceholder is the literal index.html ships with; the server replaces it
// with the token of the Sessão while serving the page (contrato 1.1).
const tokenPlaceholder = "{{SCANFILE_TOKEN}}"

const indexFileName = "index.html"

// uiHandler serves the embedded interface. index.html is rendered by hand so
// the token of the Sessão reaches the page, and is never cached: a stale copy
// would carry the token of a previous run. Every other asset keeps going
// through http.FileServer.
func (s *AppServer) uiHandler() http.Handler {
	if s.uiFS == nil {
		return http.NotFoundHandler()
	}

	files := http.FileServer(http.FS(s.uiFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isIndexRequest(r.URL.Path) {
			s.serveIndex(w, r)
			return
		}
		files.ServeHTTP(w, r)
	})
}

// isIndexRequest reports whether the path asks for the interface entry point.
func isIndexRequest(p string) bool {
	switch normalizedPath(p) {
	case "/", "/" + indexFileName:
		return true
	}
	return false
}

// serveIndex writes index.html with the token injected.
func (s *AppServer) serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeAPIError(w, http.StatusMethodNotAllowed, "Método não permitido: use GET.", "method_not_allowed")
		return
	}

	raw, err := fs.ReadFile(s.uiFS, indexFileName)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	page := strings.ReplaceAll(string(raw), tokenPlaceholder, s.token())

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(page)))
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.WriteString(w, page)
}

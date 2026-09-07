package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"path"
	"strings"
	"sync"
)

// SessionTokenHeader carries the token of the Sessão on every call to /api/*.
const SessionTokenHeader = "X-ScanFile-Token"

// sessionTokenParam is the query parameter accepted only where the browser
// cannot set a header: EventSource (/api/events) and sendBeacon
// (/api/ui/closed). Contrato 1.1.
const sessionTokenParam = "token"

// tokenQueryPaths lists the paths that may carry the token in the query string.
var tokenQueryPaths = map[string]bool{
	"/api/events":    true,
	"/api/ui/closed": true,
}

// sessionTokenMu guards the lazy creation of AppServer.sessionToken.
//
// The contract asks for a sync.Once over s.sessionToken, but AppServer declares
// no Once field and server.go belongs to another agent (see the final report):
// a package mutex around the same field gives the same guarantee — the token is
// generated once per AppServer and every reader sees the same value.
var sessionTokenMu sync.Mutex

// newSessionToken draws 32 bytes from crypto/rand and renders them as hex.
func newSessionToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand never fails on a supported platform; a server running with
		// a predictable token would be worse than no server at all.
		panic("scanfile: não foi possível gerar o token da Sessão: " + err.Error())
	}
	return hex.EncodeToString(buf)
}

// token returns the token of the Sessão, creating it on first use.
func (s *AppServer) token() string {
	sessionTokenMu.Lock()
	defer sessionTokenMu.Unlock()
	if s.sessionToken == "" {
		s.sessionToken = newSessionToken()
	}
	return s.sessionToken
}

// SetSessionToken adopts a token generated elsewhere. The elevated child of a
// handoff reads it from instance.json so the interface keeps working without a
// reload (contrato 1.9). An empty token is ignored.
func (s *AppServer) SetSessionToken(token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	sessionTokenMu.Lock()
	s.sessionToken = token
	sessionTokenMu.Unlock()
}

// authMiddleware enforces the Sessão (contrato 1.1): every /api/* call carries
// the token, a request from another origin is refused, and no CORS header is
// ever emitted — the interface is served by this very server.
func (s *AppServer) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !sameOriginRequest(r) {
			writeAPIError(w, http.StatusForbidden, "forbidden", "cross_origin")
			return
		}

		if !isAPIPath(r.URL.Path) || isTokenExempt(r) {
			next.ServeHTTP(w, r)
			return
		}

		if !s.tokenMatches(requestToken(r)) {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// tokenMatches compares the presented token with the one of the Sessão in
// constant time, so a wrong token never leaks how much of it was right.
func (s *AppServer) tokenMatches(presented string) bool {
	if presented == "" {
		return false
	}
	expected := s.token()
	return subtle.ConstantTimeCompare([]byte(presented), []byte(expected)) == 1
}

// requestToken extracts the token from the header or, only for the two paths a
// browser cannot send headers on, from the query string.
func requestToken(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get(SessionTokenHeader)); v != "" {
		return v
	}
	if tokenQueryPaths[normalizedPath(r.URL.Path)] {
		return strings.TrimSpace(r.URL.Query().Get(sessionTokenParam))
	}
	return ""
}

// isTokenExempt reports the single call served without a token: instance
// discovery, which answers only {app, version, pid} (contrato 1.1 e 1.9).
func isTokenExempt(r *http.Request) bool {
	return r.Method == http.MethodGet && normalizedPath(r.URL.Path) == "/api/instance"
}

// isAPIPath reports whether the path belongs to the API surface.
func isAPIPath(p string) bool {
	clean := normalizedPath(p)
	return clean == "/api" || strings.HasPrefix(clean, "/api/")
}

// sameOriginRequest reports whether the request either carries no Origin (a
// plain navigation or a same-origin sub-resource) or one that matches the host
// it was sent to. A page on any other origin — including "null" — is refused.
func sameOriginRequest(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	if r.Host == "" {
		return false
	}
	return strings.EqualFold(origin, "http://"+r.Host) || strings.EqualFold(origin, "https://"+r.Host)
}

// normalizedPath canonicalises a request path for comparison: "/api/events/"
// and "/api//events" both become "/api/events".
func normalizedPath(p string) string {
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return path.Clean(p)
}

// apiError is the error body of the API. Code is additive and only helps the
// interface tell one refusal from another; 401 answers exactly
// {"error":"unauthorized"} as the contract requires.
type apiError struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

// writeAPIError answers with a JSON error body and the given status.
func writeAPIError(w http.ResponseWriter, status int, message, code string) {
	writeJSON(w, status, apiError{Error: message, Code: code})
}

// writeJSON encodes payload as the whole response body.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

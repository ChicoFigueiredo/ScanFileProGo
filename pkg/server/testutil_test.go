package server

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

// testUIFS is a minimal embedded UI used by handler tests (contains the token placeholder).
func testUIFS() fs.FS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!doctype html><meta name=\"scanfile-token\" content=\"{{SCANFILE_TOKEN}}\"><title>t</title>")},
	}
}

// newTestServer builds an AppServer wired to an httptest.Server serving the raw
// handler: requests carry no token unless the test sets one. Use it to exercise
// the Sessão itself (auth_test.go); every other test wants newAuthedTestServer.
func newTestServer(t *testing.T) (*AppServer, *httptest.Server) {
	t.Helper()
	app := NewAppServer(testUIFS())
	ts := httptest.NewServer(app.Handler())
	t.Cleanup(func() {
		ts.Close()
		app.Stop()
	})
	return app, ts
}

// authedHandler wraps the server's handler so every request carries the token of
// the Sessão. Tests that are not about authentication should not have to repeat
// the header on each call — and must not be able to pass by skipping it.
func authedHandler(app *AppServer) http.Handler {
	h := app.Handler()
	token := app.token()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set(SessionTokenHeader, token)
		h.ServeHTTP(w, r)
	})
}

// newAuthedTestServer builds an AppServer whose httptest.Server authenticates
// every request, so a test can focus on the behaviour it is about.
func newAuthedTestServer(t *testing.T) (*AppServer, *httptest.Server) {
	t.Helper()
	app := NewAppServer(testUIFS())
	ts := httptest.NewServer(authedHandler(app))
	t.Cleanup(func() {
		ts.Close()
		app.Stop()
	})
	return app, ts
}

package server

import (
	"io/fs"
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

// newTestServer builds an AppServer wired to an httptest.Server. Callers get the server,
// the base URL and a cleanup function. Shared by all handler tests in this package.
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

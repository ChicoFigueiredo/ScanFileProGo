package server

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// newHTTPTestServer wires an AppServer built by the caller to an httptest
// server. It complements newTestServer for the tests that need their own
// embedded interface.
func newHTTPTestServer(t *testing.T, app *AppServer) *httptest.Server {
	t.Helper()

	ts := httptest.NewServer(app.Handler())
	t.Cleanup(func() {
		ts.Close()
		app.Stop()
	})
	return ts
}

func readBody(t *testing.T, res *http.Response) string {
	t.Helper()

	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("não foi possível ler o corpo: %v", err)
	}
	return string(data)
}

func TestIndexCarriesTheSessionToken(t *testing.T) {
	app, ts := newTestServer(t)

	res, err := ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", res.StatusCode)
	}

	body := readBody(t, res)
	if strings.Contains(body, tokenPlaceholder) {
		t.Fatalf("o placeholder %s continua no index.html servido", tokenPlaceholder)
	}
	if !strings.Contains(body, app.token()) {
		t.Fatalf("o index.html servido não contém o token da Sessão:\n%s", body)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q, esperado text/html", ct)
	}
	if cc := res.Header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Fatalf("Cache-Control = %q, esperado no-store: o token não pode ficar em cache", cc)
	}
}

func TestIndexHTMLPathAlsoCarriesTheToken(t *testing.T) {
	app, ts := newTestServer(t)

	res, err := ts.Client().Get(ts.URL + "/index.html")
	if err != nil {
		t.Fatalf("GET /index.html: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", res.StatusCode)
	}
	if !strings.Contains(readBody(t, res), app.token()) {
		t.Fatal("/index.html não trouxe o token da Sessão")
	}
}

func TestIndexNeedsNoToken(t *testing.T) {
	_, ts := newTestServer(t)

	res, err := ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: a interface precisa carregar antes de conhecer o token", res.StatusCode)
	}
}

func TestOtherAssetsGoThroughTheFileServer(t *testing.T) {
	app := NewAppServer(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte(`<meta content="` + tokenPlaceholder + `">`)},
		"js/app.js":  &fstest.MapFile{Data: []byte("var placeholder = '" + tokenPlaceholder + "';")},
	})
	ts := newHTTPTestServer(t, app)

	res, err := ts.Client().Get(ts.URL + "/js/app.js")
	if err != nil {
		t.Fatalf("GET /js/app.js: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", res.StatusCode)
	}

	body := readBody(t, res)
	if !strings.Contains(body, tokenPlaceholder) {
		t.Fatal("o token foi injetado num asset que não é o index.html")
	}
	if strings.Contains(body, app.token()) {
		t.Fatal("o token da Sessão vazou para um asset estático")
	}
}

func TestUIHandlerWithoutFilesystem(t *testing.T) {
	app := NewAppServer(nil)
	ts := newHTTPTestServer(t, app)

	res, err := ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, esperado 404 sem interface embutida", res.StatusCode)
	}
}

func TestMissingIndexIsNotFound(t *testing.T) {
	var empty fs.FS = fstest.MapFS{}
	app := NewAppServer(empty)
	ts := newHTTPTestServer(t, app)

	res, err := ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, esperado 404 sem index.html", res.StatusCode)
	}
}

func TestRealIndexHTMLGetsTheToken(t *testing.T) {
	// A interface de verdade, não uma cópia de teste: se o placeholder mudar de
	// nome em ui/index.html, este teste cai.
	app := NewAppServer(os.DirFS(filepath.Join("..", "..", "ui")))
	ts := newHTTPTestServer(t, app)

	res, err := ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", res.StatusCode)
	}

	body := readBody(t, res)
	if strings.Contains(body, tokenPlaceholder) {
		t.Fatalf("o placeholder %s continua em ui/index.html servido", tokenPlaceholder)
	}
	want := `<meta name="scanfile-token" content="` + app.token() + `">`
	if !strings.Contains(body, want) {
		t.Fatalf("a meta tag do token não veio como esperado: %s", want)
	}
}

func TestIsIndexRequest(t *testing.T) {
	cases := map[string]bool{
		"/":            true,
		"/index.html":  true,
		"//index.html": true,
		"/js/app.js":   false,
		"/css/x.css":   false,
		"/api/config":  false,
	}
	for path, want := range cases {
		if got := isIndexRequest(path); got != want {
			t.Errorf("isIndexRequest(%q) = %v, esperado %v", path, got, want)
		}
	}
}

package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"scanfile/pkg/config"
)

// =========================================================================
// Helpers shared by the handler tests of this area (agente S1). They live in
// a test file, so any test of pkg/server may use them.
// =========================================================================

// newAPIRequest builds a request against the test server carrying the token of
// the Sessão.
func newAPIRequest(t *testing.T, app *AppServer, ts *httptest.Server, method, target string, body any) *http.Request {
	t.Helper()

	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("não foi possível serializar o corpo: %v", err)
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, ts.URL+target, reader)
	if err != nil {
		t.Fatalf("não foi possível montar %s %s: %v", method, target, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set(SessionTokenHeader, app.token())
	return req
}

// doAPI performs an authenticated call and closes the body when the test ends.
func doAPI(t *testing.T, app *AppServer, ts *httptest.Server, method, target string, body any) *http.Response {
	t.Helper()

	res, err := ts.Client().Do(newAPIRequest(t, app, ts, method, target, body))
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

// decodeJSONBody reads the whole response body into out.
func decodeJSONBody(t *testing.T, res *http.Response, out any) {
	t.Helper()

	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("não foi possível ler o corpo: %v", err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("corpo não é o JSON esperado (%s): %v", string(data), err)
	}
}

// useTempConfig points the Configuração at a throwaway file for the duration of
// the test, so nothing touches the user's real scanfile_config.json.
func useTempConfig(t *testing.T, cfg config.AppConfig) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "scanfile-config-")
	if err != nil {
		t.Fatalf("não foi possível criar o diretório da configuração de teste: %v", err)
	}
	path := filepath.Join(dir, "scanfile_config.json")

	config.SetConfigPath(path)
	t.Cleanup(func() {
		config.SetConfigPath("")
		// O Windows às vezes segura o arquivo por um instante (indexador,
		// antivírus): a limpeza nunca deve derrubar um teste que passou.
		_ = os.RemoveAll(dir)
	})

	if err := config.SaveConfig(cfg); err != nil {
		t.Fatalf("não foi possível gravar a configuração de teste: %v", err)
	}
	return path
}

// offlineConfig is a configuration whose Assistente endpoints point at a closed
// port, so handlers that talk to Ollama fail fast instead of hitting a daemon
// that may or may not be running on the developer's machine.
func offlineConfig() config.AppConfig {
	cfg := config.GetDefaultConfig()
	cfg.AIOllamaEndpoint = "http://127.0.0.1:1"
	return cfg
}

// =========================================================================
// Token e Origin (contrato 1.1)
// =========================================================================

func TestAPIWithoutTokenIsUnauthorized(t *testing.T) {
	_, ts := newTestServer(t)

	res, err := ts.Client().Get(ts.URL + "/api/system/info")
	if err != nil {
		t.Fatalf("GET /api/system/info: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401", res.StatusCode)
	}

	var body map[string]string
	decodeJSONBody(t, res, &body)
	if body["error"] != "unauthorized" {
		t.Fatalf(`corpo = %v, esperado {"error":"unauthorized"}`, body)
	}
}

func TestAPIWithTokenIsAuthorized(t *testing.T) {
	app, ts := newTestServer(t)

	res := doAPI(t, app, ts, http.MethodGet, "/api/system/info", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", res.StatusCode)
	}
}

func TestAPIWithWrongTokenIsUnauthorized(t *testing.T) {
	app, ts := newTestServer(t)

	req := newAPIRequest(t, app, ts, http.MethodGet, "/api/system/info", nil)
	req.Header.Set(SessionTokenHeader, strings.Repeat("a", 64))

	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /api/system/info: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401", res.StatusCode)
	}
}

func TestForeignOriginIsForbidden(t *testing.T) {
	app, ts := newTestServer(t)

	for _, origin := range []string{"http://evil.example", "null", "http://127.0.0.1:9"} {
		req := newAPIRequest(t, app, ts, http.MethodGet, "/api/system/info", nil)
		req.Header.Set("Origin", origin)

		res, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("Origin %q: %v", origin, err)
		}
		res.Body.Close()

		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("Origin %q: status = %d, esperado 403", origin, res.StatusCode)
		}
	}
}

func TestOwnOriginIsAccepted(t *testing.T) {
	app, ts := newTestServer(t)

	req := newAPIRequest(t, app, ts, http.MethodGet, "/api/system/info", nil)
	req.Header.Set("Origin", ts.URL)

	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /api/system/info: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 para a própria origem %q", res.StatusCode, ts.URL)
	}
}

func TestNoCORSHeadersAreEmitted(t *testing.T) {
	app, ts := newTestServer(t)

	// Uma resposta de sucesso, uma de 401 e a própria interface.
	responses := []*http.Response{
		doAPI(t, app, ts, http.MethodGet, "/api/system/info", nil),
	}

	plain, err := ts.Client().Get(ts.URL + "/api/system/info")
	if err != nil {
		t.Fatalf("GET sem token: %v", err)
	}
	defer plain.Body.Close()
	responses = append(responses, plain)

	page, err := ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer page.Body.Close()
	responses = append(responses, page)

	for i, res := range responses {
		for name := range res.Header {
			if strings.HasPrefix(strings.ToLower(name), "access-control-") {
				t.Fatalf("resposta %d trouxe cabeçalho CORS %q", i, name)
			}
		}
	}
}

func TestOptionsPreflightIsNotAnswered(t *testing.T) {
	_, ts := newTestServer(t)

	req, err := http.NewRequest(http.MethodOptions, ts.URL+"/api/config", nil)
	if err != nil {
		t.Fatalf("OPTIONS: %v", err)
	}
	req.Header.Set("Origin", "http://evil.example")
	req.Header.Set("Access-Control-Request-Method", "POST")

	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("OPTIONS /api/config: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, esperado 403 (preflight nunca é autorizado)", res.StatusCode)
	}
	if res.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("o preflight recebeu Access-Control-Allow-Origin")
	}
}

func TestInstanceDiscoveryNeedsNoToken(t *testing.T) {
	_, ts := newTestServer(t)

	// A rota é do agente S3; aqui só provamos que o middleware não a bloqueia.
	res, err := ts.Client().Get(ts.URL + "/api/instance")
	if err != nil {
		t.Fatalf("GET /api/instance: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusUnauthorized {
		t.Fatal("GET /api/instance exigiu token, mas é a exceção do contrato 1.1")
	}
}

func TestInstanceFocusStillNeedsToken(t *testing.T) {
	_, ts := newTestServer(t)

	res, err := ts.Client().Post(ts.URL+"/api/instance/focus", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST /api/instance/focus: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401", res.StatusCode)
	}
}

func TestQueryTokenOnlyOnBeaconPaths(t *testing.T) {
	app, ts := newTestServer(t)
	token := app.token()

	// /api/ui/closed é do agente S3: sem a rota o mux responde 404, o que já
	// prova que o middleware aceitou o token da query.
	res, err := ts.Client().Post(ts.URL+"/api/ui/closed?token="+token, "text/plain", nil)
	if err != nil {
		t.Fatalf("POST /api/ui/closed: %v", err)
	}
	res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		t.Fatal("POST /api/ui/closed?token= foi recusado, mas o contrato 1.1 aceita a query aqui")
	}

	res, err = ts.Client().Post(ts.URL+"/api/ui/closed?token=errado", "text/plain", nil)
	if err != nil {
		t.Fatalf("POST /api/ui/closed com token errado: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("token errado na query devolveu %d, esperado 401", res.StatusCode)
	}

	// Em qualquer outra rota a query não vale.
	res, err = ts.Client().Get(ts.URL + "/api/system/info?token=" + token)
	if err != nil {
		t.Fatalf("GET /api/system/info?token=: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401: a query só vale em /api/events e /api/ui/closed", res.StatusCode)
	}
}

func TestSessionTokenShapeAndStability(t *testing.T) {
	app, _ := newTestServer(t)

	first := app.token()
	if first != app.token() {
		t.Fatal("o token da Sessão mudou entre duas leituras")
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(first) {
		t.Fatalf("token = %q, esperado 32 bytes em hexadecimal", first)
	}
	if first != app.sessionToken {
		t.Fatal("o token não foi guardado em AppServer.sessionToken")
	}

	other := NewAppServer(testUIFS())
	if other.token() == first {
		t.Fatal("dois servidores nasceram com o mesmo token")
	}
}

func TestSetSessionTokenAdoptsExistingToken(t *testing.T) {
	app, ts := newTestServer(t)

	adopted := strings.Repeat("b", 64)
	app.SetSessionToken(adopted)
	if app.token() != adopted {
		t.Fatalf("token = %q, esperado o adotado do handoff", app.token())
	}

	app.SetSessionToken("   ")
	if app.token() != adopted {
		t.Fatal("um token vazio apagou o token da Sessão")
	}

	res := doAPI(t, app, ts, http.MethodGet, "/api/system/info", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 com o token adotado", res.StatusCode)
	}
}

// =========================================================================
// Unidades do middleware
// =========================================================================

func TestIsAPIPath(t *testing.T) {
	cases := map[string]bool{
		"/api":               true,
		"/api/":              true,
		"/api/config":        true,
		"/api//config":       true,
		"/api/events/":       true,
		"/":                  false,
		"/index.html":        false,
		"/js/app.js":         false,
		"/apixyz":            false,
		"/static/api/thing":  false,
		"/api/../index.html": false,
	}
	for path, want := range cases {
		if got := isAPIPath(path); got != want {
			t.Errorf("isAPIPath(%q) = %v, esperado %v", path, got, want)
		}
	}
}

func TestSameOriginRequest(t *testing.T) {
	cases := []struct {
		origin string
		host   string
		want   bool
	}{
		{"", "127.0.0.1:47321", true},
		{"http://127.0.0.1:47321", "127.0.0.1:47321", true},
		{"https://127.0.0.1:47321", "127.0.0.1:47321", true},
		{"http://localhost:47321", "localhost:47321", true},
		{"http://127.0.0.1:47322", "127.0.0.1:47321", false},
		{"http://evil.example", "127.0.0.1:47321", false},
		{"null", "127.0.0.1:47321", false},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		req.Host = c.host
		if c.origin != "" {
			req.Header.Set("Origin", c.origin)
		}
		if got := sameOriginRequest(req); got != c.want {
			t.Errorf("Origin %q em %q = %v, esperado %v", c.origin, c.host, got, c.want)
		}
	}
}

func TestRequestTokenSources(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/events?token=daquery", nil)
	if got := requestToken(req); got != "daquery" {
		t.Errorf("token de /api/events = %q, esperado daquery", got)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/ui/closed?token=daquery", nil)
	if got := requestToken(req); got != "daquery" {
		t.Errorf("token de /api/ui/closed = %q, esperado daquery", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/config?token=daquery", nil)
	if got := requestToken(req); got != "" {
		t.Errorf("token de /api/config = %q, esperado vazio", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/config?token=daquery", nil)
	req.Header.Set(SessionTokenHeader, "docabecalho")
	if got := requestToken(req); got != "docabecalho" {
		t.Errorf("token = %q, esperado o do cabeçalho", got)
	}
}

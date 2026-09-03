package server

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"scanfile/pkg/config"
	"scanfile/pkg/recycle"
	"scanfile/pkg/scanner"
)

// requireWindowsPaths skips the tests whose rules only make sense over the
// Windows path model (volume roots, Pasta Protegida, Lixeira).
func requireWindowsPaths(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("escopo, Pasta Protegida e Lixeira dependem de caminhos do Windows")
	}
}

// tempDir creates a throwaway directory. Unlike t.TempDir it never fails the
// test during cleanup: on Windows a file that was just deleted only leaves the
// directory when the last handle on it closes, and an antivirus holding that
// handle for a moment would turn a green test red for the wrong reason.
func tempDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "scanfile-test-")
	if err != nil {
		t.Fatalf("não foi possível criar o diretório temporário: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// writeTempFile creates a file of the given size and returns its path.
func writeTempFile(t *testing.T, dir, name string, size int) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
		t.Fatalf("não foi possível criar %s: %v", path, err)
	}
	return path
}

// indexFile registers a file in the árvore and in the índice, as a Varredura
// would.
func indexFile(app *AppServer, path string, size int64, hash string) *scanner.FileNode {
	node := &scanner.FileNode{
		Path:      path,
		Name:      filepath.Base(path),
		Size:      size,
		Hash:      hash,
		Extension: strings.ToLower(filepath.Ext(path)),
	}
	app.Tree.AddFile(node)
	app.Index.UpsertFile(node)
	return node
}

// postFileAction performs one of the two batch endpoints and decodes the answer.
func postFileAction(t *testing.T, app *AppServer, ts *httptest.Server, target string, body any) (*http.Response, fileActionResponse) {
	t.Helper()

	res := doAPI(t, app, ts, http.MethodPost, target, body)
	var decoded fileActionResponse
	if res.StatusCode == http.StatusOK {
		decodeJSONBody(t, res, &decoded)
	}
	return res, decoded
}

// =========================================================================
// Reciclagem e Exclusão Permanente (contrato 1.5)
// =========================================================================

func TestRecycleRefusesPathOutsideRoots(t *testing.T) {
	requireWindowsPaths(t)
	useTempConfig(t, offlineConfig())

	app, ts := newTestServer(t)
	scanned := tempDir(t)
	outside := tempDir(t)
	victim := writeTempFile(t, outside, "fora.bin", 16)
	app.activeRoots = []string{scanned}

	res, decoded := postFileAction(t, app, ts, "/api/files/recycle", map[string]any{
		"paths":       []string{victim},
		"confirmName": "",
	})

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 com o detalhe por item", res.StatusCode)
	}
	if len(decoded.Items) != 1 {
		t.Fatalf("items = %d, esperado 1", len(decoded.Items))
	}
	if decoded.Items[0].Status != recycle.StatusRefused {
		t.Fatalf("status do item = %q, esperado refused", decoded.Items[0].Status)
	}
	if !strings.Contains(decoded.Items[0].Reason, "Raízes Varridas") {
		t.Fatalf("motivo = %q, esperado citar as Raízes Varridas", decoded.Items[0].Reason)
	}
	if decoded.Refused != 1 || decoded.Recycled != 0 || decoded.Deleted != 0 {
		t.Fatalf("contadores = %+v, esperado apenas 1 recusa", decoded)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("o arquivo recusado sumiu do disco: %v", err)
	}
}

func TestFileActionRefusesWithoutLoadedRoots(t *testing.T) {
	requireWindowsPaths(t)
	useTempConfig(t, offlineConfig())

	app, ts := newTestServer(t)
	victim := writeTempFile(t, tempDir(t), "orfao.bin", 8)

	_, decoded := postFileAction(t, app, ts, "/api/files/recycle", map[string]any{
		"paths": []string{victim},
	})

	if decoded.Refused != 1 {
		t.Fatalf("contadores = %+v, esperado 1 recusa sem Raízes carregadas", decoded)
	}
	if !strings.Contains(decoded.Items[0].Reason, "nenhuma Raiz Varrida") {
		t.Fatalf("motivo = %q, esperado explicar a ausência de Raízes", decoded.Items[0].Reason)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("o arquivo recusado sumiu do disco: %v", err)
	}
}

func TestRecycleRefusesProtectedFolderEvenInsideRoots(t *testing.T) {
	requireWindowsPaths(t)
	useTempConfig(t, offlineConfig())

	app, ts := newTestServer(t)
	app.activeRoots = []string{`C:\`}

	_, decoded := postFileAction(t, app, ts, "/api/files/recycle", map[string]any{
		"paths":       []string{`C:\Windows\explorer.exe`},
		"confirmName": "",
	})

	if decoded.Refused != 1 {
		t.Fatalf("contadores = %+v, esperado 1 recusa por Pasta Protegida", decoded)
	}
	if !strings.Contains(decoded.Items[0].Reason, "Protegida") {
		t.Fatalf("motivo = %q, esperado citar a Pasta Protegida", decoded.Items[0].Reason)
	}
}

func TestFolderNeedsMatchingConfirmName(t *testing.T) {
	requireWindowsPaths(t)
	useTempConfig(t, offlineConfig())

	app, ts := newTestServer(t)
	root := tempDir(t)
	folder := filepath.Join(root, "Fotos")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatalf("não foi possível criar a pasta: %v", err)
	}
	writeTempFile(t, folder, "a.jpg", 32)
	app.activeRoots = []string{root}

	for _, confirm := range []string{"", "Outra", "Fotos2"} {
		_, decoded := postFileAction(t, app, ts, "/api/files/recycle", map[string]any{
			"paths":       []string{folder},
			"confirmName": confirm,
		})

		if decoded.Refused != 1 {
			t.Fatalf("confirmName %q: contadores = %+v, esperado recusa", confirm, decoded)
		}
		if !strings.Contains(decoded.Items[0].Reason, "confirmação da pasta") {
			t.Fatalf("confirmName %q: motivo = %q", confirm, decoded.Items[0].Reason)
		}
		if _, err := os.Stat(folder); err != nil {
			t.Fatalf("confirmName %q: a pasta recusada sumiu do disco: %v", confirm, err)
		}
	}
}

func TestDeleteRequiresTheTypedWord(t *testing.T) {
	requireWindowsPaths(t)
	useTempConfig(t, offlineConfig())

	app, ts := newTestServer(t)
	root := tempDir(t)
	victim := writeTempFile(t, root, "importante.bin", 64)
	app.activeRoots = []string{root}

	for _, confirm := range []string{"", "excluir", "EXCLUI", "APAGAR"} {
		res, _ := postFileAction(t, app, ts, "/api/files/delete", map[string]any{
			"paths":       []string{victim},
			"confirmText": confirm,
		})
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("confirmText %q: status = %d, esperado 400", confirm, res.StatusCode)
		}
		if _, err := os.Stat(victim); err != nil {
			t.Fatalf("confirmText %q: o arquivo foi excluído mesmo sem confirmação: %v", confirm, err)
		}
	}
}

func TestDeleteRemovesFileFromTreeAndIndex(t *testing.T) {
	requireWindowsPaths(t)
	useTempConfig(t, offlineConfig())

	app, ts := newTestServer(t)
	root := tempDir(t)
	victim := writeTempFile(t, root, "copia-1.bin", 4096)
	twin := writeTempFile(t, root, "copia-2.bin", 4096)
	app.activeRoots = []string{root}

	indexFile(app, victim, 4096, "xxh64:abcdef")
	indexFile(app, twin, 4096, "xxh64:abcdef")

	if groups, files, wasted := app.Index.GetSummaryStats(); groups != 1 || files != 2 || wasted != 4096 {
		t.Fatalf("índice inicial = (%d grupos, %d arquivos, %d bytes), esperado (1, 2, 4096)", groups, files, wasted)
	}

	res, decoded := postFileAction(t, app, ts, "/api/files/delete", map[string]any{
		"paths":       []string{victim},
		"confirmText": "EXCLUIR",
	})

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", res.StatusCode)
	}
	if decoded.Deleted != 1 || decoded.Refused != 0 || decoded.Failed != 0 {
		t.Fatalf("contadores = %+v, esperado 1 excluído", decoded)
	}
	if decoded.Items[0].Status != recycle.StatusDeleted || decoded.Items[0].Path != victim {
		t.Fatalf("item = %+v, esperado deleted em %s", decoded.Items[0], victim)
	}
	if decoded.FreedBytes != 4096 {
		t.Fatalf("freedBytes = %d, esperado 4096", decoded.FreedBytes)
	}
	if _, err := os.Stat(victim); !os.IsNotExist(err) {
		t.Fatalf("o arquivo continua no disco (err = %v)", err)
	}
	if app.Tree.FindFile(victim) != nil {
		t.Fatal("o arquivo excluído continua na árvore")
	}
	if app.Tree.FindFile(twin) == nil {
		t.Fatal("a cópia que não foi excluída sumiu da árvore")
	}
	if groups, files, wasted := app.Index.GetSummaryStats(); groups != 0 || files != 0 || wasted != 0 {
		t.Fatalf("índice final = (%d grupos, %d arquivos, %d bytes), esperado zerado", groups, files, wasted)
	}
}

func TestDeleteFolderWithConfirmNameRemovesSubtree(t *testing.T) {
	requireWindowsPaths(t)
	useTempConfig(t, offlineConfig())

	app, ts := newTestServer(t)
	root := tempDir(t)
	folder := filepath.Join(root, "Backups")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatalf("não foi possível criar a pasta: %v", err)
	}
	inside := writeTempFile(t, folder, "dump.bin", 2048)
	app.activeRoots = []string{root}

	indexFile(app, inside, 2048, "xxh64:112233")
	app.Tree.EnsureDirNode(folder)

	res, decoded := postFileAction(t, app, ts, "/api/files/delete", map[string]any{
		"paths":       []string{folder},
		"confirmName": "backups", // a comparação é insensível a maiúsculas, como o NTFS
		"confirmText": "EXCLUIR",
	})

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", res.StatusCode)
	}
	if decoded.Deleted != 1 {
		t.Fatalf("contadores = %+v, esperado 1 pasta excluída", decoded)
	}
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Fatalf("a pasta continua no disco (err = %v)", err)
	}
	if app.Tree.FindDir(folder) != nil {
		t.Fatal("a pasta excluída continua na árvore")
	}
	if app.Tree.FindFile(inside) != nil {
		t.Fatal("o arquivo dentro da pasta excluída continua na árvore")
	}
}

func TestFileActionKeepsRequestOrderAndMixesOutcomes(t *testing.T) {
	requireWindowsPaths(t)
	useTempConfig(t, offlineConfig())

	app, ts := newTestServer(t)
	root := tempDir(t)
	outside := filepath.Join(tempDir(t), "fora.bin")
	if err := os.WriteFile(outside, make([]byte, 8), 0o600); err != nil {
		t.Fatalf("não foi possível criar %s: %v", outside, err)
	}
	ok := writeTempFile(t, root, "ok.bin", 128)
	missing := filepath.Join(root, "nunca-existiu.bin")
	app.activeRoots = []string{root}

	_, decoded := postFileAction(t, app, ts, "/api/files/delete", map[string]any{
		"paths":       []string{outside, ok, missing},
		"confirmText": "EXCLUIR",
	})

	if len(decoded.Items) != 3 {
		t.Fatalf("items = %d, esperado 3 na ordem do pedido", len(decoded.Items))
	}
	if decoded.Items[0].Path != outside || decoded.Items[0].Status != recycle.StatusRefused {
		t.Fatalf("item 0 = %+v, esperado refused em %s", decoded.Items[0], outside)
	}
	if decoded.Items[1].Path != ok || decoded.Items[1].Status != recycle.StatusDeleted {
		t.Fatalf("item 1 = %+v, esperado deleted em %s", decoded.Items[1], ok)
	}
	if decoded.Items[2].Path != missing || decoded.Items[2].Status != recycle.StatusFailed {
		t.Fatalf("item 2 = %+v, esperado failed em %s", decoded.Items[2], missing)
	}
	if decoded.Refused != 1 || decoded.Deleted != 1 || decoded.Failed != 1 {
		t.Fatalf("contadores = %+v, esperado 1 de cada", decoded)
	}
	if decoded.FreedBytes != 128 {
		t.Fatalf("freedBytes = %d, esperado 128", decoded.FreedBytes)
	}
}

func TestFileActionRejectsEmptyBatch(t *testing.T) {
	useTempConfig(t, offlineConfig())
	app, ts := newTestServer(t)

	res, _ := postFileAction(t, app, ts, "/api/files/recycle", map[string]any{"paths": []string{}})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 sem caminhos", res.StatusCode)
	}
}

func TestFileActionRejectsOtherMethods(t *testing.T) {
	useTempConfig(t, offlineConfig())
	app, ts := newTestServer(t)

	for _, target := range []string{"/api/files/recycle", "/api/files/delete"} {
		res := doAPI(t, app, ts, http.MethodGet, target, nil)
		if res.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("GET %s: status = %d, esperado 405", target, res.StatusCode)
		}
	}
}

func TestScopedRecycleRefusesOutsideRootsForTheAssistant(t *testing.T) {
	requireWindowsPaths(t)
	useTempConfig(t, offlineConfig())

	app, _ := newTestServer(t)
	root := tempDir(t)
	outside := writeTempFile(t, tempDir(t), "fora.bin", 8)
	app.activeRoots = []string{root}

	if app.MCPContext.RecycleFunc == nil {
		t.Fatal("o Assistente não recebeu a RecycleFunc com escopo")
	}

	res := app.MCPContext.RecycleFunc([]string{outside})
	if res.RefusedCount != 1 || res.SuccessCount != 0 {
		t.Fatalf("resultado = %+v, esperado 1 recusa", res)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("o arquivo recusado sumiu do disco: %v", err)
	}
}

// =========================================================================
// Sistema (contrato 1.3)
// =========================================================================

func TestSystemInfoReportsThreadOptions(t *testing.T) {
	useTempConfig(t, offlineConfig())
	app, ts := newTestServer(t)

	res := doAPI(t, app, ts, http.MethodGet, "/api/system/info", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", res.StatusCode)
	}

	var info systemInfoResponse
	decodeJSONBody(t, res, &info)

	if info.NumCPU != runtime.NumCPU() {
		t.Fatalf("numCPU = %d, esperado %d", info.NumCPU, runtime.NumCPU())
	}
	if info.MaxThreads != runtime.NumCPU()*4 {
		t.Fatalf("maxThreads = %d, esperado %d", info.MaxThreads, runtime.NumCPU()*4)
	}
	if len(info.ThreadOptions) < 2 || info.ThreadOptions[0] != 0 || info.ThreadOptions[1] != 4 {
		t.Fatalf("threadOptions = %v, esperado começar em [0 4 ...]", info.ThreadOptions)
	}
	for i := 2; i < len(info.ThreadOptions); i++ {
		if info.ThreadOptions[i] != info.ThreadOptions[i-1]*2 {
			t.Fatalf("threadOptions = %v, esperado potências de 2", info.ThreadOptions)
		}
	}
	if last := info.ThreadOptions[len(info.ThreadOptions)-1]; last > info.MaxThreads {
		t.Fatalf("threadOptions termina em %d, acima de maxThreads %d", last, info.MaxThreads)
	}
	if info.Version != Version {
		t.Fatalf("version = %q, esperado %q", info.Version, Version)
	}
	if info.Port != config.DefaultServerPort {
		t.Fatalf("port = %d, esperado %d vindo da Configuração", info.Port, config.DefaultServerPort)
	}
}

func TestSystemInfoPrefersTheRealListenerPort(t *testing.T) {
	useTempConfig(t, offlineConfig())
	app, ts := newTestServer(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("não foi possível abrir um listener: %v", err)
	}
	defer ln.Close()
	app.listener = ln

	res := doAPI(t, app, ts, http.MethodGet, "/api/system/info", nil)
	var info systemInfoResponse
	decodeJSONBody(t, res, &info)

	want := ln.Addr().(*net.TCPAddr).Port
	if info.Port != want {
		t.Fatalf("port = %d, esperado a porta real %d", info.Port, want)
	}
}

// =========================================================================
// Discos (contrato 1.12)
// =========================================================================

func TestDrivesCarryWSLAndSelectionFlags(t *testing.T) {
	useTempConfig(t, offlineConfig())
	app, ts := newTestServer(t)

	res := doAPI(t, app, ts, http.MethodGet, "/api/drives", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", res.StatusCode)
	}

	var list []map[string]any
	decodeJSONBody(t, res, &list)
	if len(list) == 0 {
		t.Fatal("nenhum disco devolvido, nem mesmo a reserva")
	}
	for i, d := range list {
		if _, ok := d["isWSL"]; !ok {
			t.Fatalf("disco %d sem isWSL: %v", i, d)
		}
		if _, ok := d["defaultSelected"]; !ok {
			t.Fatalf("disco %d sem defaultSelected: %v", i, d)
		}
	}
}

// =========================================================================
// Configuração (contrato 1.6)
// =========================================================================

func TestConfigRoundTripKeepsAIFieldsAndHidesTheKey(t *testing.T) {
	base := offlineConfig()
	base.AIProvider = config.ProviderOpenRouter
	base.AIOllamaModel = "qwen3:14b"
	base.AIOpenRouterModel = "anthropic/claude-sonnet-4.5"
	base.AIDryRunDefault = true
	configPath := useTempConfig(t, base)

	app, ts := newTestServer(t)

	res := doAPI(t, app, ts, http.MethodPost, "/api/config", map[string]any{"aiOpenRouterKey": "sk-teste-123"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST da chave: status = %d, esperado 200", res.StatusCode)
	}
	var saved map[string]string
	decodeJSONBody(t, res, &saved)
	if saved["status"] != "saved" {
		t.Fatalf("resposta = %v, esperado {\"status\":\"saved\"}", saved)
	}

	// Um POST parcial de outra tela não pode apagar nada do Assistente.
	res = doAPI(t, app, ts, http.MethodPost, "/api/config", map[string]any{"theme": "theme-claro", "uiZoom": 120})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST parcial: status = %d, esperado 200", res.StatusCode)
	}

	res = doAPI(t, app, ts, http.MethodGet, "/api/config", nil)
	var got map[string]any
	decodeJSONBody(t, res, &got)

	if got["theme"] != "theme-claro" {
		t.Fatalf("theme = %v, esperado theme-claro", got["theme"])
	}
	if got["aiProvider"] != config.ProviderOpenRouter {
		t.Fatalf("aiProvider = %v, esperado %s", got["aiProvider"], config.ProviderOpenRouter)
	}
	if got["aiOllamaModel"] != "qwen3:14b" {
		t.Fatalf("aiOllamaModel = %v, esperado qwen3:14b", got["aiOllamaModel"])
	}
	if got["aiOpenRouterModel"] != "anthropic/claude-sonnet-4.5" {
		t.Fatalf("aiOpenRouterModel = %v", got["aiOpenRouterModel"])
	}
	if got["hasOpenRouterKey"] != true {
		t.Fatalf("hasOpenRouterKey = %v, esperado true", got["hasOpenRouterKey"])
	}
	if v, present := got["aiOpenRouterKey"]; present && v != "" {
		t.Fatalf("a API devolveu a chave: %v", v)
	}
	if _, present := got["aiOpenRouterKeyEnc"]; present {
		t.Fatal("a API devolveu o blob protegido da chave")
	}

	// A chave continua utilizável do lado do servidor e não está em claro no disco.
	if key := config.OpenRouterKey(config.LoadConfig()); key != "sk-teste-123" {
		t.Fatalf("chave recuperada = %q, esperado sk-teste-123", key)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("não foi possível ler %s: %v", configPath, err)
	}
	if strings.Contains(string(raw), "sk-teste-123") {
		t.Fatal("a chave foi gravada em claro no arquivo de configuração")
	}
}

func TestConfigRejectsBrokenJSON(t *testing.T) {
	useTempConfig(t, offlineConfig())
	app, ts := newTestServer(t)

	req := newAPIRequest(t, app, ts, http.MethodPost, "/api/config", nil)
	req.Body = io.NopCloser(strings.NewReader("{isto não é json"))
	req.Header.Set("Content-Type", "application/json")

	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("POST /api/config: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400", res.StatusCode)
	}
}

func TestConfigRejectsOtherMethods(t *testing.T) {
	useTempConfig(t, offlineConfig())
	app, ts := newTestServer(t)

	res := doAPI(t, app, ts, http.MethodDelete, "/api/config", nil)
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, esperado 405", res.StatusCode)
	}
}

package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// useTempConfig points the package at a fresh file inside a temporary
// directory for the duration of a test.
func useTempConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "scanfile_config.json")
	previous := ConfigPath()
	SetConfigPath(path)
	t.Cleanup(func() { SetConfigPath(previous) })
	return path
}

func TestConfigLoadSave(t *testing.T) {
	testConfigFile := useTempConfig(t)

	// 1. Default config
	cfg := LoadConfig()
	if cfg.Theme != "theme-ochre-dark" {
		t.Errorf("Expected Theme theme-ochre-dark, got %s", cfg.Theme)
	}
	if cfg.WorkerThreads != 0 {
		t.Errorf("Expected WorkerThreads 0 (Auto), got %d", cfg.WorkerThreads)
	}
	if cfg.HashAlgorithm != "xxhash" {
		t.Errorf("Expected HashAlgorithm xxhash, got %s", cfg.HashAlgorithm)
	}
	if cfg.ServerPort != DefaultServerPort {
		t.Errorf("Expected ServerPort %d, got %d", DefaultServerPort, cfg.ServerPort)
	}

	// 2. Modify and Save
	cfg.Theme = "theme-light-sand"
	cfg.WorkerThreads = 16
	cfg.HashAlgorithm = "blake3"
	cfg.TreemapDepth = 12
	cfg.TreemapColorMode = "age"
	cfg.SelectedRoots = []string{"C:\\", "D:\\"}
	cfg.UIZoom = 125
	cfg.ChkFolderTopLevelOnly = true
	cfg.ComparePathA = "C:\\Docs"
	cfg.ComparePathB = "D:\\Backup"

	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}
	if _, err := os.Stat(testConfigFile); os.IsNotExist(err) {
		t.Fatalf("Expected config file to be created at %s", testConfigFile)
	}

	// 3. Reload and verify
	loaded := LoadConfig()
	if loaded.Theme != "theme-light-sand" {
		t.Errorf("Expected Theme theme-light-sand, got %s", loaded.Theme)
	}
	if loaded.WorkerThreads != 16 {
		t.Errorf("Expected WorkerThreads 16, got %d", loaded.WorkerThreads)
	}
	if loaded.HashAlgorithm != "blake3" {
		t.Errorf("Expected HashAlgorithm blake3, got %s", loaded.HashAlgorithm)
	}
	if loaded.TreemapDepth != 12 {
		t.Errorf("Expected TreemapDepth 12, got %d", loaded.TreemapDepth)
	}
	if loaded.UIZoom != 125 {
		t.Errorf("Expected UIZoom 125, got %d", loaded.UIZoom)
	}
	if !loaded.ChkFolderTopLevelOnly {
		t.Error("Expected ChkFolderTopLevelOnly true")
	}
	if loaded.ComparePathA != "C:\\Docs" || loaded.ComparePathB != "D:\\Backup" {
		t.Errorf("Expected ComparePaths C:\\Docs and D:\\Backup, got %s and %s", loaded.ComparePathA, loaded.ComparePathB)
	}
	if len(loaded.SelectedRoots) != 2 || loaded.SelectedRoots[0] != "C:\\" {
		t.Errorf("Expected SelectedRoots [C:\\ D:\\], got %v", loaded.SelectedRoots)
	}
}

func TestSaveConfigIsAtomicAndLeavesNoTemporary(t *testing.T) {
	path := useTempConfig(t)
	dir := filepath.Dir(path)

	cfg := GetDefaultConfig()
	cfg.Theme = "theme-1"
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Theme = "theme-2"
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("a gravação atômica deixou arquivos extras: %v", names)
	}
	if entries[0].Name() != filepath.Base(path) {
		t.Fatalf("arquivo inesperado: %s", entries[0].Name())
	}
	if LoadConfig().Theme != "theme-2" {
		t.Error("a segunda gravação deveria ter substituído a primeira")
	}
}

func TestSaveConfigCreatesMissingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub", "dir")
	previous := ConfigPath()
	SetConfigPath(filepath.Join(dir, "scanfile_config.json"))
	t.Cleanup(func() { SetConfigPath(previous) })

	if err := SaveConfig(GetDefaultConfig()); err != nil {
		t.Fatalf("SaveConfig devia criar o diretório: %v", err)
	}
}

func TestMergeJSONAppliesOnlyPresentKeys(t *testing.T) {
	cur := GetDefaultConfig()
	cur.Theme = "theme-antigo"
	cur.SelectedRoots = []string{`C:\`, `D:\`}
	cur.AIProvider = "openrouter"
	cur.AIOllamaModel = "qwen3:14b"
	cur.AIOllamaEndpoint = "http://127.0.0.1:1234"
	cur.AIOpenRouterModel = "anthropic/claude-3.7-sonnet"
	cur.AIDryRunDefault = false
	SetOpenRouterKey(&cur, "sk-segredo-123")

	got, err := MergeJSON(cur, []byte(`{"theme":"theme-novo","uiZoom":150}`))
	if err != nil {
		t.Fatalf("MergeJSON: %v", err)
	}

	if got.Theme != "theme-novo" {
		t.Errorf("Theme = %q, esperado theme-novo", got.Theme)
	}
	if got.UIZoom != 150 {
		t.Errorf("UIZoom = %d, esperado 150", got.UIZoom)
	}
	// C3: preferências de IA jamais são apagadas por uma gravação parcial.
	if got.AIProvider != "openrouter" {
		t.Errorf("AIProvider = %q, esperado openrouter", got.AIProvider)
	}
	if got.AIOllamaModel != "qwen3:14b" {
		t.Errorf("AIOllamaModel = %q", got.AIOllamaModel)
	}
	if got.AIOllamaEndpoint != "http://127.0.0.1:1234" {
		t.Errorf("AIOllamaEndpoint = %q", got.AIOllamaEndpoint)
	}
	if got.AIDryRunDefault {
		t.Error("AIDryRunDefault deveria continuar false")
	}
	if OpenRouterKey(got) != "sk-segredo-123" {
		t.Errorf("a chave OpenRouter foi perdida na gravação parcial: %q", OpenRouterKey(got))
	}
	if len(got.SelectedRoots) != 2 {
		t.Errorf("SelectedRoots = %v, deveria continuar com 2 raízes", got.SelectedRoots)
	}
}

func TestMergeJSONReplacesPresentSlices(t *testing.T) {
	cur := GetDefaultConfig()
	cur.SelectedRoots = []string{`C:\`, `D:\`}

	got, err := MergeJSON(cur, []byte(`{"selectedRoots":["E:\\"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.SelectedRoots) != 1 || got.SelectedRoots[0] != `E:\` {
		t.Errorf("SelectedRoots = %v, esperado [E:\\]", got.SelectedRoots)
	}
}

func TestMergeJSONSetsAndClearsTheSecret(t *testing.T) {
	cur := GetDefaultConfig()

	withKey, err := MergeJSON(cur, []byte(`{"aiOpenRouterKey":"sk-nova-chave"}`))
	if err != nil {
		t.Fatal(err)
	}
	if OpenRouterKey(withKey) != "sk-nova-chave" {
		t.Fatalf("a chave não foi guardada: %q", OpenRouterKey(withKey))
	}
	if withKey.AIOpenRouterKey != "" {
		t.Error("a chave em texto puro não pode ficar no campo aberto")
	}

	cleared, err := MergeJSON(withKey, []byte(`{"aiOpenRouterKey":""}`))
	if err != nil {
		t.Fatal(err)
	}
	if OpenRouterKey(cleared) != "" || cleared.AIOpenRouterKeyEnc != "" {
		t.Error("chave vazia deveria remover o segredo")
	}
}

func TestMergeJSONIgnoresTheStoredSecretFields(t *testing.T) {
	cur := GetDefaultConfig()
	SetOpenRouterKey(&cur, "sk-preservar")

	// A interface devolve o objeto inteiro que recebeu de Public(), onde os
	// campos do segredo estão vazios: isso não pode apagar a chave guardada.
	body, err := json.Marshal(cur.Public())
	if err != nil {
		t.Fatal(err)
	}
	got, err := MergeJSON(cur, body)
	if err != nil {
		t.Fatal(err)
	}
	if OpenRouterKey(got) != "sk-preservar" {
		t.Errorf("a chave foi perdida ao reenviar a config pública: %q", OpenRouterKey(got))
	}
	if got.HasOpenRouterKey {
		t.Error("hasOpenRouterKey nunca deve ser gravado na configuração")
	}
}

func TestMergeJSONRejectsInvalidBody(t *testing.T) {
	cur := GetDefaultConfig()
	cur.Theme = "intacto"
	got, err := MergeJSON(cur, []byte(`{isso não é json`))
	if err == nil {
		t.Fatal("esperado erro para corpo inválido")
	}
	if got.Theme != "intacto" {
		t.Error("a configuração atual deve voltar intacta quando o corpo é inválido")
	}
	if _, err := MergeJSON(cur, []byte(`[1,2,3]`)); err == nil {
		t.Fatal("esperado erro para corpo que não é um objeto JSON")
	}
}

func TestMergeJSONNormalizesProviderAndPort(t *testing.T) {
	cur := GetDefaultConfig()

	got, err := MergeJSON(cur, []byte(`{"aiProvider":"direct","serverPort":0}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.AIProvider != ProviderQuick {
		t.Errorf("AIProvider = %q, esperado %q", got.AIProvider, ProviderQuick)
	}
	if got.ServerPort != DefaultServerPort {
		t.Errorf("ServerPort = %d, esperado %d", got.ServerPort, DefaultServerPort)
	}

	got, err = MergeJSON(cur, []byte(`{"aiProvider":"DIRECT","workerThreads":0}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.AIProvider != ProviderQuick {
		t.Errorf("alias legado em maiúsculas: AIProvider = %q", got.AIProvider)
	}
	if got.WorkerThreads != 0 {
		t.Errorf("WorkerThreads 0 significa Auto e deve ser preservado, obtido %d", got.WorkerThreads)
	}

	got, err = MergeJSON(cur, []byte(`{"workerThreads":-4,"serverPort":70000}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkerThreads != 0 {
		t.Errorf("WorkerThreads negativo deveria virar Auto, obtido %d", got.WorkerThreads)
	}
	if got.ServerPort != DefaultServerPort {
		t.Errorf("porta fora da faixa deveria voltar ao padrão, obtido %d", got.ServerPort)
	}
}

func TestSecretRoundTripAndNeverLeaks(t *testing.T) {
	const plain = "sk-or-v1-chave-secreta-de-teste"

	cfg := GetDefaultConfig()
	SetOpenRouterKey(&cfg, plain)

	if cfg.AIOpenRouterKey != "" {
		t.Error("SetOpenRouterKey não pode deixar a chave em texto puro")
	}
	if cfg.AIOpenRouterKeyEnc == "" {
		t.Fatal("SetOpenRouterKey deveria preencher o campo protegido")
	}
	if got := OpenRouterKey(cfg); got != plain {
		t.Fatalf("OpenRouterKey = %q, esperado %q", got, plain)
	}

	pub := cfg.Public()
	if pub.AIOpenRouterKey != "" || pub.AIOpenRouterKeyEnc != "" {
		t.Error("Public() não pode devolver a chave nem o blob protegido")
	}
	if !pub.HasOpenRouterKey {
		t.Error("Public() deveria informar que existe uma chave configurada")
	}
	data, err := json.Marshal(pub)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), plain) {
		t.Fatalf("a chave apareceu no JSON público: %s", data)
	}
	if !strings.Contains(string(data), `"hasOpenRouterKey":true`) {
		t.Errorf("hasOpenRouterKey ausente do JSON público: %s", data)
	}

	// A configuração original continua utilizável depois de Public().
	if OpenRouterKey(cfg) != plain {
		t.Error("Public() não pode alterar a configuração de origem")
	}

	empty := GetDefaultConfig().Public()
	if empty.HasOpenRouterKey {
		t.Error("sem chave configurada, hasOpenRouterKey deve ser false")
	}
}

func TestSetOpenRouterKeyEmptyRemovesSecret(t *testing.T) {
	cfg := GetDefaultConfig()
	SetOpenRouterKey(&cfg, "sk-abc")
	SetOpenRouterKey(&cfg, "   ")
	if cfg.AIOpenRouterKeyEnc != "" || OpenRouterKey(cfg) != "" {
		t.Error("chave em branco deveria remover o segredo")
	}
}

func TestSecretIsNotStoredInPlaintextOnDisk(t *testing.T) {
	path := useTempConfig(t)
	const plain = "sk-or-v1-nao-deve-vazar"

	cfg := GetDefaultConfig()
	SetOpenRouterKey(&cfg, plain)
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), plain) {
		t.Fatalf("a chave foi gravada em texto puro:\n%s", data)
	}

	loaded := LoadConfig()
	if OpenRouterKey(loaded) != plain {
		t.Fatalf("a chave não sobreviveu ao ciclo de gravação/leitura: %q", OpenRouterKey(loaded))
	}
}

func TestLoadConfigMigratesLegacyPlaintextKey(t *testing.T) {
	path := useTempConfig(t)
	const plain = "sk-legado"

	legacy := `{"theme":"theme-ochre-dark","aiOpenRouterKey":"` + plain + `"}`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded := LoadConfig()
	if loaded.AIOpenRouterKey != "" {
		t.Error("a chave legada não pode permanecer no campo aberto depois da carga")
	}
	if OpenRouterKey(loaded) != plain {
		t.Fatalf("a chave legada foi perdida: %q", OpenRouterKey(loaded))
	}
	if loaded.Public().AIOpenRouterKey != "" {
		t.Error("Public() vazaria a chave legada")
	}

	if err := SaveConfig(loaded); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), plain) {
		t.Fatalf("a gravação não migrou a chave legada:\n%s", data)
	}
}

func TestLoadConfigKeepsDefaultsOnBrokenFile(t *testing.T) {
	path := useTempConfig(t)
	if err := os.WriteFile(path, []byte("{lixo"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := LoadConfig()
	if cfg.Theme != GetDefaultConfig().Theme || cfg.ServerPort != DefaultServerPort {
		t.Errorf("arquivo corrompido deveria devolver os padrões, obtido %+v", cfg)
	}
}

func TestConfigPathPrefersExistingFileInWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	local := filepath.Join(dir, configFileName)
	if err := os.WriteFile(local, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := resolveConfigPath()
	// Em Windows o diretório temporário pode vir por um caminho curto (8.3),
	// então comparamos os caminhos avaliados.
	wantResolved, _ := filepath.EvalSymlinks(local)
	gotResolved, _ := filepath.EvalSymlinks(got)
	if !strings.EqualFold(gotResolved, wantResolved) {
		t.Errorf("resolveConfigPath = %q, esperado %q", got, local)
	}
}

func TestConfigPathFallsBackNextToExecutable(t *testing.T) {
	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	exe, err := os.Executable()
	if err != nil {
		t.Skip("executável não determinável nesta plataforma")
	}
	want := filepath.Join(filepath.Dir(exe), configFileName)
	if got := resolveConfigPath(); got != want {
		t.Errorf("resolveConfigPath = %q, esperado %q", got, want)
	}
}

func TestConfigPathIsResolvedOnlyOnce(t *testing.T) {
	previous := ConfigPath()
	t.Cleanup(func() { SetConfigPath(previous) })

	first := ConfigPath()
	if first == "" {
		t.Fatal("ConfigPath devolveu vazio")
	}
	if second := ConfigPath(); second != first {
		t.Errorf("ConfigPath mudou entre chamadas: %q -> %q", first, second)
	}
}

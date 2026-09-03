package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// fakeOllama serves /api/tags and /api/version like a real local daemon.
func fakeOllama(t *testing.T, models []map[string]interface{}, version string) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"models": models})
	})
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"version": version})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func findModel(models []ModelInfo, id string) *ModelInfo {
	for i := range models {
		if models[i].ID == id {
			return &models[i]
		}
	}
	return nil
}

func TestBuildCatalog_ModelosCuradosConformeContrato(t *testing.T) {
	srv := fakeOllama(t, nil, "0.6.1")

	cat := BuildCatalogWithMemory(context.Background(), srv.URL, 32*1024*1024*1024)

	esperados := []struct {
		id     string
		vision bool
		tools  bool
	}{
		{"qwen3-vl:8b", true, true},
		{"qwen2.5vl:7b", true, false},
		{"gemma3:12b", true, false},
		{"qwen3:14b", false, true},
		{"gpt-oss:20b", false, true},
		{"devstral:24b", false, true},
	}

	for _, e := range esperados {
		m := findModel(cat.Models, e.id)
		if m == nil {
			t.Fatalf("modelo %q ausente do catálogo", e.id)
		}
		if m.Provider != string(ProviderOllama) {
			t.Fatalf("%s: provedor %q inesperado", e.id, m.Provider)
		}
		if m.Vision != e.vision {
			t.Fatalf("%s: vision=%v, esperado %v", e.id, m.Vision, e.vision)
		}
		if m.Tools != e.tools {
			t.Fatalf("%s: tools=%v, esperado %v", e.id, m.Tools, e.tools)
		}
		if m.SizeGB <= 0 || m.SizeGB > CatalogMaxSizeGB {
			t.Fatalf("%s: sizeGB=%v fora do limite de %.0f GB", e.id, m.SizeGB, CatalogMaxSizeGB)
		}
	}

	def := findModel(cat.Models, DefaultOllamaModel)
	if def == nil || !def.Recommended {
		t.Fatalf("%s deveria ser o modelo recomendado", DefaultOllamaModel)
	}

	// Nenhum modelo curado do Ollama pode passar do teto de ~14 GB.
	for _, m := range cat.Models {
		if m.Provider == string(ProviderOllama) && m.SizeGB > CatalogMaxSizeGB {
			t.Fatalf("modelo %s (%.1f GB) ultrapassa o teto do catálogo", m.ID, m.SizeGB)
		}
	}
}

func TestBuildCatalog_MarcaInstaladosETamanhosDetectados(t *testing.T) {
	srv := fakeOllama(t, []map[string]interface{}{
		{"name": "qwen3-vl:8b", "model": "qwen3-vl:8b", "size": int64(6_000_000_000)},
		{"name": "gemma3:12b", "model": "gemma3:12b", "size": int64(8_100_000_000)},
		{"name": "nomic-embed-text:latest", "model": "nomic-embed-text:latest", "size": int64(274_000_000)},
	}, "0.6.1")

	cat := BuildCatalogWithMemory(context.Background(), srv.URL, 16*1024*1024*1024)

	if !cat.OllamaOnline {
		t.Fatal("o Ollama falso deveria ser detectado como online")
	}
	if cat.OllamaVersion != "0.6.1" {
		t.Fatalf("versão inesperada: %q", cat.OllamaVersion)
	}
	if len(cat.InstalledModels) != 3 {
		t.Fatalf("esperados 3 modelos instalados, obtidos %v", cat.InstalledModels)
	}

	if m := findModel(cat.Models, "qwen3-vl:8b"); m == nil || !m.Installed {
		t.Fatal("qwen3-vl:8b deveria estar marcado como instalado")
	}
	if m := findModel(cat.Models, "gemma3:12b"); m == nil || !m.Installed {
		t.Fatal("gemma3:12b deveria estar marcado como instalado")
	}
	if m := findModel(cat.Models, "qwen3:14b"); m == nil || m.Installed {
		t.Fatal("qwen3:14b não está instalado no Ollama falso")
	}

	// Modelo detectado fora do catálogo curado entra com o tamanho informado.
	extra := findModel(cat.Models, "nomic-embed-text:latest")
	if extra == nil {
		t.Fatal("modelo instalado fora do catálogo deveria aparecer na lista")
	}
	if !extra.Installed {
		t.Fatal("modelo detectado deveria estar marcado como instalado")
	}
	if extra.SizeGB < 0.2 || extra.SizeGB > 0.3 {
		t.Fatalf("tamanho detectado incorreto: %.3f GB", extra.SizeGB)
	}

	// Instalados aparecem primeiro.
	if !cat.Models[0].Installed {
		t.Fatalf("modelos instalados deveriam vir primeiro, o primeiro é %s", cat.Models[0].ID)
	}
}

func TestBuildCatalog_OllamaOffline(t *testing.T) {
	cat := BuildCatalogWithMemory(context.Background(), "http://127.0.0.1:1", 16*1024*1024*1024)

	if cat.OllamaOnline {
		t.Fatal("endpoint inválido não pode ser reportado como online")
	}
	if len(cat.InstalledModels) != 0 {
		t.Fatalf("nenhum modelo deveria ser detectado: %v", cat.InstalledModels)
	}
	if findModel(cat.Models, DefaultOllamaModel) == nil {
		t.Fatal("o catálogo curado deve continuar disponível offline")
	}
	for _, m := range cat.Models {
		if m.Installed {
			t.Fatalf("modelo %s não pode estar instalado com o Ollama offline", m.ID)
		}
	}
}

func TestBuildCatalog_FitsMemory(t *testing.T) {
	srv := fakeOllama(t, nil, "0.6.1")

	// Máquina de 8 GB: só os modelos pequenos cabem.
	small := BuildCatalogWithMemory(context.Background(), srv.URL, 8*1024*1024*1024)
	if m := findModel(small.Models, "qwen3-vl:8b"); m == nil || !m.FitsMemory {
		t.Fatal("qwen3-vl:8b (6 GB) deveria caber em 8 GB")
	}
	if m := findModel(small.Models, "gpt-oss:20b"); m == nil || m.FitsMemory {
		t.Fatal("gpt-oss:20b (14 GB) não cabe em 8 GB")
	}

	// Máquina de 32 GB: tudo cabe.
	big := BuildCatalogWithMemory(context.Background(), srv.URL, 32*1024*1024*1024)
	for _, m := range big.Models {
		if m.Provider == string(ProviderOllama) && !m.FitsMemory {
			t.Fatalf("modelo %s (%.1f GB) deveria caber em 32 GB", m.ID, m.SizeGB)
		}
	}

	// Memória desconhecida: assume o teto do catálogo.
	unknown := BuildCatalogWithMemory(context.Background(), srv.URL, 0)
	if unknown.TotalMemoryGB != 0 {
		t.Fatalf("memória desconhecida deveria ser reportada como 0, obtido %v", unknown.TotalMemoryGB)
	}
	if m := findModel(unknown.Models, DefaultOllamaModel); m == nil || !m.FitsMemory {
		t.Fatal("com memória desconhecida o modelo padrão deve ser considerado compatível")
	}
}

func TestBuildCatalog_JSONConformeSecao111(t *testing.T) {
	srv := fakeOllama(t, nil, "0.6.1")
	cat := BuildCatalogWithMemory(context.Background(), srv.URL, 16*1024*1024*1024)

	m := findModel(cat.Models, DefaultOllamaModel)
	if m == nil {
		t.Fatal("modelo padrão ausente")
	}

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"id", "name", "provider", "sizeGB", "vision", "tools", "installed", "recommended", "fitsMemory"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("campo %q ausente no JSON de ModelInfo: %s", key, string(data))
		}
	}
	if got["name"] != "Qwen3-VL 8B" {
		t.Fatalf("nome exibido inesperado: %v", got["name"])
	}
}

// O catálogo não pode mais listar arquivos .gguf nem criar a pasta models/.
func TestBuildCatalog_NaoCriaPastaModels(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	srv := fakeOllama(t, nil, "0.6.1")
	cat := BuildCatalogWithMemory(context.Background(), srv.URL, 16*1024*1024*1024)

	if _, err := os.Stat(filepath.Join(dir, "models")); err == nil {
		t.Fatal("a pasta models/ não pode mais ser criada pelo catálogo")
	}
	for _, m := range cat.Models {
		if m.Provider == string(ProviderQuick) || m.Provider == "direct" {
			t.Fatalf("o catálogo não deve listar modelos do provedor %q", m.Provider)
		}
	}
}

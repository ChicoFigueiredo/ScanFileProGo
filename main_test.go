package main

import (
	"os"
	"strings"
	"testing"

	"scanfile/pkg/server"
)

func TestResolveStartupPortPrefersExplicitFlag(t *testing.T) {
	cases := []struct {
		name     string
		flagPort int
		handoff  bool
		info     server.InstanceInfo
		want     int
	}{
		{"--port vence sempre", 5000, false, server.InstanceInfo{Port: 47321}, 5000},
		{"--port vence no handoff", 5000, true, server.InstanceInfo{Port: 47321}, 5000},
		{"handoff herda a porta anterior", 0, true, server.InstanceInfo{Port: 47321}, 47321},
		{"handoff sem porta cai no padrão", 0, true, server.InstanceInfo{}, 0},
		{"execução normal usa a porta da Configuração", 0, false, server.InstanceInfo{Port: 47321}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveStartupPort(tc.flagPort, tc.handoff, tc.info); got != tc.want {
				t.Errorf("resolveStartupPort = %d, quer %d", got, tc.want)
			}
		})
	}
}

func TestEdgeWindowArgsPointsToTheServerURL(t *testing.T) {
	args := edgeWindowArgs("http://127.0.0.1:47321")
	if len(args) == 0 || args[0] != "--app=http://127.0.0.1:47321" {
		t.Fatalf("primeiro argumento = %q, quer --app com a URL do servidor", args)
	}
	var hasProfile bool
	for _, a := range args {
		if strings.HasPrefix(a, "--user-data-dir=") && strings.Contains(a, "ScanFile_Webview_Profile") {
			hasProfile = true
		}
	}
	if !hasProfile {
		t.Errorf("faltou o perfil dedicado que faz o Edge reaproveitar a Janela: %v", args)
	}
}

func TestFocusRunningInstanceIgnoresMissingAndStaleFiles(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())

	if focusRunningInstance() {
		t.Error("sem instance.json não há instância para focar")
	}

	// Arquivo obsoleto: porta que ninguém escuta.
	if err := server.WriteInstanceFile(server.InstanceInfo{Port: 1, PID: os.Getpid() + 1000}); err != nil {
		t.Fatalf("gravação falhou: %v", err)
	}
	if focusRunningInstance() {
		t.Error("instance.json obsoleto não pode impedir uma nova execução")
	}
}

func TestFocusRunningInstanceFindsLiveInstance(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())

	running := server.NewAppServer(nil)
	defer running.Stop()
	if _, err := running.Start(0); err != nil {
		t.Skipf("não foi possível subir a instância de teste: %v", err)
	}

	// O PID real é o nosso (mesmo processo); finge outro para exercitar o caminho.
	if err := server.WriteInstanceFile(server.InstanceInfo{
		Port: running.Port(), PID: os.Getpid() + 1000, Token: "t",
	}); err != nil {
		t.Fatalf("gravação falhou: %v", err)
	}

	if !focusRunningInstance() {
		t.Error("uma instância viva deveria receber o foco e encerrar esta execução")
	}
}

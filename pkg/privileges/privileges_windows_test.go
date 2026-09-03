//go:build windows

package privileges

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestEscapeArgsQuotesSpacesAndSurvivesRoundTrip(t *testing.T) {
	args := []string{
		`--handoff`,
		`--config=C:\Program Files\ScanFile\scanfile_config.json`,
		`--label=Disco Local (C:)`,
		`--regex=a"b`,
		`C:\pasta com espaço\`,
	}

	line := escapeArgs(args)
	if !strings.Contains(line, `"--config=C:\Program Files\ScanFile\scanfile_config.json"`) {
		t.Errorf("argumento com espaços não foi citado: %s", line)
	}

	// O Windows precisa devolver exatamente os mesmos argumentos ao filho.
	decomposed, err := windows.DecomposeCommandLine("scanfile.exe " + line)
	if err != nil {
		t.Fatalf("DecomposeCommandLine: %v", err)
	}
	got := decomposed[1:]
	if !reflect.DeepEqual(got, args) {
		t.Errorf("argumentos deformados na ida e volta:\n  enviado: %q\n  recebido: %q", args, got)
	}
}

func TestEscapeArgsSkipsEmptyArguments(t *testing.T) {
	if got := escapeArgs([]string{"--a", "", "--b"}); got != `--a --b` {
		t.Errorf("escapeArgs = %q", got)
	}
	if got := escapeArgs(nil); got != "" {
		t.Errorf("escapeArgs(nil) = %q", got)
	}
}

func TestBuildRelaunchArgsDropsInheritedElevationFlags(t *testing.T) {
	raw := []string{
		"--admin",
		"-admin",
		"--elevated-child",
		"--ipc-addr=127.0.0.1:1",
		"--parent-pid=42",
		"--debug",
		"--port=47321",
	}

	got := buildRelaunchArgs(raw, "127.0.0.1:5555", 1234)

	want := []string{
		"--elevated-child",
		"--ipc-addr=127.0.0.1:5555",
		"--parent-pid=1234",
		"--debug",
		"--port=47321",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildRelaunchArgs =\n  %q\nesperado\n  %q", got, want)
	}
}

func TestWaitForProcessExitTimesOutOnLiveProcess(t *testing.T) {
	start := time.Now()
	err := WaitForProcessExit(uint32(os.Getpid()), 150*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("esperado erro de tempo esgotado para um processo vivo")
	}
	if elapsed > 3*time.Second {
		t.Errorf("WaitForProcessExit não respeitou o tempo limite: %v", elapsed)
	}
}

func TestWaitForProcessExitReturnsForUnknownProcess(t *testing.T) {
	// PID que não existe: o processo já terminou, nada a esperar.
	if err := WaitForProcessExit(0xFFFFFFF0, 2*time.Second); err != nil {
		t.Errorf("processo inexistente deveria ser tratado como encerrado: %v", err)
	}
}

func TestWaitForProcessExitRejectsInvalidPID(t *testing.T) {
	if err := WaitForProcessExit(0, time.Second); err == nil {
		t.Error("pid 0 deveria ser recusado")
	}
}

func TestMonitorParentProcessExitsWhenParentCannotBeOpened(t *testing.T) {
	done := make(chan int, 1)
	previous := processExit
	processExit = func(code int) { done <- code }
	defer func() { processExit = previous }()

	// M7: sem esse encerramento imediato o filho elevado vira órfão.
	MonitorParentProcess(0xFFFFFF0)

	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("código de saída = %d, esperado 0", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("MonitorParentProcess não encerrou o processo quando OpenProcess falhou")
	}
}

func TestMonitorParentProcessIgnoresInvalidPID(t *testing.T) {
	done := make(chan int, 1)
	previous := processExit
	processExit = func(code int) { done <- code }
	defer func() { processExit = previous }()

	MonitorParentProcess(0)
	MonitorParentProcess(-1)

	select {
	case <-done:
		t.Fatal("sem PID de pai não há nada a monitorar e nada a encerrar")
	case <-time.After(300 * time.Millisecond):
	}
}

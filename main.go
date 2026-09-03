package main

import (
	"embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"scanfile/pkg/ai"
	"scanfile/pkg/config"
	"scanfile/pkg/mcp"
	"scanfile/pkg/privileges"
	"scanfile/pkg/server"
)

//go:embed ui/*
var embeddedUI embed.FS

var (
	// Version can be overwritten during build with -ldflags "-X main.Version=0.1.0"
	Version = "0.1.0"
	Commit  = "dev"
	Date    = "now"
)

// autoSaveDir é a pasta dos Snapshots e do Autosave (contrato 1.7).
const autoSaveDir = "saved_scans"

func main() {
	// Parse CLI Flags
	flagVersion := flag.Bool("version", false, "Exibe a versão do ScanFile Pro")
	flagLog := flag.Bool("log", false, "Habilita gravação de logs gerais da aplicação em arquivo de log na pasta local")
	flagDebug := flag.Bool("debug", false, "Habilita modo debug com log ultra-detalhado de requisições, I/O, memória RAM e erros em arquivo local")
	flagAdmin := flag.Bool("admin", false, "Executa diretamente como Administrador (solicita elevação UAC se necessário)")
	flagPort := flag.Int("port", 0, "Porta HTTP do servidor (0 usa a porta fixa da Configuração, padrão 47321)")
	flagNoWindow := flag.Bool("no-window", false, "Inicia apenas o servidor de backend sem abrir janela gráfica")
	flagMCP := flag.Bool("mcp", false, "Executa o servidor Model Context Protocol (MCP) via Stdio para conexão com Claude Desktop/Antigravity/Cursor")
	flagElevatedChild := flag.Bool("elevated-child", false, "Flag interna de instância filha elevada")
	flagHandoff := flag.Bool("handoff", false, "Flag interna: assume a porta e o token da Sessão da instância anterior (handoff de elevação), sem abrir uma segunda Janela")
	flagIPCAddr := flag.String("ipc-addr", "", "Endereço IPC interno para redirecionar saída ao console pai")
	flagParentPID := flag.Int("parent-pid", 0, "PID do processo pai para encerramento automático sem processos zumbis")
	flag.Parse()

	// A versão do build é a mesma reportada por GET /api/instance e /api/system/info.
	server.Version = Version

	if *flagVersion {
		fmt.Printf("ScanFile Pro v%s (commit: %s, built: %s)\n", Version, Commit, Date)
		return
	}

	// MCP Server Mode over Stdio (o contexto vem do último Autosave, Q11)
	if *flagMCP {
		cfg := config.LoadConfig()
		ollamaClient := ai.NewOllamaClient(cfg.AIOllamaEndpoint)
		mcpCtx, snapshot, err := mcp.NewMCPToolsContextFromAutosave(autoSaveDir, ollamaClient, cfg.AIOllamaModel)
		if err != nil {
			// Sem Autosave não há Raízes Varridas: as ferramentas recusariam
			// tudo, então é mais honesto não subir o servidor MCP.
			log.Fatalf("[X] Servidor MCP não iniciado: %v", err)
		}
		if snapshot != nil {
			log.Printf("[+] Autosave carregado para o MCP: %d arquivos, %d pastas, raízes %v\n",
				snapshot.TotalFiles, snapshot.TotalDirs, snapshot.Roots)
		}
		if err := mcp.StartStdioServer(mcpCtx); err != nil {
			log.Fatalf("Erro no servidor MCP: %v", err)
		}
		return
	}

	if *flagDebug {
		server.DebugMode = true
	}

	// Anti-Zombie Process Guard: If running as child, monitor parent PID
	if *flagParentPID > 0 {
		privileges.MonitorParentProcess(*flagParentPID)
	}

	// Direct Administrator Elevation Check
	if *flagAdmin && runtime.GOOS == "windows" {
		privStatus := privileges.CheckPrivilegeStatus()
		if !privStatus.IsElevated {
			log.Println("[*] Flag --admin detectada. Solicitando elevação de Administrador ao Windows...")
			log.Println("[*] O processo com privilégios rodará em segundo plano e cuspirá a saída diretamente neste terminal.")
			err := privileges.RelaunchAsAdminWithIPC(os.Args[1:])
			if err != nil {
				log.Fatalf("[X] %v", err)
			}
			return
		}
	}

	// Setup Output Writers (Console, IPC stream, and Disk file)
	var writers []io.Writer
	writers = append(writers, os.Stdout)

	// If running as hidden elevated child, connect back to parent console
	if *flagElevatedChild && *flagIPCAddr != "" {
		conn, err := net.DialTimeout("tcp", *flagIPCAddr, 5*time.Second)
		if err == nil {
			writers = append(writers, conn)
			// Monitor parent connection: if parent terminal is closed, shutdown cleanly
			go func() {
				buf := make([]byte, 16)
				for {
					_, errRead := conn.Read(buf)
					if errRead != nil {
						os.Exit(0)
					}
				}
			}()
		}
	}

	// Setup Disk File Logging if --log or --debug flag is passed
	if *flagLog || *flagDebug {
		logDir := "logs"
		_ = os.MkdirAll(logDir, 0755)
		prefix := "scanfile_app"
		if *flagDebug {
			prefix = "scanfile_debug"
		}
		logFileName := filepath.Join(logDir, fmt.Sprintf("%s_%s.log", prefix, time.Now().Format("2006-01-02_150405")))
		logFile, err := os.OpenFile(logFileName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err == nil {
			writers = append(writers, logFile)
			log.Printf("[+] Log em disco ativado: %s\n", logFileName)
		} else {
			log.Printf("[!] Não foi possível criar arquivo de log em disco: %v\n", err)
		}
	}

	log.SetOutput(io.MultiWriter(writers...))

	// Periodic RAM & Goroutines monitor when in Debug Mode
	if *flagDebug {
		log.Println("[DEBUG] Modo Debug Ativado! Rastreando HTTP, Goroutines, I/O e Memória RAM em tempo real.")
		go func() {
			var mem runtime.MemStats
			ticker := time.NewTicker(10 * time.Second)
			for range ticker.C {
				runtime.ReadMemStats(&mem)
				log.Printf("[DEBUG MEM] RAM em Uso (Alloc): %d MB | Total Alocado: %d MB | Heap Sys: %d MB | NumGC: %d | Goroutines: %d\n",
					mem.Alloc/1024/1024, mem.TotalAlloc/1024/1024, mem.Sys/1024/1024, mem.NumGC, runtime.NumGoroutine())
			}
		}()
	}

	log.Println("=========================================================")
	log.Println("           ScanFile Pro - Windows Native Engine          ")
	log.Println("=========================================================")

	// Enable high-privilege Windows security tokens (SeBackupPrivilege / SeRestorePrivilege)
	if runtime.GOOS == "windows" {
		privResults, _ := privileges.EnableAllBackupPrivileges()
		privStatus := privileges.CheckPrivilegeStatus()
		if privStatus.IsElevated {
			log.Println("[+] Executando com privilégios de Administrador (UAC Elevado)")
			if privResults["SeBackupPrivilege"] {
				log.Println("[+] SeBackupPrivilege ATIVADO (Bypass de ACLs do NTFS para leitura total)")
			}
		} else {
			log.Println("[!] Executando em modo de usuário padrão. Para acesso total, execute com --admin.")
		}
	}

	// Strip "ui" prefix from embedded filesystem
	uiSubFS, err := fs.Sub(embeddedUI, "ui")
	if err != nil {
		log.Fatalf("Falha ao carregar interface embutida: %v", err)
	}

	// Instância única (contrato 1.9): se já houver uma instância nossa em
	// execução, traz a Janela dela para frente e encerra esta execução.
	if !*flagHandoff && focusRunningInstance() {
		return
	}

	// Create and start backend application server
	appServer := server.NewAppServer(uiSubFS)
	appServer.SetNoWindow(*flagNoWindow)
	appServer.SetWindowOpener(launchNativeWindow)

	port := *flagPort
	if *flagHandoff {
		info, errInfo := server.ReadInstanceFile()
		if errInfo != nil {
			log.Fatalf("[X] --handoff sem instância anterior em %s: %v", server.InstanceFilePath(), errInfo)
		}
		port = resolveStartupPort(*flagPort, true, info)
		appServer.AdoptSession(info.Token)
		appServer.SetHandoffMode(true)
		// Anúncio: o pai ainda ocupa a porta e só a libera ao ver este PID.
		if errAnnounce := server.AnnounceHandoff(port, info.Token); errAnnounce != nil {
			log.Printf("[!] Não foi possível anunciar o handoff: %v\n", errAnnounce)
		}
		log.Printf("[*] Handoff: assumindo a porta %d e a Sessão da instância anterior (PID %d).\n", port, info.PID)
	}

	serverURL, err := appServer.Start(port)
	if err != nil {
		if errors.Is(err, server.ErrInstanceAlreadyRunning) {
			log.Println("[*] Outra instância assumiu a porta primeiro; trazendo a Janela dela para frente.")
			focusRunningInstance()
			return
		}
		log.Fatalf("Erro ao iniciar servidor de aplicação: %v", err)
	}

	log.Printf("[+] Servidor de backend ativo em: %s\n", serverURL)

	// Launch Native Windows Desktop Window if not disabled. No handoff a Janela
	// já está aberta apontando para a mesma porta: não abrimos uma segunda.
	if !*flagNoWindow && !*flagHandoff {
		go func() {
			time.Sleep(300 * time.Millisecond)
			launchNativeWindow(serverURL)
		}()
	}

	// Graceful shutdown handling: Ctrl+C, ausência da Janela ou handoff.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case <-sigChan:
		log.Println("\n[*] Encerrando ScanFile Pro...")
	case <-appServer.Done():
		log.Println("[*] Servidor encerrado (Janela fechada ou handoff concluído).")
	}
	appServer.Stop()
	log.Println("[*] Finalizado.")
}

// focusRunningInstance procura uma instância nossa já em execução por
// instance.json e, se ela responder, pede o foco da Janela. Devolve true
// quando esta execução deve simplesmente encerrar.
func focusRunningInstance() bool {
	info, err := server.ReadInstanceFile()
	if err != nil || info.Port <= 0 || info.PID == os.Getpid() {
		return false
	}
	if _, ok := server.ProbeInstance(info.Port, 2*time.Second); !ok {
		// Arquivo obsoleto (instância anterior morreu sem limpar).
		return false
	}
	if err := server.FocusInstance(info, 5*time.Second); err != nil {
		log.Printf("[!] O ScanFile Pro já está em execução (PID %d, porta %d), mas a Janela não respondeu: %v\n",
			info.PID, info.Port, err)
		return true
	}
	log.Printf("[*] O ScanFile Pro já está em execução (PID %d, porta %d). A Janela existente foi trazida para frente.\n",
		info.PID, info.Port)
	return true
}

// resolveStartupPort decide a porta de escuta: --port sempre vence; no handoff,
// a porta herdada da instância anterior; senão 0, que Start resolve para a
// porta fixa da Configuração.
func resolveStartupPort(flagPort int, handoff bool, info server.InstanceInfo) int {
	if flagPort > 0 {
		return flagPort
	}
	if handoff && info.Port > 0 {
		return info.Port
	}
	return 0
}

// launchNativeWindow opens the application in a native Windows Edge App window or native browser.
func launchNativeWindow(targetURL string) {
	if runtime.GOOS != "windows" {
		_ = exec.Command("xdg-open", targetURL).Start()
		return
	}

	if edgeExe := edgeExecutablePath(); edgeExe != "" {
		cmd := exec.Command(edgeExe, edgeWindowArgs(targetURL)...)
		if err := cmd.Start(); err == nil {
			log.Printf("[+] Janela nativa do Windows iniciada com sucesso via Edge App Mode (%s)\n", edgeExe)
			return
		}
	}

	// Fallback to rundll32 / default Windows browser
	log.Println("[*] Abrindo navegador padrão do sistema...")
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", targetURL).Start()
}

// edgeExecutablePath localiza o msedge.exe instalado, ou "" se não houver.
func edgeExecutablePath() string {
	candidates := []string{
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
		filepath.Join(os.Getenv("LOCALAPPDATA"), `Microsoft\Edge\Application\msedge.exe`),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// edgeWindowArgs monta os argumentos da Janela (Edge em modo aplicativo). O
// perfil dedicado faz o Edge reaproveitar a mesma Janela em vez de abrir outra.
func edgeWindowArgs(targetURL string) []string {
	return []string{
		fmt.Sprintf("--app=%s", targetURL),
		fmt.Sprintf("--user-data-dir=%s", filepath.Join(os.TempDir(), "ScanFile_Webview_Profile")),
		"--window-size=1360,860",
		"--disable-features=TranslateUI",
		"--disable-extensions",
	}
}

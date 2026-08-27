package main

import (
	"embed"
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

func main() {
	// Parse CLI Flags
	flagVersion := flag.Bool("version", false, "Exibe a versão do ScanFile Pro")
	flagLog := flag.Bool("log", false, "Habilita gravação de logs gerais da aplicação em arquivo de log na pasta local")
	flagDebug := flag.Bool("debug", false, "Habilita modo debug com log ultra-detalhado de requisições, I/O, memória RAM e erros em arquivo local")
	flagAdmin := flag.Bool("admin", false, "Executa diretamente como Administrador (solicita elevação UAC se necessário)")
	flagPort := flag.Int("port", 0, "Porta HTTP do servidor (0 para porta aleatória disponível)")
	flagNoWindow := flag.Bool("no-window", false, "Inicia apenas o servidor de backend sem abrir janela gráfica")
	flagMCP := flag.Bool("mcp", false, "Executa o servidor Model Context Protocol (MCP) via Stdio para conexão com Claude Desktop/Antigravity/Cursor")
	flagElevatedChild := flag.Bool("elevated-child", false, "Flag interna de instância filha elevada")
	flagIPCAddr := flag.String("ipc-addr", "", "Endereço IPC interno para redirecionar saída ao console pai")
	flagParentPID := flag.Int("parent-pid", 0, "PID do processo pai para encerramento automático sem processos zumbis")
	flag.Parse()

	if *flagVersion {
		fmt.Printf("ScanFile Pro v%s (commit: %s, built: %s)\n", Version, Commit, Date)
		return
	}

	// MCP Server Mode over Stdio
	if *flagMCP {
		mcpCtx := mcp.NewMCPToolsContext(nil, nil, nil)
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

	// Create and start backend application server
	appServer := server.NewAppServer(uiSubFS)
	serverURL, err := appServer.Start(*flagPort)
	if err != nil {
		log.Fatalf("Erro ao iniciar servidor de aplicação: %v", err)
	}

	log.Printf("[+] Servidor de backend ativo em: %s\n", serverURL)

	// Launch Native Windows Desktop Window if not disabled
	if !*flagNoWindow {
		go func() {
			time.Sleep(300 * time.Millisecond)
			launchNativeWindow(serverURL)
		}()
	}

	// Graceful shutdown handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	log.Println("\n[*] Encerrando ScanFile Pro...")
	appServer.Stop()
	log.Println("[*] Finalizado.")
}

// launchNativeWindow opens the application in a native Windows Edge App window or native browser.
func launchNativeWindow(targetURL string) {
	if runtime.GOOS != "windows" {
		_ = exec.Command("xdg-open", targetURL).Start()
		return
	}

	// Edge executable paths on Windows
	possibleEdgePaths := []string{
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
		filepath.Join(os.Getenv("LOCALAPPDATA"), `Microsoft\Edge\Application\msedge.exe`),
	}

	var edgeExe string
	for _, p := range possibleEdgePaths {
		if _, err := os.Stat(p); err == nil {
			edgeExe = p
			break
		}
	}

	if edgeExe != "" {
		tempProfile := filepath.Join(os.TempDir(), "ScanFile_Webview_Profile")
		cmd := exec.Command(edgeExe,
			fmt.Sprintf("--app=%s", targetURL),
			fmt.Sprintf("--user-data-dir=%s", tempProfile),
			"--window-size=1360,860",
			"--disable-features=TranslateUI",
			"--disable-extensions",
		)
		if err := cmd.Start(); err == nil {
			log.Printf("[+] Janela nativa do Windows iniciada com sucesso via Edge App Mode (%s)\n", edgeExe)
			return
		}
	}

	// Fallback to rundll32 / default Windows browser
	log.Println("[*] Abrindo navegador padrão do sistema...")
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", targetURL).Start()
}

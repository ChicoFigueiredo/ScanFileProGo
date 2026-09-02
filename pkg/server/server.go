package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"scanfile/pkg/ai"
	"scanfile/pkg/config"
	"scanfile/pkg/hasher"
	"scanfile/pkg/indexer"
	"scanfile/pkg/mcp"
	"scanfile/pkg/scanner"
	"scanfile/pkg/watcher"
)

// AppServer orchestrates the REST & SSE API for the ScanFile application.
type AppServer struct {
	Tree        *scanner.TreeManager
	Scanner     *scanner.Scanner
	Hasher      *HasherManager
	Index       *indexer.DuplicateIndex
	FolderIndex *indexer.FolderDuplicateIndex
	Watcher     *watcher.FSWatcher
	MCPContext  *mcp.MCPToolsContext
	AIAgent     *ai.AgentCoordinator
	uiFS        fs.FS
	httpServer  *http.Server
	listener    net.Listener
	activeRoots []string
	lastConfig  scanner.ScanConfig

	eventsMu     sync.RWMutex
	sseClients   map[chan string]bool
	recentLogs   []scanner.FSEventLog
	recentLogsMu sync.RWMutex

	// Sessão e ciclo de vida (etapa 2, contrato 1.1 e 1.9)
	sessionToken     string
	startedAt        time.Time
	noWindow         bool
	shutdownOnce     sync.Once
	shutdownCh       chan struct{}
	presenceMu       sync.Mutex
	sseCount         int
	lastClientSeen   time.Time
	shutdownWhenDone bool

	// Varredura em curso (etapa 2, contrato 1.2 e 1.7)
	scanMu             sync.Mutex
	scanCancel         context.CancelFunc
	scanDone           chan struct{}
	autosaveMu         sync.Mutex
	lastAutoSaveChange uint64
}

// HasherManager wraps hasher execution and state.
type HasherManager struct {
	Engine *hasher.Hasher
	Status scanner.ScanStatus
	mu     sync.RWMutex
}

type serverToolsExecutor struct {
	mcpCtx *mcp.MCPToolsContext
}

func (ste *serverToolsExecutor) ExecuteTool(ctx context.Context, name string, argsJSON string) (string, *ai.ActionProposal, error) {
	switch name {
	case "classify_files":
		var params mcp.ClassifyFilesParams
		_ = json.Unmarshal([]byte(argsJSON), &params)
		items, err := ste.mcpCtx.ClassifyFiles(ctx, params)
		if err != nil {
			return "", nil, err
		}
		data, _ := json.MarshalIndent(items, "", "  ")
		return string(data), nil, nil

	case "analyze_file_content":
		var params mcp.AnalyzeFileParams
		_ = json.Unmarshal([]byte(argsJSON), &params)
		res, err := ste.mcpCtx.AnalyzeFileContent(ctx, params)
		if err != nil {
			return "", nil, err
		}
		data, _ := json.MarshalIndent(res, "", "  ")
		return string(data), nil, nil

	case "analyze_image_visual":
		var params mcp.AnalyzeImageParams
		_ = json.Unmarshal([]byte(argsJSON), &params)
		res, err := ste.mcpCtx.AnalyzeImageVisual(ctx, params)
		if err != nil {
			return "", nil, err
		}
		data, _ := json.MarshalIndent(res, "", "  ")
		return string(data), nil, nil

	case "compare_visual_similarity":
		var params mcp.CompareVisualParams
		_ = json.Unmarshal([]byte(argsJSON), &params)
		res, err := ste.mcpCtx.CompareVisualSimilarity(ctx, params)
		if err != nil {
			return "", nil, err
		}
		data, _ := json.MarshalIndent(res, "", "  ")
		return string(data), nil, nil

	case "write_file_metadata":
		var params mcp.WriteMetadataParams
		_ = json.Unmarshal([]byte(argsJSON), &params)
		meta, err := ste.mcpCtx.WriteFileMetadata(params)
		if err != nil {
			return "", nil, err
		}
		data, _ := json.MarshalIndent(meta, "", "  ")
		return string(data), nil, nil

	case "propose_actions":
		var params mcp.ProposeActionParams
		_ = json.Unmarshal([]byte(argsJSON), &params)
		prop, err := ste.mcpCtx.ProposeActions(params)
		if err != nil {
			return "", nil, err
		}
		var aiProp *ai.ActionProposal
		if prop != nil {
			aiProp = &ai.ActionProposal{
				ProposalID:  prop.ID,
				ActionType:  prop.ActionType,
				Description: prop.Description,
				Files:       prop.Files,
				FileCount:   prop.FileCount,
				TotalBytes:  prop.TotalBytes,
				TotalSize:   prop.TotalSize,
				Category:    prop.Category,
				DryRun:      prop.DryRun,
				Executed:    prop.Executed,
				CreatedAt:   prop.CreatedAt.Format(time.RFC3339),
			}
		}
		data, _ := json.MarshalIndent(prop, "", "  ")
		return string(data), aiProp, nil

	default:
		return fmt.Sprintf("Ferramenta desconhecida: %s", name), nil, nil
	}
}

// NewAppServer creates and initializes an AppServer.
func NewAppServer(uiFS fs.FS) *AppServer {
	tree := scanner.NewTreeManager()
	idx := indexer.NewDuplicateIndex()
	fIdx := indexer.NewFolderDuplicateIndex()
	sc := scanner.NewScanner(tree)
	hEngine := hasher.NewHasher()

	cfg := config.LoadConfig()
	ollamaClient := ai.NewOllamaClient(cfg.AIOllamaEndpoint)
	mcpCtx := mcp.NewMCPToolsContext(tree, idx, fIdx, ollamaClient, cfg.AIOllamaModel)

	toolsDefs := mcp.GetOpenAIToolDefinitions()
	agent := ai.NewAgentCoordinator(
		cfg.AIOllamaEndpoint,
		cfg.AIOpenRouterKey,
		"",
		&serverToolsExecutor{mcpCtx: mcpCtx},
		toolsDefs,
	)

	return &AppServer{
		Tree:        tree,
		Scanner:     sc,
		Hasher:      &HasherManager{Engine: hEngine},
		Index:       idx,
		FolderIndex: fIdx,
		MCPContext:  mcpCtx,
		AIAgent:     agent,
		uiFS:        uiFS,
		sseClients:  make(map[chan string]bool),
		recentLogs:  make([]scanner.FSEventLog, 0, 100),
	}
}

// DebugMode controls verbose HTTP request and internal state logging.
var DebugMode bool

type responseRecorder struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
}

func (rec *responseRecorder) WriteHeader(code int) {
	rec.statusCode = code
	rec.ResponseWriter.WriteHeader(code)
}

func (rec *responseRecorder) Write(b []byte) (int, error) {
	n, err := rec.ResponseWriter.Write(b)
	rec.bytesWritten += int64(n)
	return n, err
}

func (rec *responseRecorder) Flush() {
	if f, ok := rec.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *AppServer) debugMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}

		defer func() {
			if recErr := recover(); recErr != nil {
				log.Printf("[PANIC RECOVER] %s %s: %v\nStack:\n%s\n", r.Method, r.URL.Path, recErr, string(debug.Stack()))
				http.Error(w, fmt.Sprintf("Internal Server Error: %v", recErr), http.StatusInternalServerError)
			}
			if DebugMode && !strings.HasPrefix(r.URL.Path, "/api/events") {
				duration := time.Since(start)
				log.Printf("[HTTP DEBUG] %s %s | Status: %d | Size: %d B | Time: %v | Remote: %s\n",
					r.Method, r.URL.RequestURI(), rec.statusCode, rec.bytesWritten, duration, r.RemoteAddr)
			}
		}()

		next.ServeHTTP(rec, r)
	})
}

// Type alias for status inside server
type ScanStatus = scanner.ScanStatus

// GetLiveMemoryStats gathers process memory and host OS RAM utilization.
func GetLiveMemoryStats() *scanner.MemoryStatsPayload {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	payload := &scanner.MemoryStatsPayload{
		AllocMB:      m.Alloc / (1024 * 1024),
		TotalAllocMB: m.TotalAlloc / (1024 * 1024),
		SysMB:        m.Sys / (1024 * 1024),
		NumGC:        m.NumGC,
		Goroutines:   runtime.NumGoroutine(),
	}

	getSystemPhysicalMemory(payload, m.Alloc)
	return payload
}

func getSystemPhysicalMemory(payload *scanner.MemoryStatsPayload, allocBytes uint64) {
	if runtime.GOOS == "windows" {
		type memoryStatusEx struct {
			cbSize                  uint32
			dwMemoryLoad            uint32
			ullTotalPhys            uint64
			ullAvailPhys            uint64
			ullTotalPageFile        uint64
			ullAvailPageFile        uint64
			ullTotalVirtual         uint64
			ullAvailVirtual         uint64
			ullAvailExtendedVirtual uint64
		}
		var stat memoryStatusEx
		stat.cbSize = uint32(unsafe.Sizeof(stat))
		kernel32 := syscall.NewLazyDLL("kernel32.dll")
		globalMemoryStatusEx := kernel32.NewProc("GlobalMemoryStatusEx")
		ret, _, _ := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&stat)))
		if ret != 0 && stat.ullTotalPhys > 0 {
			payload.SystemTotalRAMMB = stat.ullTotalPhys / (1024 * 1024)
			payload.SystemFreeRAMMB = stat.ullAvailPhys / (1024 * 1024)
			if stat.ullTotalPhys >= stat.ullAvailPhys {
				payload.SystemUsedRAMMB = (stat.ullTotalPhys - stat.ullAvailPhys) / (1024 * 1024)
			}
			payload.SystemPercent = float64(stat.dwMemoryLoad)
			if stat.ullTotalPhys > 0 {
				payload.AppPercentOfSys = (float64(allocBytes) / float64(stat.ullTotalPhys)) * 100.0
			}
		}
	}
}

func (s *AppServer) findFileInTree(filePath string) *scanner.FileNode {
	dir := filepath.Dir(filePath)
	node := s.Tree.FindDir(dir)
	if node == nil {
		return nil
	}
	for _, f := range node.Files {
		if f.Path == filePath {
			return f
		}
	}
	return nil
}

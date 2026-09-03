# Notas de integração para a etapa 2 (acumuladas dos relatórios da etapa 1)

## Agente D (pkg/recycle, pkg/privileges, pkg/config, pkg/drives) — commit 5ed87f0

APIs:
- recycle: `IsProtectedPath(path) (bool, string)`, `IsWithinRoots(path, roots) bool`, `Preflight(path) (ok, reason)`, `BatchDeleteItems(paths, toRecycleBin) BatchDeleteResult` (Items []ItemResult{Path,Status,Reason}; StatusRecycled/Deleted/Refused/Failed; RefusedCount), `ItemSize(path)`.
- privileges: `LaunchElevatedHandoff(args) (pid uint32, err)`, `WaitForProcessExit(pid, timeout) error`.
- config: `DefaultServerPort=47321`, `ProviderOllama/OpenRouter/Quick`, `ConfigPath()`, `SetConfigPath()`, `MergeJSON(cur, body)`, `SetOpenRouterKey(*cfg, plain)`, `OpenRouterKey(cfg) string`, `(AppConfig).Public()`; campos `ServerPort`, `AIOpenRouterKeyEnc`, `AIOpenRouterKeyPlain`, `HasOpenRouterKey`. `LoadConfig()` NÃO preenche mais `AIOpenRouterKey`.
- drives: `DriveInfo.IsWSL`, `DriveInfo.DefaultSelected`, `IsWSLVolume`, `IsDefaultSelected`.

Integração (S1):
1. Handlers recycle/delete: validar `IsWithinRoots(path, activeRoots)` no servidor; depois `BatchDeleteItems`. Resposta 1.5: `items` pronto; recycled=SuccessCount, refused=RefusedCount, failed=FailedCount. Confirmações (`confirmName`, `confirmText`) no servidor. Proteção e preflight já rodam dentro da lib. Exclusão permanente devolve status `deleted`.
2. Config: POST → `MergeJSON(LoadConfig(), body)` + `SaveConfig`; GET → `cfg.Public()`. `aiOpenRouterKey` sai com omitempty (não "" sempre) — UI usa `hasOpenRouterKey`.
3. OpenRouter: trocar `ai.NewOpenRouterClient(cfg.AIOpenRouterKey)` por `config.OpenRouterKey(cfg)`; `hasOpenRouterKey` em handleAIStatus = `cfg.Public().HasOpenRouterKey`.
4. Handoff (S3): `LaunchElevatedHandoff(append(args, "--handoff"))` não bloqueia; pai fecha listener quando filho responder; `WaitForProcessExit` para acompanhar. Filho continua chamando `MonitorParentProcess`.
5. Porta (S3): `cfg.ServerPort` padrão 47321; `--port` sobrepõe.
6. `WorkerThreads` padrão agora 0 (Auto).
7. Defeito pré-existente: `GOOS=linux go build` falha em `pkg/server/server.go` (`syscall.NewLazyDLL` em getSystemPhysicalMemory) — S2 deve mover para `memory_windows.go` + stub `memory_other.go`.
8. Teste `TestSendToRecycleBinRoundTrip` só com `SCANFILE_TEST_RECYCLE=1`.

## Agente B (pkg/watcher, pkg/indexer, pkg/scanner/tree_watch.go) — commit f651129

APIs:
- watcher: `New(opts Options) (*FSWatcher, error)`; `Options{Tree, Index, FolderIndex, Debounce, HashWorkers, HashFunc func(path)(hash string, err), Ignore, OnEvent func(FSEventLog), OnOverflow func(root string), BufferSize}`; `Start(ctx, roots []string, onEvent ...func(FSEventLog))`; `Stop()`; `ChangeCount() uint64`; `IsRunning()`; `Roots()`; `DefaultIgnore(path) bool`; `ErrAlreadyRunning`, `ErrNoTree`; `DefaultDebounce=2s`, `DefaultHashWorkers=2`. `NewFSWatcher(tree,index,algo)` Deprecated (adaptador).
- scanner/tree_watch.go: `ReplaceFile(f) (previousSize int64, replaced bool)`, `RemoveDir(dirPath) (removedBytes, removedFiles int64, ok bool)`, `FindFile(filePath) *FileNode`.
- indexer: `DuplicateIndex.UpsertFile(f)`, `RemoveDirFromIndex(dirPath) int`; `FolderDuplicateIndex.MarkDirty()`, `IsDirty()`, `RebuildIfDirty(tm) bool`; `FolderSummaryOf(dir)`; `ConfidenceHash`/`ConfidenceSizeMTime`. `Query` devolve CÓPIAS dos grupos. JSON aditivo: `allFilesHashed`, `confidence`.

Integração (S2):
```go
fw, err := watcher.New(watcher.Options{
    Tree: s.Tree, Index: s.Index, FolderIndex: s.FolderIndex,
    HashFunc: func(p string) (string, error) { h, _, err := hasher.ComputeSingleFileHash(p, config.HashAlgorithm); return h, err },
    OnEvent:    func(ev scanner.FSEventLog) { s.appendRecentLog(ev); s.broadcastSSE("fs_event", ev) },
    OnOverflow: func(root string) { s.rescanRoot(root) },
})
if err == nil { s.Watcher = fw; err = fw.Start(ctx, config.Roots) }
```
- Nas consultas de pastas trocar `RebuildFolderIndex` por `RebuildIfDirty` (fecha M3).
- `ReplaceFile`/`RemoveDir` ainda não chamam o `ChangeCounter()` do Agente A: S2 (ou o integrador do merge) deve ligar quando A chegar.
- Pendência confirmada: `pkg/server/server.go` `syscall.NewLazyDLL` quebra `GOOS=linux` (S2 move para `memory_windows.go`).

## Agente C (pkg/mcp, pkg/ai) — commit 46f4587

APIs:
- mcp: `SetAllowedRoots([]string)`, `AllowedRoots()`, `GetProposal(id) (*ActionProposal, bool)`, `NewMCPToolsContextFromAutosave(dir, ollama, model) (*MCPToolsContext, *scanner.CacheSnapshot, error)`, `NewStdioServer(tc)`, `ErrorIsNoRoots(err)`, campo `MCPToolsContext.RecycleFunc func([]string) recycle.BatchDeleteResult`, `ErrNoAllowedRoots`, `ProposalTTL=30min`. `ProposeActions` nunca executa; `dryRun` sempre true; `DELETE` não é ação aceita.
- ai: `ProviderQuick="quick"` (alias `direct`), `NormalizeProvider`, `ProviderDisplayName`, `SupportedProviders`, `NewQuickRouter().Chat(...)`, `BuildCatalogWithMemory(ctx, endpoint, totalMem)`, `OllamaClient.ListModels`, `FindCuratedModel`, `DescribeModel`, `CatalogMaxSizeGB=14`, `DefaultOllamaModel="qwen3-vl:8b"`. `CatalogResponse{models, installedModels, ollamaOnline, ollamaVersion, totalMemoryGB, maxSizeGB, defaultModel}`. `ai.ActionExecuteRequest.Confirm bool`.

Integração (S1/S2/S3):
1. S2: `s.MCPContext.SetAllowedRoots(roots)` a cada StartScan e a cada load/restore (fail-closed sem isso).
2. S1: `RecycleFunc` → usar `recycle.BatchDeleteItems` com escopo (mesma regra do handler).
3. S3 (main.go): `--mcp` → `mcp.NewMCPToolsContextFromAutosave(dir, ollamaClient, cfg.AIOllamaModel)`; tratar "nenhum Autosave encontrado"; usar `mcp.NewStdioServer`/StartStdioServer conforme exposto.
4. S1: `handleAIModels` deve devolver `catalog.Models` (array) conforme 1.11.
5. S1: `/api/ai/actions/execute` exige `confirm:true` → 400 antes de ExecuteProposal.
6. `qwen2.5vl:7b` e `gemma3:12b`: visão sem ferramentas (dados honestos).
7. `NewMCPToolsContextFromAutosave` devolve *CacheSnapshot; trocar por CacheSnapshotSummary quando A chegar (opcional).

## Agente E (ui/**) — commit 520509d

- Comando de teste correto: `node --test "ui/tests/*.test.mjs"` (94 testes). `node --test ui/tests` NÃO funciona no Node 24 Windows.
- UI chama: GET /api/config, /api/system/info (numCPU, threadOptions, maxThreads), /api/system/privileges, /api/system/memory, /api/drives (isWSL, defaultSelected), /api/scan/status (load + onopen), /api/logs, /api/logs/skipped?limit=200, /api/tree?path&depth, /api/tree/files?path&offset&limit=100&sortBy (404 → fallback), /api/duplicates, /api/folders/duplicates, /api/folders/compare, /api/stats/extensions, /api/stats/idle-files, /api/cache/list, /api/cache/autosave/status, /api/ai/models (aceita array 1.11 ou {localModels}).
- POST: /api/config (parcial), /api/scan/start (409 tratado), /api/scan/cancel, /api/files/recycle {paths, confirmName}, /api/files/delete {paths, confirmText:"EXCLUIR"}, /api/cache/save, /api/cache/load, /api/cache/autosave/restore (lê `summary`; `snapshot` legado), /api/system/elevate, /api/ai/models/pull, /api/ai/chat, /api/ai/actions/execute {proposalId, confirm:true}.
- EventSource('/api/events?token=') eventos: scan_progress, fs_event, autosave_done, shutdown. sendBeacon('/api/ui/closed?token=') no pagehide.
- Respostas recycle/delete lidas como {items:[{path,status,reason}], freedBytes}; status recycled|deleted|refused|failed.
- HUD exibe ScanStatus.skippedCount, prehashCount, phase1Workers, phase2Workers.
- Token: se o placeholder `{{SCANFILE_TOKEN}}` não for substituído, a UI não envia cabeçalho (compat).
- Combo de algoritmos inclui blake3 e md5.
- Fontes: 1 woff2 variável por família (inter-latin, jetbrains-mono-latin).

## Agente A (pkg/scanner, pkg/hasher) — commit f644832

APIs: `HashXXHash/HashBlake3/HashMD5/HashSHA256`, `SupportedHashAlgorithms()`, `NormalizeHashAlgorithm`, `HashPrefix`, `HashAlgorithmOf`, `HashMatchesAlgorithm`; `PhaseMetadata/PhaseHashing`, `MaxThreads()`, `ThreadOptions()`, `ResolveWorkers(requested, phase)`; `ErrScanInProgress`, `Scanner.IsRunning/SkippedCount/GetSkipped/GetErrorLogs/LoggerPath/CloseLogger`, `SkippedEntry{Timestamp,Path,Reason}`, `MaxScanDepth`; `TreeManager.ChangeCounter()`, `GetDirSummary(path, maxDepth, maxFiles ...int)`, `GetFilesPage(path, offset, limit, sortBy) (int, []*FileNode)`, `DefaultSummaryMaxFiles=500`, `MaxFilesPageLimit=500`, `SortSizeDesc/SortNameAsc/SortModDesc`; `CacheSnapshotSummary`, `ImportCacheStream`, `LoadCacheSummaryFromFile`, `BuildQuickScanLookupFromTree(tm)`, `LoadQuickScanLookupFromFile(path)`; hasher: `ErrHashingInProgress`, `ComputeQuickHash`, `NewDigest`, `FormatDigest`, `HashBytes`, `HashProgress`, `ComputeHashOptions.{DisablePrehash,OnDetailedProgress}`.
Campos: `ScanStatus.{SkippedCount,PrehashCount,Phase1Workers,Phase2Workers}`, `ScanConfig.LogDir`, `DirSummary.DirectFileCount`.

Integração (S2 salvo indicação):
1. Quick Scan: `BuildQuickScanLookupFromTree(s.Tree)`; do autosave, `LoadQuickScanLookupFromFile(path)`. (Import é streaming: `CacheSnapshot.Files` volta VAZIO — sem a troca, Quick Scan de autosave deixa de reaproveitar.)
2. Restore/load: `LoadCacheSummaryFromFile` e devolver `summary` (fecha H3).
3. `OnDetailedProgress` → preencher `ScanStatus.PrehashCount`. Phase1/2Workers já vêm do StartScan.
4. `StartScan` devolve `ErrScanInProgress`/`context.Canceled`; `RunHashing` devolve `ErrHashingInProgress`/`ctx.Err()`. Parar de ignorar com `_ =`.
5. `/api/system/info`: `ThreadOptions()`, `MaxThreads()`.
6. `/api/logs/skipped`: `GetSkipped()`; `/api/tree/files`: `GetFilesPage`.
7. Autosave "só se mudou": `ChangeCounter()`.
8. `Stop()` deve chamar `Scanner.CloseLogger()`.
9. `ScanConfig.LogDir` padrão "logs".
10. `DirSummary.DirectFileCount` = arquivos diretos reais (fileCount continua recursivo).
11. Corrida `FileNode.Hash` (H7) fica para a etapa 3 (ADR-0001).

## Agente S1 (auth.go, static.go, handlers_files.go, handlers_ai.go) — commit 8d1b896

Entregue: token de sessão com `X-ScanFile-Token` (query só em `/api/events` e `/api/ui/closed`), `Origin` estranho → 403, zero CORS, `/api/instance` isento; injeção do token no `index.html`; recycle/delete com escopo, `confirmName`, `confirmText`, resposta `items[]`; config via `MergeJSON`/`Public()`; `/api/system/info`; `/api/ai/models` como array; `execute` exige `confirm:true`.
Novos identificadores: `server.Version` (var), `server.SessionTokenHeader`, `(*AppServer).SetSessionToken(string)`, `(*AppServer).token()`.

### Pendências abertas (passagem final de integração)
1. `activeRoots` sem mutex: escrito por `handlers_scan.go`/`handlers_cache.go` e lido pelos handlers de arquivos. S1 fez cópia defensiva (`scanRoots()`), mas o `RWMutex` em `server.go` é de S2.
2. `NewAppServer` ainda passa `cfg.AIOpenRouterKey` (sempre vazio) ao `NewAgentCoordinator`; trocar por `config.OpenRouterKey(cfg)`.
3. `findFileInTree` em `server.go` ficou sem uso.
4. `main.go` (S3) deve fazer `server.Version = Version`; filho do handoff usa `SetSessionToken`; o modo `--mcp` não constrói `Handler()`, então precisa injetar a própria `RecycleFunc` com escopo, senão o Assistente cai no padrão sem escopo.
5. Token guardado por mutex de pacote em `auth.go` (não `sync.Once`), porque `server.go` é de S2.

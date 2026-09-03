# Contrato de implementação — etapa (b)

Este documento é a fonte única de verdade para os agentes que implementam as decisões de [`DECISOES.md`](DECISOES.md). Vocabulário em [`../CONTEXT.md`](../CONTEXT.md). Achados referidos (C1, H2, M5...) estão em [`AVALIACAO.md`](AVALIACAO.md).

## 0. Regras para todos os agentes

1. **Propriedade de arquivos é exclusiva.** Cada agente edita apenas os arquivos e pacotes listados na sua seção. Se precisar de algo de outro pacote que ainda não existe, descreva a necessidade no relatório final; não crie a função no pacote alheio.
2. **Não commitar, não fazer push, não rodar `go mod tidy`** (só o agente A altera `go.mod`/`go.sum`).
3. **Identificadores em inglês, mensagens ao usuário em pt-BR**, como o código atual.
4. **Testes são o produto.** Todo comportamento novo tem teste; todo bug corrigido tem um teste que falhava antes. `go vet ./...`, `go test ./...` e `go test -race ./pkg/<seu pacote>/...` precisam passar no seu worktree antes de terminar. Testes de UI rodam com `node --test ui/tests`.
5. **Windows é a plataforma alvo.** Arquivos `_other.go` continuam compilando (`GOOS=linux go build ./...` deve passar), com stubs honestos.
6. **Não altere o formato JSON que a UI já consome** a não ser onde este contrato manda. Campos novos são aditivos.
7. Relatório final obrigatório: nome do branch do worktree, arquivos alterados, APIs públicas novas com assinatura, o que o integrador precisa ligar em `pkg/server`/`main.go`, resumo dos testes e qualquer decisão que você teve de tomar sozinho.

## 1. Contratos compartilhados

### 1.1 Sessão e token (Q8)

- O servidor gera um token aleatório de 32 bytes por execução. `index.html` contém literalmente `<meta name="scanfile-token" content="{{SCANFILE_TOKEN}}">`; o servidor substitui o placeholder ao servir `/`.
- Toda chamada a `/api/*` exige o token: cabeçalho `X-ScanFile-Token` ou, só para `EventSource` e `sendBeacon`, query `?token=`. Exceções sem token: `GET /api/instance` (descoberta de instância, responde só `{app, version, pid}`).
- Sem token ou token errado → `401 {"error":"unauthorized"}`. Requisições com cabeçalho `Origin` diferente da própria origem → `403`. Nenhum cabeçalho CORS é emitido.

### 1.2 Fases e status da Varredura (Q6)

`ScanStatus.phase` assume: `idle`, `phase1_metadata`, `phase2_hashing`, `indexing`, `completed`, `cancelling`, `cancelled`, `loading_cache`, `watching`.
- `POST /api/scan/start` durante `phase1_metadata`, `phase2_hashing`, `indexing`, `cancelling` ou `loading_cache` → `409 {"error":"scan_in_progress","phase":"..."}`.
- `POST /api/scan/cancel` → `{"status":"cancelling"}`; o pipeline inteiro para (Fase 1, Fase 2, autosave, índices) e o status final é `cancelled`. Nenhum autosave é gravado ao cancelar.
- Campos novos em `ScanStatus`: `skippedCount` (int64), `prehashCount` (int64, candidatos que passaram pelo Pré-hash), `phase1Workers`, `phase2Workers` (int, threads efetivas).

### 1.3 Threads (Q20, Q36)

- `GET /api/system/info` → `{"numCPU":16,"threadOptions":[0,4,8,16,32,64],"maxThreads":64,"version":"...","port":47321,"elevated":false}`.
- `threadOptions`: `0` significa Auto; demais são potências de 2 de 4 até `4 × NumCPU` inclusive. O servidor sempre aplica `min(pedido, maxThreads)`.
- Auto: Fase 1 usa `2 × NumCPU`; Fase 2 usa `NumCPU`.

### 1.4 Árvore (Q25)

- `GET /api/tree?path=&depth=` → como hoje, mas `files` contém no máximo os 500 maiores arquivos diretos da pasta e o objeto ganha `fileCount` (total real). Subpastas: até 500 no nível 1 e 50 nos demais (mantido).
- `GET /api/tree/files?path=&offset=0&limit=100&sortBy=size_desc|name_asc|mod_desc` → `{"total":N,"offset":0,"limit":100,"files":[FileNode...]}`. `limit` máximo 500.

### 1.5 Reciclagem e Exclusão (Q22, Q23, Q32)

- `POST /api/files/recycle` body `{"paths":[...],"confirmName":""}` → `{"items":[{"path":"...","status":"recycled|refused|failed","reason":"..."}],"recycled":n,"refused":n,"failed":n,"freedBytes":n}`.
  - Servidor recusa (`refused`) caminho fora das Raízes Varridas, Pasta Protegida, ou volume sem Lixeira disponível (preflight), e pasta cujo nome base não seja igual a `confirmName`.
- `POST /api/files/delete` body `{"paths":[...],"confirmText":"EXCLUIR"}` → mesmo formato; `confirmText` diferente de `EXCLUIR` → `400`. Mesmas recusas de escopo e proteção.
- Pasta Protegida: raiz de qualquer volume; `<raiz>\Windows` e tudo abaixo; qualquer `System Volume Information` e tudo abaixo. Vale mesmo elevado.

### 1.6 Configuração (Q19, C3, M13)

- `GET /api/config` → `AppConfig` com `aiOpenRouterKey` sempre `""` e o campo aditivo `hasOpenRouterKey: bool`.
- `POST /api/config` aceita **JSON parcial**: só as chaves presentes são aplicadas sobre a config atual; resposta `{"status":"saved"}`. `aiOpenRouterKey` presente e não vazia → cifrada com DPAPI e guardada em `aiOpenRouterKeyEnc`; presente e vazia → remove a chave. A UI nunca recebe a chave de volta.
- Campos novos: `serverPort` (int, padrão 47321), `aiProvider` aceita `"ollama" | "openrouter" | "quick"` (`"direct"` é alias legado de `"quick"`).
- Gravação atômica (arquivo temporário + rename) no mesmo caminho de onde foi lida; padrão é ao lado do executável.

### 1.7 Snapshots e Autosave (Q7, Q21, H3)

- `POST /api/cache/load`, `POST /api/cache/autosave/restore` → `{"status":"loaded|restored","summary":{"roots":[],"totalFiles":n,"totalDirs":n,"totalBytes":n,"timestamp":"...","hashAlgorithm":"..."}}`. Nunca devolvem a lista de arquivos.
- Formato em disco continua JSON + gzip, com as mesmas chaves de hoje; escrita e leitura são em streaming.
- Autosave: durante a Varredura a cada `autoSaveIntervalMinutes` (padrão 5), ao concluir a Fase 1 e ao concluir a Fase 2; com Monitoramento ativo, a cada 10 min **somente se** o contador de mudanças da árvore avançou desde o último autosave. Um único ticker por processo, nunca um por Varredura.

### 1.8 Eventos SSE

- `event: scan_progress` (ScanStatus), `event: fs_event` (FSEventLog), `event: autosave_done`, `event: shutdown` (`{"reason":"...","inSeconds":n}`).
- A UI, ao carregar e em todo `onopen` do `EventSource`, chama `GET /api/scan/status` para ressincronizar.

### 1.9 Ciclo de vida (Q3, Q24, Q33, Q13)

- Presença = conexões SSE ativas + `POST /api/ui/closed` via `sendBeacon` no `pagehide`. Zero clientes por 10 s → se há Varredura em curso, marca "encerrar ao concluir"; senão encerra com `Stop()`. `--no-window` desliga esse comportamento.
- Porta padrão 47321 (`serverPort`), `--port` sobrepõe. Instância em execução grava `%LOCALAPPDATA%\ScanFile\instance.json` `{"port":n,"pid":n,"token":"..."}`; uma nova execução lê o arquivo, chama `GET /api/instance` e, se responder, chama `POST /api/instance/focus` com o token e encerra.
- Elevar pela UI: `POST /api/system/elevate` lança o filho elevado com `--handoff`; o filho adota porta e token de `instance.json`; o pai fecha o listener assim que o filho responder em `/api/instance` e encerra.

### 1.10 Monitor e itens pulados (Q5, Q18, Q35)

- `GET /api/logs` → últimos 200 `FSEventLog` (já existe). `GET /api/logs/skipped?limit=200` → `[{"timestamp","path","reason"}]`.
- Monitoramento: coalescência de 2 s por caminho; 2 workers de hash em segundo plano; índices incrementais; estouro do buffer → revarredura da raiz afetada.

### 1.11 Assistente (Q10, Q27, Q28, Q29, Q38)

- `propose_actions` sempre cria Proposta pendente; nada executa no turno do modelo. `POST /api/ai/actions/execute` body `{"proposalId":"...","confirm":true}`; sem `confirm:true` → `400`.
- Ferramentas que leem arquivos só aceitam caminhos dentro das Raízes Varridas; sem Raízes carregadas, recusam com mensagem clara.
- `GET /api/ai/models` → `[{"id":"qwen3-vl:8b","name":"Qwen3-VL 8B","provider":"ollama","sizeGB":6.0,"vision":true,"tools":true,"installed":false,"recommended":true,"fitsMemory":true}]`. Catálogo limitado a ~14 GB: `qwen3-vl:8b` (padrão), `qwen2.5vl:7b`, `gemma3:12b`, `qwen3:14b`, `gpt-oss:20b`, `devstral:24b`.
- Provedor `quick` aparece na UI como "Comandos Rápidos (sem modelo)".

### 1.12 Discos (Q39)

`GET /api/drives` itens ganham `isWSL: bool` (sistema de arquivos `9P` ou caminho `\\wsl`) e `defaultSelected: bool` (falso para WSL, rede e CD-ROM).

### 1.13 Hashing (Q4, Q37)

- Algoritmos: `xxhash` (padrão), `blake3`, `md5`, `sha256`. Prefixos de string: `xxh64:`, `blake3:`, `md5:`, `sha256:`.
- Pré-hash: xxHash64 dos primeiros 4096 bytes + últimos 4096 bytes, guardado em `FileNode.quickHash`. Só candidatos com o mesmo `(size, quickHash)` em grupo ≥ 2 vão ao Hash Completo. Arquivos ≤ 8192 bytes vão direto ao Hash Completo.
- Quick Scan reaproveita hash só se o prefixo do hash gravado for o do algoritmo atual.

## 2. Agente A — motor: `pkg/scanner`, `pkg/hasher`, `go.mod`, `go.sum`

Não crie `pkg/scanner/tree_watch.go` (pertence ao agente B) e não adicione `RemoveDir` em `tree.go`.

1. **Pré-hash e algoritmos** conforme 1.13. BLAKE3 via `lukechampine.com/blake3` (Go puro). Pool de buffers de 1 MiB (`sync.Pool`). `Hasher.Cancel()` protegido por mutex; `RunHashing` devolve `ctx.Err()` ao ser cancelado. Progresso expõe contagem de pré-hash e bytes lidos.
2. **Threads**: `scanner.ResolveWorkers(requested, phase int) int` e `scanner.ThreadOptions() []int` conforme 1.3. Clamp em `StartScan` e em `RunHashing`.
3. **Cancelamento**: `Scanner.Cancel()` seguro para concorrência; `StartScan` devolve `context.Canceled`; `Scanner.IsRunning()`.
4. **Itens Pulados**: toda decisão de pular vira `logSkipped(path, reason)`: contador `SkippedCount`, anel de 500 entradas `GetSkipped()`, linha no `DiskErrorLogger` com fase `SKIPPED`. Heurística WSL só quando a raiz do volume tem sistema de arquivos `9P` ou o caminho começa com `\\wsl` (detecte uma vez por raiz). Remova a contagem de nomes repetidos; loops são tratados por reparse point + `visitedDirs` + profundidade máxima 50 (que também registra o pulo).
5. **`EvalSymlinks` só para reparse points**; diretórios normais não entram em `visitedDirs`.
6. **Quick Scan** só reaproveita hash com prefixo do algoritmo atual (1.13).
7. **`DiskErrorLogger`**: `Log` faz flush a cada 32 linhas e um ticker de 2 s; `Close` idempotente; novo `StartScan` fecha o logger anterior.
8. **`cache.go` em streaming**: exportação escreve o JSON chave a chave e os arrays `files`/`directories` elemento a elemento; importação usa `json.Decoder.Token()` e insere na árvore conforme lê, tolerando qualquer ordem de chaves. Antes de mudar o código, gere `pkg/scanner/testdata/legacy_v2.scanfile.gz` com o exportador atual (3 pastas, 5 arquivos) e mantenha um teste que o importa. Adicione `CacheSnapshotSummary` (roots, totais, timestamp, hashAlgorithm) devolvido pelas funções de import junto com a árvore. `BuildQuickScanLookupFromTree(tm)`.
9. **`TreeManager`**: `ChangeCounter() uint64` (atômico, avança em `AddFile`, `RemoveFile`, `FastSetDir`, `EnsureDirNode`); `GetDirSummary(path, maxDepth, maxFiles int)` com os `maxFiles` maiores; `GetFilesPage(path string, offset, limit int, sortBy string) (total int, files []*FileNode)`.
10. Testes obrigatórios: vetores conhecidos dos 4 algoritmos; pré-hash evita leitura completa quando cabeçalho/rodapé diferem e detecta diferença no meio; cancelamento durante Fase 2 retorna em < 1 s; clamp de threads; tabela de heurísticas WSL/loop com casos que antes eram pulados indevidamente (`C:\dev`, `src\src\src`); Quick Scan com algoritmo diferente re-hasheia; roundtrip streaming + fixture legado; `ChangeCounter`; `GetFilesPage`; logger grava linhas.

## 3. Agente B — Monitoramento e índices: `pkg/watcher`, `pkg/indexer`, `pkg/scanner/tree_watch.go` (novo)

1. **Watcher próprio em Windows** com `ReadDirectoryChangesW` recursivo (`bWatchSubtree = true`) por raiz, I/O overlapped, buffer de 1 MiB local e 64 KiB para rede. `watcher_other.go` mantém o fsnotify atual como stub honesto.
2. API: `watcher.New(opts Options) (*FSWatcher, error)` com
   ```go
   type Options struct {
       Tree        *scanner.TreeManager
       Index       *indexer.DuplicateIndex
       FolderIndex *indexer.FolderDuplicateIndex
       Debounce    time.Duration            // padrão 2s
       HashWorkers int                      // padrão 2
       HashFunc    func(path string) (hash string, err error)
       Ignore      func(path string) bool
       OnEvent     func(scanner.FSEventLog)
       OnOverflow  func(root string)
       BufferSize  int                      // só para testes
   }
   ```
   `Start(ctx, roots)`, `Stop()`, `ChangeCount() uint64`, `IsRunning()`.
3. Comportamento: coalescer por caminho; após silêncio de `Debounce`, `Lstat`: pasta criada → `EnsureDirNode`; removida → `RemoveDir`; arquivo criado/alterado → fila limitada (2 workers) que calcula tamanho, datas, hash e chama `tree.ReplaceFile` + `index.UpsertFile`; removido/renomeado → `RemoveFile` + `RemoveFileFromIndex`. `ERROR_NOTIFY_ENUM_DIR` → `OnOverflow(root)`.
4. `pkg/scanner/tree_watch.go`: `RemoveDir(path) (removedBytes int64, removedFiles int64, ok bool)` com propagação aos ancestrais e `ReplaceFile(f *FileNode)` (substitui por caminho ou adiciona).
5. **Indexador**: `DuplicateIndex.UpsertFile(f)` incremental; `RemoveFileFromIndex` O(1) via mapa `path → chave de grupo`; `FolderDuplicateIndex.MarkDirty()` e `RebuildIfDirty(tree)`. Corrija M14: `FolderSummary.AllFilesHashed`; grupos e `CompareFolders` ganham `confidence: "hash" | "size_mtime"`; `is100PercentMatch` só com `hash`.
6. Testes obrigatórios (Windows, diretórios temporários): criar/alterar/renomear/remover em subpasta de 3 níveis reflete em árvore e índice em ≤ 5 s; 50 escritas em 1 s geram 1 hash; estouro com `BufferSize` pequeno chama `OnOverflow`; `Stop` não vaza goroutines (`runtime.NumGoroutine` antes/depois); `UpsertFile`/`RemoveFileFromIndex` mantêm `WastedBytes` corretos; confiança de pastas.

## 4. Agente C — Assistente e MCP: `pkg/mcp`, `pkg/ai`

1. `MCPToolsContext.SetAllowedRoots([]string)`; toda ferramenta que lê arquivo (`analyze_file_content`, `analyze_image_visual`, `compare_visual_similarity`, `write_file_metadata` com sidecar, `propose_actions`) recusa caminho fora das raízes ou sem raízes carregadas.
2. `ProposeActions` nunca executa; `DryRun` ignorado na entrada e sempre `true` na resposta; propostas expiram em 30 min. `ExecuteProposal`: `RECYCLE` via `RecycleFunc func([]string) recycle.BatchDeleteResult` injetável (padrão: `recycle.BatchDelete`); `MOVE` recusa destino existente (`Lstat`), coleta erros por arquivo e só marca `Executed` sem erros; `TAG` idem.
3. SQLite somente leitura: DSN `file:<caminho>?mode=ro&immutable=1`; teste prova que `INSERT` falha. Consulta do usuário: só `SELECT`, sem `;`, sem `ATTACH`/`load_extension`/`PRAGMA` (case-insensitive), timeout 5 s, 20 linhas.
4. MCP stdio: `server.WithRecovery()`; expõe também `analyze_image_visual` e `compare_visual_similarity`; `NewMCPToolsContextFromAutosave(dir string, ollama *ai.OllamaClient, model string) (*MCPToolsContext, *scanner.CacheSnapshotSummary, error)` que carrega `autosave_latest.sfz` e reconstrói índices (se o agente A ainda não tiver `CacheSnapshotSummary`, devolva `*scanner.CacheSnapshot` e anote no relatório).
5. `pkg/ai`: provedor `quick` (`ProviderQuick = "quick"`, alias `direct`), nome exibido "Comandos Rápidos (sem modelo)"; remova a listagem de `.gguf` e a pasta `models/`. Catálogo conforme 1.11 com `Vision`, `Tools`, `SizeGB`, `Installed`, `Recommended`, `FitsMemory` (≤ 14 GB). `BuildSystemPrompt` sem citar um modelo específico e dizendo que toda ação exige aprovação humana.
6. Testes: raízes permitidas (tabela, case-insensitive, prefixo falso `C:\Users2` não casa `C:\Users`); proposta nunca executa; MOVE com destino existente; SQLite ro e filtro de consultas; catálogo com Ollama falso via `httptest` (modelos instalados e tamanhos); roteador `quick`; contexto a partir de autosave gerado no teste.

## 5. Agente D — plataforma: `pkg/recycle`, `pkg/privileges`, `pkg/config`, `pkg/drives`

1. `pkg/recycle/policy.go`: `IsProtectedPath(path) (bool, reason string)` e `IsWithinRoots(path string, roots []string) bool` (case-insensitive, `filepath.Clean`, fronteira de separador).
2. `pkg/recycle`: `Preflight(path) (ok bool, reason string)`: volume fixo ou removível com `$Recycle.Bin` na raiz; tamanho ≤ `MaxCapacity` do volume (registro `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\BitBucket\Volume\<GUID>\MaxCapacity` em MB; ausente → 5% do volume). `SendToRecycleBin` adiciona `FOF_WANTNUKEWARNING` e trata `fAnyOperationsAborted`. Nova API `BatchDeleteItems(paths []string, toRecycleBin bool) BatchDeleteResult` com `Items []ItemResult{Path, Status, Reason}` além dos contadores atuais; a função antiga continua existindo.
3. `pkg/privileges`: `MonitorParentProcess` encerra imediatamente se `OpenProcess` falhar; `RelaunchAsAdminWithIPC` usa `syscall.EscapeArg` e timeout de 120 s; nova `LaunchElevatedHandoff(args []string) (pid uint32, err error)` não bloqueante (`ShellExecuteExW` com `SEE_MASK_NOCLOSEPROCESS`).
4. `pkg/config`: `MergeJSON(cur AppConfig, body []byte) (AppConfig, error)`; `SetOpenRouterKey(*AppConfig, plain string)` e `OpenRouterKey(AppConfig) string` com DPAPI (`windows.CryptProtectData`) em `secret_windows.go` e stub em `secret_other.go` que guarda base64 com `AIOpenRouterKeyPlain: true`; `Public() AppConfig` zera a chave e preenche `HasOpenRouterKey`; `SaveConfig` atômico no caminho resolvido uma vez (`ConfigPath()`), padrão ao lado do executável; `ServerPort` padrão 47321; `AIProvider` normaliza `direct` → `quick`.
5. `pkg/drives`: `IsWSL` e `DefaultSelected` conforme 1.12.
6. Testes: tabela de Pasta Protegida (`C:\`, `C:\Windows`, `C:\Windows\Temp\x`, `D:\System Volume Information\a`, `C:\Users\x` não protegido, `C:\Windows2` não protegido); `IsWithinRoots`; preflight em `C:\` e num caminho UNC inexistente; merge parcial preserva `ai*`; segredo cifra/decifra e nunca aparece em `Public()`; gravação atômica não deixa temporário; `EscapeArg` com espaços; `IsWSL`.

## 6. Agente E — interface: `ui/index.html`, `ui/js/app.js`, `ui/js/core.js` (novo), `ui/css/styles.css`, `ui/fonts/` (novo), `ui/tests/` (novo)

Implemente contra os contratos da seção 1; o servidor será ligado depois.

1. **Token** (1.1): leia da meta tag; `apiFetch(url, opts)` central que injeta `X-ScanFile-Token`; `EventSource('/api/events?token=')`; `sendBeacon('/api/ui/closed?token=')` em `pagehide`; `401` → aviso bloqueante "Sessão inválida, reabra o ScanFile".
2. **Config** (1.6): envie só campos alterados; chave OpenRouter só quando digitada; exiba "chave configurada" via `hasOpenRouterKey`; nunca sobrescreva com defaults quando o GET falhar.
3. **Escape de HTML**: `esc()` em `core.js`; nenhum `innerHTML` com texto do modelo, de arquivos, de rótulos de volume ou de caminhos sem `esc()`.
4. **Estado** (1.2, 1.8): `GET /api/scan/status` ao carregar e em `onopen`; fases `cancelling`/`cancelled`/`indexing` com botões coerentes; `409` mostra a fase atual; botão Cancelar visível até `cancelled`.
5. **Monitor**: implemente `fetchEventLogs`, `addFSEvent`, `renderEventLogs` e a lista de Itens Pulados (`/api/logs/skipped`).
6. **Árvore** (1.4): treemap com os 500 maiores; tabela paginada por `/api/tree/files`.
7. **Threads** (1.3): combo alimentado por `/api/system/info`, rótulo "Auto (32 na Fase 1, 16 na Fase 2)" calculado a partir de `numCPU`.
8. **Confirmações** (1.5): modal de reciclagem de pasta com tamanho, quantidade e nome digitado; ação separada "Excluir permanentemente" com texto `EXCLUIR`; resultados por item (`refused` com motivo).
9. **Assistente** (1.11): "Comandos Rápidos (sem modelo)"; catálogo com selos Visão/Ferramentas e aviso para modelos sem visão; execução de Proposta envia `confirm:true`.
10. **Discos** (1.12): WSL desmarcado com aviso.
11. **Fontes** (Q30): baixe Inter (400, 500, 600, 700) e JetBrains Mono (400, 500) em woff2 para `ui/fonts/` (use `curl` com User-Agent de Chrome contra o CSS do Google Fonts e baixe as URLs `latin`); `@font-face` local; remova o `<link>` externo.
12. **Limpeza**: remova os 28 `getElementById` mortos, listeners duplicados dos filtros de pastas, resíduos em inglês, `ev.ToolName`; `JSON.parse` protegido; `localStorage` só para preferências efêmeras.
13. **Zoom**: garanta que clique e desenho do treemap coincidam em 80% e 120% (use medidas em px de layout, não `getBoundingClientRect` escalado, ou troque `body.style.zoom` por `transform: scale` com compensação de largura). Extraia a função de mapeamento de coordenadas para `core.js` e teste-a.
14. **Testes** em `ui/tests/*.test.mjs` com `node:test`, sem dependências: `esc`, `formatBytes`, diff de config, squarify (área total preservada, sem sobreposição, aspecto ≤ 4 para itens grandes), mapeamento de coordenadas do treemap, seleção "manter mais recente" preservando uma cópia por grupo, validação de confirmações. `core.js` deve funcionar no navegador (global `ScanFileCore`) e no Node (`module.exports`).

## 7. Agente F — build e CI: `.github/workflows/ci-cd.yml`, `.gitlab-ci.yml`, `build.ps1`, `build.bat`, `LICENSE`, `.gitignore`

1. GitHub: `go-version-file: go.mod`; job de teste roda `go vet ./...`, `go test -race ./...` e `node --test ui/tests` (com `actions/setup-node@v4`, Node 22); build com `go build -ldflags ... -o scanfile.exe .`.
2. GitLab: imagem `golang:1.26`; job de teste com `CGO_ENABLED=1` (o `-race` exige) e Node instalado via `apt`; build com `CGO_ENABLED=0` e `.`; sufixo de pré-release `-pr.<iid>`; `DEFAULT_VERSION` usada ou removida; data com `date -u`.
3. `LICENSE` MIT, "Copyright (c) 2026 Chico Figueiredo".
4. `build.ps1`/`build.bat`: `go vet`, `go test ./...`, `node --test ui/tests` se `node` existir, build com `.`; parâmetro `-Race` no PowerShell.
5. `.gitignore`: `models/` já está; adicione `ui/fonts/*.tmp` se usar temporários; nada de fontes ignoradas.

## 8. Etapa 2 (depois da fusão) — `pkg/server`, `main.go`

Ligar tudo conforme a seção 1: middleware de token e `Origin`; injeção do token no `index.html`; remoção do CORS; handlers de reciclagem/exclusão com escopo, proteção, preflight e confirmações; `409`/cancelamento de pipeline com contexto próprio; `WriteTimeout` substituído por `ReadHeaderTimeout` + `IdleTimeout` e deadlines por handler via `http.ResponseController`; restore/load devolvendo `summary`; autosave com ticker único e "só se mudou"; `/api/tree/files`; `/api/system/info`; `/api/logs/skipped`; `/api/instance` + `instance.json` + porta fixa + instância única; presença e desligamento; handoff de elevação; `--mcp` com autosave; `MCPContext.SetAllowedRoots` a cada scan/load; watcher novo com `HashFunc` do hasher e revarredura em estouro. Testes com `httptest` para cada regra.

## 9. Etapa 3 — ADR-0001, nó compacto

Depois de tudo verde: `FileNode` sem `Path` armazenado (derivado do `DirNode` pai), extensão internada, hash em bytes fixos com algoritmo; `MarshalJSON` mantém exatamente o JSON atual (`path`, `name`, `size`, ..., `hash` como string com prefixo, `extension`). Meta: ≤ 150 bytes por item medidos por benchmark com 1 M de itens sintéticos.

### 8.1 Propriedade de arquivos na etapa 2 (`pkg/server` já dividido por responsabilidade)

| Agente | Arquivos exclusivos | Responsabilidades |
|---|---|---|
| S1 — segurança e arquivos | `auth.go`, `handlers_files.go`, `handlers_ai.go`, `auth_test.go`, `handlers_files_test.go`, `handlers_ai_test.go` | token e Origin (1.1), injeção do token no `index.html`, drives (1.12), reciclagem/exclusão com escopo, proteção, preflight e confirmações (1.5), config parcial e segredo (1.6), Assistente com aprovação, raízes permitidas e catálogo (1.11), `/api/system/info` (1.3) |
| S2 — estado da varredura | `server.go`, `sse.go`, `handlers_scan.go`, `handlers_cache.go` e seus `_test.go` | `409`/cancelamento com contexto próprio e fases (1.2), threads efetivas (1.3), `/api/tree` com teto e `/api/tree/files` (1.4), restore/load com `summary` e streaming (1.7), autosave com ticker único e "só se mudou", `/api/logs/skipped` (1.10), watcher novo com `HashFunc` e revarredura em estouro, `SetAllowedRoots` a cada scan/load, `NewAppServer` |
| S3 — ciclo de vida | `lifecycle.go`, `lifecycle_test.go`, `main.go` | `Start` sem `WriteTimeout` global (`ReadHeaderTimeout` + `IdleTimeout`, deadlines por handler via `http.ResponseController` onde precisar), porta fixa e fallback, `instance.json`, `/api/instance` e `/api/instance/focus`, presença e desligamento (1.9), `POST /api/ui/closed`, handoff de elevação, `--mcp` com autosave, `--no-window` |

Regras: `AppServer` já tem os campos de sessão/ciclo de vida/varredura pré-declarados em `server.go`; S1 e S3 usam esses campos e não editam `server.go` (se faltar um campo, peçam no relatório e usem uma variável de pacote temporária). Cada agente registra suas rotas na própria função `register*Routes`. `testutil_test.go` fornece `newTestServer(t)` para todos.

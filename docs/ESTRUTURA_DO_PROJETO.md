# Estrutura do projeto: ScanFile Pro

Mapa dos arquivos, dos pacotes Go e dos endpoints. Como o programa funciona está em [`ARQUITETURA.md`](ARQUITETURA.md); o vocabulário está em [`CONTEXT.md`](../CONTEXT.md); a visão de produto está no [`README.md`](../README.md).

## 1. Árvore de diretórios

Tudo o que está listado aqui existe no repositório. Arquivos gerados em tempo de execução estão marcados como tal.

```
ScanFile/
├── .github/
│   └── workflows/
│       └── ci-cd.yml                # CI de referência (GitHub Actions)
├── .gitlab-ci.yml                   # espelho do pipeline no GitLab
├── .agents/skills/                  # skills dos agentes, versionadas
├── docs/
│   ├── adr/
│   │   └── 0001-no-compacto-em-memoria.md
│   ├── ARQUITETURA.md               # funcionamento interno
│   ├── AVALIACAO.md                 # relatório de avaliação (histórico)
│   ├── DECISOES.md                  # as 39 decisões (histórico)
│   ├── ESTRUTURA_DO_PROJETO.md      # este arquivo
│   ├── IMPLEMENTACAO-CONTRATO.md    # contrato de implementação (histórico)
│   └── INTEGRACAO-ETAPA2.md         # notas de integração (histórico)
├── pkg/
│   ├── ai/                          # Provedores do Assistente e Comandos Rápidos
│   │   ├── agent.go                 # laço ReAct multi-turno com tool calling
│   │   ├── catalog.go               # catálogo de modelos com selo de visão/ferramentas
│   │   ├── memory_windows.go        # RAM disponível para o orçamento do catálogo
│   │   ├── memory_other.go
│   │   ├── ollama.go                # cliente Ollama
│   │   ├── openrouter.go            # cliente OpenRouter
│   │   ├── provider.go              # tipos de Provedor e normalização
│   │   ├── quick.go                 # roteador Comandos Rápidos
│   │   ├── types.go
│   │   └── catalog_test.go · prompt_test.go · quick_test.go
│   ├── config/                      # Configuração persistente e segredos
│   │   ├── config.go                # AppConfig, gravação atômica, MergeJSON, Public
│   │   ├── secret_windows.go        # DPAPI
│   │   ├── secret_other.go          # base64 marcado como não protegido
│   │   └── config_test.go
│   ├── drives/                      # volumes e capacidade
│   │   ├── types.go                 # DriveInfo, detecção de WSL/9P
│   │   ├── drives_windows.go        # GetVolumeInformationW, GetDiskFreeSpaceExW
│   │   ├── drives_other.go
│   │   └── drives_test.go
│   ├── hasher/                      # Fase 2
│   │   ├── algo.go                  # digests dos quatro algoritmos, pool de buffers
│   │   ├── prehash.go               # Pré-hash xxHash64 de 4 KB + 4 KB
│   │   ├── hasher.go                # pipeline, workers, progresso, cancelamento
│   │   └── algo_test.go · cancel_test.go · hasher_test.go · prehash_test.go
│   ├── indexer/                     # índices de consulta
│   │   ├── duplicate_index.go       # grupos por (hash, tamanho)
│   │   ├── folder_index.go          # Pastas Clones e nível de confiança
│   │   ├── idle_index.go            # Arquivos Ociosos em streaming
│   │   └── duplicate_index_test.go · folder_confidence_test.go
│   │       · folder_index_test.go · idle_index_test.go · incremental_test.go
│   ├── mcp/                         # ferramentas do Assistente e servidor MCP
│   │   ├── server.go                # registro das 6 ferramentas, stdio, WithRecovery
│   │   ├── tools.go                 # implementação, Propostas, ExecuteProposal
│   │   ├── roots.go                 # escopo das Raízes Varridas
│   │   ├── autosave.go              # contexto a partir do último Autosave (--mcp)
│   │   ├── sqlite.go                # inspeção somente leitura de bancos SQLite
│   │   └── autosave_test.go · proposal_test.go · roots_test.go
│   │       · server_test.go · sqlite_test.go
│   ├── privileges/                  # UAC, privilégios, IPC, guarda de PID
│   │   ├── privileges_windows.go
│   │   ├── privileges_other.go
│   │   └── privileges_windows_test.go
│   ├── recycle/                     # Reciclagem e Exclusão Permanente
│   │   ├── policy.go                # Pasta Protegida e escopo das raízes
│   │   ├── types.go                 # lote, resultado por item, Preflight
│   │   ├── recycle_windows.go       # SHFileOperationW, capacidade da Lixeira
│   │   ├── recycle_other.go
│   │   └── policy_test.go · recycle_windows_test.go · types_test.go
│   ├── scanner/                     # Fase 1, árvore, Snapshot
│   │   ├── types.go                 # FileNode, DirNode, ScanConfig, ScanStatus
│   │   ├── scanner.go               # orquestração da Fase 1, Itens Pulados
│   │   ├── queue.go                 # fila de diretórios com sync.Cond
│   │   ├── workers.go               # ResolveWorkers, MaxThreads, ThreadOptions
│   │   ├── skip.go                  # motivos de Item Pulado, detecção de WSL
│   │   ├── algo.go                  # identificadores e prefixos dos algoritmos
│   │   ├── tree.go                  # TreeManager, GetDirSummary, GetFilesPage
│   │   ├── tree_watch.go            # atualização incremental pelo Monitoramento
│   │   ├── cache.go                 # Snapshot e Autosave em streaming
│   │   ├── error_logger.go          # log de erros em disco com flush periódico
│   │   ├── filetimes_windows.go     # tamanho alocado e compressão NTFS
│   │   ├── filetimes_other.go
│   │   ├── reparse_windows.go       # junções e links simbólicos
│   │   ├── reparse_other.go
│   │   ├── volume_windows.go        # sistema de arquivos do volume
│   │   ├── volume_other.go
│   │   ├── testdata/
│   │   │   ├── legacy_v2.scanfile.gz
│   │   │   └── gen/main.go          # gerador de fixtures
│   │   └── algo_test.go · cache_stream_test.go · cache_test.go
│   │       · error_logger_test.go · queue_test.go · scanner_test.go
│   │       · skip_test.go · tree_page_test.go · tree_test.go
│   │       · tree_watch_test.go · workers_test.go
│   ├── server/                      # servidor HTTP, Sessão, ciclo de vida
│   │   ├── server.go                # AppServer, relógio do Autosave, estado
│   │   ├── auth.go                  # token da Sessão, Origin, ausência de CORS
│   │   ├── static.go                # UI embutida e injeção do token
│   │   ├── sse.go                   # broadcast e handler de /api/events
│   │   ├── lifecycle.go             # porta, instância única, presença, handoff
│   │   ├── handlers_scan.go         # Varredura, árvore, duplicados, logs
│   │   ├── handlers_cache.go        # Snapshot e Autosave
│   │   ├── handlers_files.go        # discos, Reciclagem, Exclusão, Configuração
│   │   ├── handlers_ai.go           # Assistente e aprovação de Propostas
│   │   ├── memory_windows.go        # RAM do processo e do sistema
│   │   ├── memory_other.go
│   │   └── auth_test.go · handlers_ai_test.go · handlers_cache_test.go
│   │       · handlers_files_test.go · handlers_scan_test.go
│   │       · integration_test.go · lifecycle_test.go · server_test.go
│   │       · static_test.go · testutil_test.go
│   └── watcher/                     # Monitoramento
│       ├── watcher.go               # coalescência, fila de hash, atualização
│       ├── watcher_windows.go       # ReadDirectoryChangesW recursivo
│       ├── watcher_other.go         # substituto com fsnotify
│       └── watcher_test.go · watcher_windows_test.go
├── ui/                              # interface, embutida por //go:embed ui/*
│   ├── css/styles.css
│   ├── fonts/
│   │   ├── inter-latin.woff2
│   │   └── jetbrains-mono-latin.woff2
│   ├── js/
│   │   ├── app.js                   # telas, SSE, treemap, navegação
│   │   └── core.js                  # funções puras, window.ScanFileCore + module.exports
│   ├── tests/
│   │   ├── actions.test.mjs · app-smoke.test.mjs · config.test.mjs
│   │   └── format.test.mjs · system.test.mjs · treemap.test.mjs
│   ├── index.html                   # inclui o literal {{SCANFILE_TOKEN}}
│   └── package.json
├── build.bat                        # go vet, go test, testes da UI, build
├── build.ps1                        # o mesmo, com -Race opcional
├── CONTEXT.md                       # glossário do domínio
├── go.mod                           # module scanfile · go 1.26.7
├── go.sum
├── LICENSE                          # MIT
├── main.go                          # flags, modo MCP, elevação, Janela, embed
├── main_test.go
├── README.md
└── scanfile_config.example.json     # modelo sem segredos
```

Gerados em tempo de execução e fora do controle de versão: `scanfile.exe`, `scanfile_config.json`, `logs/`, `saved_scans/` (com `autosave_latest.sfz` e `autosave_previous.sfz`), `dist/` e `%LOCALAPPDATA%\ScanFile\instance.json`.

## 2. Pacotes Go

São 11 pacotes em `pkg/`, mais o pacote `main` na raiz.

| Pacote | Responsabilidade |
| :--- | :--- |
| **`pkg/ai`** | Provedores do Assistente: cliente Ollama, cliente OpenRouter e o roteador Comandos Rápidos, que aciona ferramentas por palavras-chave sem modelo de linguagem. Mantém o catálogo de modelos limitado a ~14 GB com selo de visão e de ferramentas, e o laço ReAct multi-turno com tool calling. Normaliza o identificador legado `direct` para `quick`. |
| **`pkg/config`** | Carrega e grava `scanfile_config.json` de forma atômica e síncrona. `MergeJSON` aplica documentos parciais, para que salvar uma preferência não apague outra. A chave do OpenRouter é protegida por DPAPI no Windows e `Public()` a remove de tudo o que sai pela API. |
| **`pkg/drives`** | Lista volumes com `GetVolumeInformationW` e `GetDiskFreeSpaceExW`: letra, rótulo, sistema de arquivos, tipo, total, livre e percentual. Marca volumes 9P do WSL, que aparecem na interface com aviso. Volumes de WSL, de rede, CD-ROM e os que não puderam ser sondados começam desmarcados (`defaultSelected: false`). |
| **`pkg/hasher`** | Fase 2. Implementa o Pré-hash (xxHash64 de 4 KB + 4 KB) e o Hash Completo em xxHash64, BLAKE3, MD5 ou SHA-256, com pool de buffers de 1 MiB, workers cancelável e progresso detalhado por arquivo. |
| **`pkg/indexer`** | Índices sob `sync.RWMutex`: duplicados por (hash, tamanho), Pastas Clones com nível de confiança `hash` ou `size_mtime`, e Arquivos Ociosos consultados em streaming. Reconstrução incremental marcada como suja pelo Monitoramento. |
| **`pkg/mcp`** | As seis ferramentas do Assistente e o servidor MCP por stdio. Guarda as Propostas pendentes, com validade de 30 minutos, e o escopo das Raízes Varridas que toda leitura de arquivo precisa respeitar. `ExecuteProposal` só é alcançável a partir da aprovação humana. |
| **`pkg/privileges`** | Estado do UAC, habilitação dos seis privilégios do token, relançamento elevado com `SW_HIDE`, canal IPC em loopback para devolver a saída ao terminal original e guarda anti-zumbi por PID do pai. |
| **`pkg/recycle`** | Política e execução de Reciclagem e Exclusão Permanente: Pasta Protegida, escopo das Raízes Varridas, verificação prévia da Lixeira do volume e `SHFileOperationW` com `FOF_ALLOWUNDO`. Devolve um resultado por item, nunca um sucesso agregado. |
| **`pkg/scanner`** | Fase 1 e a árvore em memória: fila de diretórios, metadados com tamanho alocado e compressão NTFS, junções e links simbólicos, Itens Pulados com motivo, log de erros em disco, resolução de threads e leitura e escrita de Snapshot em streaming. |
| **`pkg/server`** | Servidor HTTP em `127.0.0.1`, token da Sessão, ausência de CORS, rotas REST, SSE, servidor da interface embutida com injeção do token, porta fixa com instância única, presença da Janela, handoff de elevação e o relógio único do Autosave. |
| **`pkg/watcher`** | Monitoramento recursivo das Raízes Varridas com `ReadDirectoryChangesW`, coalescência de 2 s por caminho, dois workers de hash em segundo plano e revarredura da raiz afetada quando o buffer do sistema estoura. |

## 3. Endpoints

Todos vivem sob `/api/` e exigem o token da Sessão no cabeçalho `X-ScanFile-Token`, com duas exceções descritas abaixo. Nenhuma resposta traz cabeçalho CORS. O caminho `/` e os demais serve a interface embutida.

### 3.1 Varredura e árvore

| Método | Endpoint | Descrição |
| :--- | :--- | :--- |
| `POST` | `/api/scan/start` | Inicia a Varredura com o `ScanConfig` do corpo. Devolve **409** se já houver uma em curso. |
| `POST` | `/api/scan/cancel` | Cancela o pipeline inteiro. Leva o status a `cancelling` e depois a `cancelled`. |
| `GET` | `/api/scan/status` | Retrato completo da Varredura, incluindo memória do processo. |
| `GET` | `/api/events` | Fluxo SSE: `scan_progress`, `fs_event`, `autosave_done`, `shutdown`. Aceita o token na query. |
| `GET` | `/api/tree` | Árvore a partir de `path`, com `depth` até 8 e no máximo 500 arquivos diretos por pasta. |
| `GET` | `/api/tree/files` | Página de arquivos de uma pasta: `path`, `offset`, `limit` (100 padrão, 500 máximo), `sortBy`. |
| `GET` | `/api/duplicates` | Grupos de duplicados, com filtros de tamanho e busca, ordenação e paginação. |
| `GET` | `/api/folders/duplicates` | Pastas Clones, espaço desperdiçado e nível de confiança. Aceita `topLevelOnly`. |
| `POST` | `/api/folders/compare` | Compara duas pastas: idênticos, modificados e exclusivos, com nível de confiança. |
| `GET` | `/api/stats/extensions` | Distribuição de espaço por extensão. |
| `GET` | `/api/stats/idle-files` | Arquivos Ociosos por idade mínima e tamanho mínimo, paginados. |
| `GET` | `/api/system/memory` | Memória do processo e da máquina. Alimenta a barra de memória. |

### 3.2 Logs

| Método | Endpoint | Descrição |
| :--- | :--- | :--- |
| `GET` | `/api/logs` | Últimos eventos do Monitoramento mantidos em memória (anel de 200). |
| `GET` | `/api/logs/skipped` | Itens Pulados da Varredura corrente, com motivo. Anel de 500 no motor, `limit` de 200 padrão e 500 máximo. |
| `GET` | `/api/logs/errors/list` | Arquivos de log de erro presentes em `logs/`. |
| `GET` | `/api/logs/errors/active` | Conteúdo do log de erros da Varredura corrente. |

### 3.3 Snapshot e Autosave

| Método | Endpoint | Descrição |
| :--- | :--- | :--- |
| `POST` | `/api/cache/save` | Grava um Snapshot em `saved_scans/`, com extensão `.scanfile.gz`. |
| `POST` | `/api/cache/load` | Carrega um Snapshot e devolve o **resumo**, nunca a lista de arquivos. |
| `GET` | `/api/cache/list` | Snapshots e Autosaves disponíveis em `saved_scans/`. |
| `GET` | `/api/cache/autosave/status` | Existência, data e tamanho do último Autosave. |
| `POST` | `/api/cache/autosave/restore` | Restaura `autosave_latest.sfz` e devolve o **resumo**. |

### 3.4 Discos, arquivos e Configuração

| Método | Endpoint | Descrição |
| :--- | :--- | :--- |
| `GET` | `/api/drives` | Volumes com rótulo, sistema de arquivos, tipo, espaço, marca de WSL e `defaultSelected`. |
| `POST` | `/api/files/recycle` | Envia para a Lixeira. Exige escopo nas Raízes Varridas e, para pasta, o nome digitado em `confirmName`. |
| `POST` | `/api/files/delete` | Exclusão Permanente. Exige `confirmText` igual a `EXCLUIR`. |
| `GET` | `/api/config` | Configuração sem segredo: a chave do OpenRouter vira o booleano `hasOpenRouterKey`. |
| `POST` | `/api/config` | Aplica um documento **parcial**: só as chaves presentes mudam. |
| `GET` | `/api/system/privileges` | Estado do UAC e privilégios ativos. |
| `GET` | `/api/system/info` | Versão, porta em uso, elevação, `numCPU`, teto de threads e opções do combo. |

### 3.5 Assistente

| Método | Endpoint | Descrição |
| :--- | :--- | :--- |
| `GET` | `/api/ai/models` | Catálogo de modelos com selo de visão e de ferramentas, e o que já está instalado. |
| `POST` | `/api/ai/models/pull` | Baixa um modelo pelo Ollama. |
| `POST` | `/api/ai/chat` | Turno de conversa. Pode devolver uma Proposta pendente. |
| `POST` | `/api/ai/actions/execute` | **Aprovação humana**: executa uma Proposta pendente pelo `proposalID`. |
| `GET` | `/api/ai/status` | Se o Ollama responde, versão, endpoint, modelos instalados e o booleano `hasOpenRouterKey`. |

### 3.6 Ciclo de vida

| Método | Endpoint | Descrição |
| :--- | :--- | :--- |
| `GET` | `/api/instance` | **Sem token.** Devolve `{app, version, pid}`. É como uma segunda execução reconhece a instância viva. |
| `POST` | `/api/instance/focus` | Traz a Janela existente para frente. |
| `POST` | `/api/ui/closed` | A Janela avisa que fechou. Aceita o token na query, porque é chamado por `sendBeacon`. |
| `POST` | `/api/system/elevate` | Eleva por handoff: o filho assume a porta e o token, e esta instância encerra. |

### 3.7 Interface estática

| Método | Endpoint | Descrição |
| :--- | :--- | :--- |
| `GET` | `/` e `/index.html` | Página inicial, renderizada com o token da Sessão injetado e `Cache-Control: no-store`. |
| `GET` | `/css/…`, `/js/…`, `/fonts/…` | Assets servidos da memória do processo pelo sistema de arquivos embutido. |

### 3.8 Observações

- **Códigos de erro**: 401 com `{"error":"unauthorized"}` para token ausente ou errado; 403 para `Origin` de outra origem; 409 para segunda Varredura; 405 quando o método não confere.
- **Verificação de método**: recusam explicitamente o método errado `/api/scan/start`, `/api/files/recycle`, `/api/files/delete`, `/api/config`, `/api/cache/save`, `/api/cache/load`, `/api/cache/autosave/restore`, `/api/ai/models/pull`, `/api/ai/chat`, `/api/ai/actions/execute`, `/api/instance`, `/api/instance/focus`, `/api/ui/closed`, `/api/system/elevate` e a página inicial. Os demais são consultas de leitura e não impõem o verbo; a coluna "Método" indica o que a interface usa.

## 4. Interface

| Arquivo | Conteúdo |
| :--- | :--- |
| `ui/index.html` | Estrutura das oito abas, seletor de 12 temas, controles de Varredura e ícones SVG escritos direto no HTML — não há arquivos de ícone. Traz o literal `{{SCANFILE_TOKEN}}`, substituído pelo servidor. |
| `ui/css/styles.css` | Temas, layout de largura total, treemap, zoom e as declarações `@font-face` que apontam para `ui/fonts/`. Nenhuma referência a CDN. |
| `ui/js/app.js` | Telas, chamadas à API com o token, conexão SSE, desenho do treemap em Canvas e navegação. |
| `ui/js/core.js` | Funções puras compartilhadas: escape de HTML, formatação, paginação, `squarify` e mapeamento de clique no canvas. Escrito sem ESM de propósito: o navegador recebe `window.ScanFileCore` e o Node recebe `module.exports`, então os testes rodam sobre o mesmo código que a página usa. |
| `ui/fonts/` | Inter e JetBrains Mono, subconjunto latino, embutidas no binário. |
| `ui/tests/` | Seis arquivos de teste executados por `node --test`. |

As oito abas são: Discos e Varredura, Gráfico e Árvore, Duplicados por Hash, Comparador de Pastas, Distribuição, Arquivos Ociosos, Monitor do SO e Assistente IA.

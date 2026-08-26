# Estrutura do Projeto: ScanFile Pro

Este documento descreve a organização dos pacotes, arquivos fonte, endpoints HTTP e o frontend da aplicação.

---

## 1. Árvore de Diretórios

```
p:/Chico/ScanFile/
├── docs/                        # Documentação técnica e arquitetural
│   ├── ARQUITETURA.md           # Detalhes de funcionamento, memória, concorrência e Win32
│   └── ESTRUTURA_DO_PROJETO.md  # Mapeamento de pacotes, rotas e componentes
├── logs/                        # Logs de execução em disco (gerados com --log)
├── pkg/                         # Módulos e pacotes Go (Backend Engine)
│   ├── config/                  # Persistência e gestão de preferências (JSON)
│   │   ├── config.go
│   │   └── config_test.go
│   ├── drives/                  # Detecção e métricas de discos e volumes Win32
│   │   ├── drives.go
│   │   └── drives_windows.go
│   ├── hasher/                  # Motor de hashing progressivo multithread
│   │   ├── hasher.go
│   │   └── hasher_test.go
│   ├── indexer/                 # Indexador O(1) de arquivos duplicados e pastas
│   │   ├── indexer.go
│   │   └── indexer_test.go
│   ├── privileges/              # Elevação UAC, SeBackupPrivilege, IPC e Anti-Zombie
│   │   ├── privileges_other.go
│   │   └── privileges_windows.go
│   ├── recycle/                 # Integração nativa com a Lixeira do Windows (SHFileOperation)
│   │   ├── recycle_other.go
│   │   └── recycle_windows.go
│   ├── scanner/                 # Motor de varredura concorrente e Árvore de diretórios
│   │   ├── scanner.go
│   │   ├── scanner_test.go
│   │   └── tree.go
│   ├── server/                  # Servidor REST API, Server-Sent Events e Roteamento
│   │   └── server.go
│   └── watcher/                 # Monitoramento de alterações em tempo real no sistema de arquivos
│       └── watcher.go
├── ui/                          # Frontend Web Moderno (Embutido no binário via //go:embed)
│   ├── css/
│   │   └── styles.css           # Estilos Dark Glassmorphism, Treemap, Full-Width e Zoom
│   ├── js/
│   │   └── app.js               # Lógica da interface, SSE, Canvas Treemap e navegação
│   └── index.html               # Estrutura HTML5 com todas as abas e componentes
├── build.bat                    # Script de compilação em lote para Windows
├── build.ps1                    # Script PowerShell automatizado com testes e verificação
├── go.mod                       # Definição de dependências Go
├── go.sum                       # Checksums das dependências
├── main.go                      # Ponto de entrada, CLI flags, Edge App mode e embed FS
├── scanfile.exe                 # Binário executável final nativo (autocontido)
└── scanfile_config.json         # Arquivo de configuração e preferências persistidas
```

---

## 2. Descrição dos Pacotes Go (`pkg/`)

| Pacote | Responsabilidade |
| :--- | :--- |
| **`pkg/config`** | Gerencia a carga e salvamento atômico das preferências do usuário (`scanfile_config.json`), incluindo discos selecionados, threads, algoritmos de hash, filtros, profundidade do treemap, esquemas de cores e nível de zoom da UI. |
| **`pkg/drives`** | Consulta a API Win32 do Windows (`GetVolumeInformationW`, `GetDiskFreeSpaceExW`) para listar letras de unidades, tipos de disco (Fixed, Removable, Network), espaço total, espaço livre e percentual de uso. |
| **`pkg/hasher`** | Executa o cálculo paralelo de hashes utilizando a técnica *Smart Progressive Hashing* (Pre-hash -> Mid-hash -> Full-hash). Suporta `xxHash`, `BLAKE3`, `MD5` e `SHA-256`. |
| **`pkg/indexer`** | Mantém estruturas de dados concorrentes protegidas por mutexes (`sync.RWMutex`) para agrupamento e consulta $O(1)$ de duplicatas de arquivos e comparação de diretórios inteiros. |
| **`pkg/privileges`** | Verifica o status UAC do Windows, habilita privilégios de Administrador (`SeBackupPrivilege`, `SeRestorePrivilege`), gerencia o redirecionamento IPC do processo elevado invisível (`SW_HIDE`) e monitora o PID do processo pai para evitar processos zumbis. |
| **`pkg/recycle`** | Executa a movimentação segura de arquivos e pastas para a Lixeira nativa do Windows utilizando a API `shell32.dll` (`SHFileOperationW`), preservando a opção de desfazer / restaurar. |
| **`pkg/scanner`** | Realiza a varredura multithread do sistema de arquivos e mantém a hierarquia de diretórios em memória (`TreeManager`) com agregação de tamanhos, contagens de arquivos e datas de criação/modificação. |
| **`pkg/server`** | Servidor HTTP embutido que expõe a REST API JSON, canais de streaming SSE a 60 FPS e serve a UI estática embutida (`ui/`). |
| **`pkg/watcher`** | Serviço de observação de mudanças em tempo real no disco para atualizar a árvore automaticamente caso arquivos sejam criados, renomeados ou deletados. |

---

## 3. Endpoints da API REST e SSE

| Método | Endpoint | Descrição |
| :--- | :--- | :--- |
| `GET` | `/api/drives` | Retorna lista de volumes e discos com espaço e tipo |
| `POST` | `/api/scan/start` | Inicia uma nova varredura nos discos selecionados |
| `POST` | `/api/scan/cancel` | Interrompe imediatamente a varredura em andamento |
| `GET` | `/api/scan/status` | Retorna o estado atual do motor de varredura |
| `GET` | `/api/events` | Stream **SSE (Server-Sent Events)** para telemetria em tempo real a 60 FPS |
| `GET` | `/api/tree` | Consulta a árvore de diretórios (`path` e `depth` configuráveis) |
| `GET` | `/api/duplicates` | Retorna grupos de arquivos duplicados por hash com filtros de tamanho |
| `GET` | `/api/folders/duplicates` | Retorna pastas duplicadas e espaço desperdiçado |
| `POST` | `/api/folders/compare` | Executa comparação visual e estrutural entre duas pastas |
| `GET` | `/api/stats/extensions` | Estatísticas de distribuição de espaço por tipo de arquivo / extensão |
| `GET` | `/api/stats/idle-files` | Localiza arquivos não modificados há mais de $N$ dias com tamanho $\ge X$ MB |
| `POST` | `/api/files/recycle` | Envia arquivos ou pastas para a **Lixeira do Windows** |
| `POST` | `/api/files/delete` | Exclusão definitiva de arquivos (quando solicitado) |
| `GET` | `/api/config` | Carrega as preferências salvas do usuário |
| `POST` | `/api/config` | Salva as opções selecionadas no `scanfile_config.json` |
| `GET` | `/api/system/privileges` | Retorna status de elevação UAC e tokens ativos |
| `POST` | `/api/system/elevate` | Solicita elevação de privilégios UAC ao Windows |
| `POST` | `/api/cache/save` | Salva snapshot da árvore e hashes em arquivo de cache |
| `POST` | `/api/cache/load` | Carrega snapshot de cache previamente gravado |
| `GET` | `/api/logs` | Retorna logs recentes da aplicação |
| `GET` | `/api/logs/errors/active`| Retorna relatório de erros encontrados durante a varredura |

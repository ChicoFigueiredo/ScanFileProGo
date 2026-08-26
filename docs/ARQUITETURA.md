# Arquitetura do ScanFile Pro

Este documento detalha o funcionamento interno, as decisões de design e a arquitetura completa do **ScanFile Pro (Windows Native Engine)**.

---

## 1. O Executável é Autocontido?

**Sim, 100% autocontido.**

O `scanfile.exe` é um binário estático e independente do ecossistema Windows:
- **Zero Runtimes Externos**: Não depende de .NET Framework/.NET Core, Node.js, Python, Java ou Electron.
- **Zero Instaladores / DLLs Externas**: Não requer instalação de pacotes C++ Redistributable ou bibliotecas dinâmicas de terceiros.
- **Portabilidade Total**: Basta copiar o arquivo `scanfile.exe` para qualquer pendrive ou máquina Windows (Windows 10, 11, Server) e executá-lo diretamente.

---

## 2. Como Funciona a Interface HTML? (Memória vs Disco)

### A interface é extraída para o disco em alguma pasta temporária?
**Não! Nenhuma pasta temporária é criada em disco para a interface.**

### Como a interface é armazenada e servida:
1. **Embutida na Compilação (`//go:embed`)**:
   - No arquivo `main.go`, a diretiva nativa do compilador Go `//go:embed ui/*` captura todos os arquivos da pasta `ui/` (`index.html`, `css/styles.css`, `js/app.js`, ícones SVG, etc.) e os empacota diretamente como **dados binários imutáveis na seção `.rodata` do próprio executável (`.exe`)**.
2. **Servida Diretamente da Memória RAM**:
   - O servidor HTTP embutido em Go utiliza a abstração `io/fs.FS` (`http.FS(uiSubFS)`).
   - Quando o frontend solicita a página inicial ou seus assets estáticos (`GET /`, `GET /css/styles.css`, `GET /js/app.js`), o servidor lê os bytes **diretamente da memória RAM do processo**.
   - Isso elimina qualquer overhead de I/O em disco para a UI, previne problemas de permissões em pastas de `%TEMP%` e garante carregamento em microssegundos.

---

## 3. Visão Geral da Arquitetura

O ScanFile Pro é estruturado em **duas camadas integradas**:

```
+-------------------------------------------------------------------------+
|                  SCANFILE PRO (PROCESSO ÚNICO / .EXE)                   |
|                                                                         |
|  +-------------------------------------------------------------------+  |
|  |                       CAMADA DE APRESENTAÇÃO                      |  |
|  |  * Microsoft Edge App Mode (Janela Nativa Webview Chromium)       |  |
|  |  * Interface HTML5 / CSS3 Glassmorphism / Vanilla JS Moderno      |  |
|  |  * Canvas 2D Treemap de Alta Performance (Squarified Bruls Alg.)  |  |
|  +-------------------------------------------------------------------+  |
|                                  ^                                      |
|                                  | REST API (JSON) + SSE (60 FPS)       |
|                                  v                                      |
|  +-------------------------------------------------------------------+  |
|  |                     CAMADA DE BACKEND & ENGINE                    |  |
|  |                                                                   |  |
|  |  [ Servidor HTTP Nativo ]  <-->  [ Loopback 127.0.0.1:RandomPort ]|  |
|  |                                                                   |  |
|  |  [ pkg/scanner ]     -> Varredura paralela, Árvore de Diretórios |  |
|  |  [ pkg/hasher ]      -> Smart Hash (xxHash, Blake3, MD5, SHA256) |  |
|  |  [ pkg/indexer ]     -> Índice O(1) de duplicatas e pastas       |  |
|  |  [ pkg/privileges ]  -> SeBackupPrivilege + Anti-Zombie IPC       |  |
|  |  [ pkg/config ]      -> Persistência JSON (scanfile_config.json)  |  |
|  |  [ pkg/recycle ]     -> Win32 SHFileOperation (Lixeira Windows)  |  |
|  |  [ pkg/drives ]      -> Win32 API GetVolumeInformation / Discos   |  |
|  +-------------------------------------------------------------------+  |
+-------------------------------------------------------------------------+
```

---

## 4. Detalhamento dos Componentes

### 4.1. Camada de Apresentação (Frontend)
- **Modo Janela Nativa (Edge App Mode)**:
  - O aplicativo inicia o Edge com a flag `--app=http://127.0.0.1:<porta>`, que executa o motor Chromium em uma janela de aplicativo independente sem barras de endereço, menus de navegador ou extensões.
  - Aproveita a aceleração por hardware (GPU) para animações, sombras glassmorphism e renderização do Canvas.
- **Gráfico da Estrutura (Cushion Treemap)**:
  - Implementado em HTML5 Canvas puro com resolução ajustada ao `devicePixelRatio` da tela.
  - Utiliza o algoritmo *Squarified Treemap* (Bruls, Huizing, van Wijk) para gerar retângulos com aspect ratio próximo a 1:1.
  - Shading volumétrico (*cushion gradient*) e suporte a drill-down (zoom-in/zoom-out com duplo clique ou menu de contexto).
- **Telemetria em Tempo Real (Server-Sent Events)**:
  - O frontend mantém uma conexão persistente via `/api/events` onde recebe atualizações de progresso, taxa de I/O em MB/s, arquivos/s e status dos workers sem sobrecarregar a CPU com requisições periódicas (*no polling*).

---

### 4.2. Motor de Varredura e Árvore em Memória (`pkg/scanner`)
- **Varredura Concorrente**:
  - Utiliza um pool configurável de workers (threads leves/goroutines) que dividem a leitura de múltiplos diretórios e discos em paralelo.
- **Árvore de Diretórios em Memória (`TreeManager`)**:
  - Constrói uma hierarquia em RAM com agregação recursiva de tamanhos (`totalSize`), contagem de arquivos (`fileCount`), contagem de subpastas (`subDirCount`), datas de modificação (`modTime`) e datas de criação (`createTime`).
  - Permite consultas instantâneas com profundidade controlada (`/api/tree?path=...&depth=N`).

---

### 4.3. Motor de Hashing Inteligente Progressivo (`pkg/hasher`)
O maior gargalo na detecção de duplicados em discos grandes é o I/O de leitura. O ScanFile Pro implementa uma estratégia em 3 estágios:

```
[ Arquivos com mesmo tamanho em bytes ]
                 │
                 ▼
 ┌────────────────────────────────┐
 │ 1. Filtro de Tamanho Único     │ -> Arquivos com tamanho exclusivo são descartados
 └────────────────────────────────┘    (0 bytes lidos do disco!)
                 │
                 ▼
 ┌────────────────────────────────┐
 │ 2. Smart Pre-Hash (4 KB)       │ -> Lê apenas os primeiros 4 KB do cabeçalho
 └────────────────────────────────┘
                 │ (Apenas se houver colisão de cabeçalho)
                 ▼
 ┌────────────────────────────────┐
 │ 3. Smart Mid-Hash (4 KB Final) │ -> Lê os 4 KB do final do arquivo
 └────────────────────────────────┘
                 │ (Apenas se persistir colisão)
                 ▼
 ┌────────────────────────────────┐
 │ 4. Full Hash (Fluxo Completo)  │ -> Calcula o hash de 100% dos dados
 └────────────────────────────────┘
```

- **Algoritmos Disponíveis**:
  - `xxhash`: Algoritmo extremamente rápido (ultrapassa 5 GB/s por núcleo).
  - `blake3`: Alta velocidade criptográfica com paralelismo em árvore.
  - `md5` / `sha256`: Algoritmos clássicos para conformidade e validação rigorosa.

---

### 4.4. Elevação de Privilégios e Proteção Anti-Zumbi (`pkg/privileges`)

1. **Bypass de Permissões NTFS (`SeBackupPrivilege`)**:
   - Ao executar com privilégios de Administrador, o processo habilita os tokens `SeBackupPrivilege` e `SeRestorePrivilege` no Windows Security Token.
   - Isso permite que o mecanismo leia e indexe pastas protegidas do sistema (como `System Volume Information`, `WindowsApps`, pastas de outros perfis de usuários) sem erros de "Acesso Negado".
2. **Execução Invisível com Redirecionamento IPC**:
   - Ao chamar `scanfile.exe --admin` a partir de um terminal comum, o processo original dispara a instância elevada com `SW_HIDE` (invisível, sem nova janela de console).
   - O processo filho conecta-se a um socket IPC local criado pelo pai e transmite todos os logs em tempo real para o terminal original.
3. **Guarda Anti-Zumbi (Parent PID Watcher)**:
   - O processo pai transmite seu PID (`--parent-pid=XXXX`) para o processo filho.
   - O filho abre um handle nativo via `windows.OpenProcess(windows.SYNCHRONIZE, ...)` e monitora com `windows.WaitForSingleObject`.
   - Caso o terminal pai seja encerrado, fechado no "X" ou morto pelo Gerenciador de Tarefas, o processo filho é **terminado instantaneamente**, garantindo zero processos órfãos.

---

### 4.5. Configurações Persistentes (`pkg/config`)
- Arquivo: `scanfile_config.json`.
- Armazena:
  - Discos selecionados (`selectedRoots`).
  - Quantidade de threads (`workerThreads`).
  - Algoritmo e estratégia de Hash (`hashAlgorithm`, `hashMode`, `minFileSize`).
  - Configurações do Treemap (`treemapDepth`, `treemapColorMode`, `treemapViewMode`).
  - Nível de Zoom da Interface (`uiZoom`).
  - Filtros de Duplicatas e Arquivos Ociosos.
- Atualizado e restaurado de forma atômica e assíncrona.

---

### 4.6. Lixeira do Windows e Exclusão Segura (`pkg/recycle`)
- Utiliza a API Win32 `SHFileOperationW` com a flag `FO_DELETE` e `FOF_ALLOWUNDO`.
- Os arquivos selecionados são enviados para a **Lixeira oficial do Windows**, permitindo que o usuário restaure qualquer arquivo excluído acidentalmente com suporte total a desfazer.

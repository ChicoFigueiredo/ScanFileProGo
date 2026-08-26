# ScanFile Pro 🚀

> **Analisador de Espaço em Disco de Alta Performance, Deduplicação Inteligente por Hash e Monitoramento do SO em Tempo Real para Windows (Escrito em Go).**

---

## 📋 Visão Geral

O **ScanFile Pro** é uma ferramenta nativa para Windows desenvolvida em Go, projetada para combinar a velocidade de análise de ferramentas como *TreeSize* e *WinDirStat* com a precisão de deduplicação por hash criptográfico e ganchos (*hooks*) de monitoramento do sistema de arquivos em tempo real.

---

## ✨ Principais Funcionalidades

1. **Interface Nativa Windows Moderna (Dark Theme & Glassmorphism)**:
   - Interface fluida com gráficos, medidores visuais de ocupação de disco, tabela em árvore interativa e sem congelamentos na UI.
   - Totalmente embutida dentro do executável (`//go:embed`), sem necessidade de instalar dependências externas no cliente.

2. **Seleção de Discos & Scanner Multithread Paralelo**:
   - Detecta todos os discos lógicos do Windows (C:, D:, E:, etc.), exibindo capacidade total, espaço livre e porcentagem de uso.
   - Permite selecionar qualquer combinação de drives ou diretórios raiz.
   - Dispara goroutines/threads dedicadas por disco raiz + worker pool dinâmico para varredura recursiva de diretórios em alta velocidade.

3. **Varredura em Duas Fases (Two-Phase Scan)**:
   - **Fase 1 (Metadados em Memória)**: Mapeia toda a hierarquia de pastas, tamanhos e datas na velocidade máxima do disco, permitindo que você comece a navegar na árvore imediatamente.
   - **Fase 2 (Hash Multithread Inteligente)**:
     - Algoritmo em 3 estágios: agrupa arquivos pelo tamanho exato em bytes $\rightarrow$ calcula pré-amostra rápida (xxHash64) $\rightarrow$ calcula hash completo apenas para arquivos com mesma amostra.
     - Opção de escolha entre **xxHash64** (ultra rápido, 10+ GB/s) e **SHA-256** (padrão criptográfico).

4. **Explorador Hierárquico em Árvore (Estilo TreeSize / WinDirStat)**:
   - Navegação por pastas com barras de porcentagem visual em relação ao disco/pasta pai, contagem de subpastas e arquivos, e ordenação automática do maior para o menor.
   - Breadcrumbs clicáveis e filtro instantâneo por nome/extensão (.mp4, .zip, etc.).

5. **Agrupamento por Hash & Localizador de Duplicados**:
   - Agrupamento de arquivos com mesmo hash ordenado pelo **maior tamanho de arquivo no topo** (ou maior espaço desperdiçado).
   - **Proteção contra Colisão de Hash**: arquivos de tamanhos diferentes são estritamente isolados e nunca agrupados juntos.
   - Regras de marcação automática inteligente:
     - ⭐ *Manter mais recente* (marca os clones mais antigos para remoção).
     - 📅 *Manter mais antigo* (marca as cópias recentes para remoção).
     - Seleção manual por checkbox com somatório em tempo real do espaço a ser liberado.

6. **Envio Seguro para a Lixeira do Windows**:
   - Integração com a API Win32 nativa `SHFileOperationW` (`FOF_ALLOWUNDO`).
   - Os arquivos marcados são movidos para a **Lixeira oficial do Windows**, podendo ser restaurados a qualquer momento pelo usuário.

7. **Monitor do Sistema Operacional em Tempo Real (Hooks do Windows)**:
   - Utiliza `ReadDirectoryChangesW` (`fsnotify`) nos discos escaneados.
   - Detecta criações, modificações, renomeações e exclusões de arquivos em segundo plano.
   - Atualiza a árvore em memória e o índice de hashes instantaneamente sem necessidade de re-escanear todo o disco.
   - Feed visual de logs de eventos com badges coloridos (`CREATE`, `WRITE`, `REMOVE`, `RENAME`).

---

## 🛠️ Como Rodar

### Opção 1: Executar o Binário Pré-compilado (Recomendado)

Basta dar dois cliques em `scanfile.exe` ou executar no terminal:

```powershell
.\scanfile.exe
```

O aplicativo iniciará o motor Go em segundo plano e abrirá a janela nativa do Windows automaticamente.

---

### Opção 2: Compilar a partir do Código Fonte

Requisitos: **Go 1.22+** instalado.

```powershell
# 1. Compilar o executável
go build -o scanfile.exe main.go

# 2. Executar
.\scanfile.exe
```

---

## 🧪 Executando os Testes Automatizados

Para rodar todos os testes unitários de scanner, hasher e indexer:

```powershell
go test -v ./...
```

---

## 📁 Estrutura do Projeto

```
ScanFile/
├── main.go                       # Ponto de entrada, embutimento de assets e inicialização de janela nativa
├── go.mod                        # Módulo Go e dependências
├── go.sum                        # Checksums de dependências
├── scanfile.exe                  # Executável nativo compilado (9.8 MB autônomo)
├── pkg/
│   ├── drives/                   # Detecção de drives Win32 (GetLogicalDriveStringsW, GetDiskFreeSpaceEx)
│   │   └── drives_windows.go
│   ├── scanner/                  # Motor de varredura multithread (Fase 1) e árvore de diretórios
│   │   ├── types.go
│   │   ├── tree.go
│   │   ├── tree_test.go
│   │   └── scanner.go
│   ├── hasher/                   # Motor de cálculo de Hash multithread (Fase 2)
│   │   ├── hasher.go
│   │   └── hasher_test.go
│   ├── indexer/                  # Agrupador de duplicados, ordenação por tamanho e tratamento de colisão
│   │   ├── duplicate_index.go
│   │   └── duplicate_index_test.go
│   ├── watcher/                  # Ganchos de monitoramento do Windows (ReadDirectoryChangesW / fsnotify)
│   │   └── watcher_windows.go
│   ├── recycle/                  # Integração com a Lixeira do Windows (SHFileOperationW)
│   │   └── recycle_windows.go
│   └── server/                   # Servidor de API REST e SSE (Server-Sent Events) em tempo real
│       └── server.go
└── ui/                           # Interface Web moderna embutida
    ├── index.html                # Layout com abas, cards de discos, HUD e exploradores
    ├── css/
    │   └── styles.css            # Dark Theme, Glassmorphism, medidores de tamanho e animações
    └── js/
        └── app.js                # Lógica reativa do cliente, SSE em tempo real, árvore e filtros
```

---

## 💡 Casos de Uso e Exemplo Prático

1. **Liberar Espaço Rápido**:
   - Selecione seus drives (ex: `C:\` e `D:\`).
   - Clique em **"Iniciar Varredura Multithread"**.
   - Na aba **Duplicados por Hash**, os maiores arquivos duplicados (ex: vídeos de 10 GB repetidos) aparecerão no topo.
   - Clique em **"⭐ Manter +Recente"** e depois em **"Mandar para Lixeira do Windows"**.

2. **Explorar Pastas Pesadas**:
   - Acesse a aba **Explorador em Árvore** e veja instantaneamente quais pastas ocupam a maior porcentagem do seu disco com barras visuais.

3. **Monitoramento Contínuo**:
   - Deixe o aplicativo aberto; a aba **Monitor do SO** continuará recebendo eventos do Windows e manterá a memória 100% atualizada em tempo real.

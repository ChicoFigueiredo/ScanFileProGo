# ScanFile Pro 🚀

> **Analisador de Espaço em Disco de Alta Performance, Gráfico da Estrutura (Cushion Treemap), Deduplicação Inteligente por Hash e Monitoramento do SO em Tempo Real para Windows (100% em Go).**

---

## 📚 Documentação Técnica Completa

Para detalhes aprofundados sobre o design, decisões de arquitetura e organização do código, consulte a pasta [`docs/`](docs/):

- 📄 **[Arquitetura do Sistema (`docs/ARQUITETURA.md`)](docs/ARQUITETURA.md)**: Explicação detalhada sobre autocontenção do binário, carregamento de assets HTML em memória RAM (sem pastas temporárias em disco), comunicação via Server-Sent Events (SSE), motor de Smart Hashing, tokens de segurança Win32 (`SeBackupPrivilege`) e proteção anti-zumbi.
- 📄 **[Estrutura do Projeto (`docs/ESTRUTURA_DO_PROJETO.md`)](docs/ESTRUTURA_DO_PROJETO.md)**: Mapeamento de todos os pacotes Go (`pkg/`), arquivos, componentes frontend e catálogo de endpoints REST e SSE da API.

---

## ✨ Principais Funcionalidades

### 1. 📊 Gráfico da Estrutura (Cushion Treemap Interativo estilo TreeSize)
- **Algoritmo Squarified Treemap**: Renderização ultra-rápida em **HTML5 Canvas 2D** com proporção de aspecto otimizada (Bruls-Huizing-van Wijk) e sombreamento volumétrico (*cushion gradient shading*).
- **Visualização Dividida (Lado a Lado)**:
  - 🌓 **Dividido**: Tabela hierárquica detalhada à esquerda + Gráfico Treemap à direita.
  - 📊 **Apenas Gráfico**: Treemap em tela cheia para exploração visual imersiva.
  - 📁 **Apenas Tabela**: Tabela completa com colunas de tamanho, alocado, arquivos, pastas e datas.
- **Navegação Fluida & Drill-Down (Zoom In / Out)**:
  - **Duplo-clique em qualquer bloco**: Faz Zoom-In instantâneo na pasta clicada com recálculo dos subblocos.
  - **Clique simples**: Destaca a borda do bloco e rola a tabela automaticamente até a linha correspondente.
  - **Botão "Subir Nível" & Breadcrumbs**: Retorne facilmente a qualquer nível superior ou para "Meus Discos".
- **Controle Dinâmico de Profundidade (Slider de 2 a 20 Níveis)**: Ajuste em tempo real da profundidade da árvore exibida no gráfico.
- **3 Modos de Cores Customizáveis**:
  - 🎨 **Por Tipo de Arquivo**: Vídeos (azul), Áudio (amarelo), Imagens (verde), Compactados (laranja), Executáveis (rosa), Documentos (ciano), Código/DB (roxo) e Pastas.
  - 🌈 **Por Nível / Profundidade**: Nível 0, Nível 1, Nível 2... com **Régua de Níveis** no rodapé (estilo TreeSize clássico).
  - 🔥 **Por Idade / Inatividade**: Gradiente térmico de verde (<30 dias) a vermelho (>5 anos).
- **Tooltip Flutuante & Menu de Contexto (Botão Direito)**:
  - Hover rico com caminho completo, tamanho, porcentagem, contagens e datas.
  - Menu no botão direito: *Entrar na Pasta*, *Subir Nível*, *Copiar Caminho* e *Mandar para a Lixeira do Windows*.

---

### 2. ⚡ Scanner Multithread Paralelo & Smart Hashing
- **Varredura em Duas Fases**:
  - **Fase 1 (Metadados em RAM)**: Mapeia milhões de arquivos na velocidade máxima de I/O do disco com workers paralelos.
  - **Fase 2 (Smart Progressive Hashing)**:
    1. *Filtro de Tamanho Único*: Arquivos com tamanho exclusivo em bytes nunca são lidos do disco (0% I/O desperdiçado).
    2. *Smart Pre-Hash (4 KB)*: Amostra rápida dos primeiros 4 KB.
    3. *Smart Mid-Hash (4 KB)*: Amostra dos 4 KB do final do arquivo.
    4. *Full-Hash*: Hash completo de 100% dos dados apenas em colisões reais.
- **Múltiplos Algoritmos de Hash**: **xxHash** (ultra rápido, >5 GB/s por núcleo), **BLAKE3** (alta performance criptográfica), **MD5** e **SHA-256**.

---

### 3. 🛡️ Modo Administrador com Redirecionamento IPC e Proteção Anti-Zumbi
- **Execução Privilegiada (`--admin`)**:
  - Habilita o token **`SeBackupPrivilege`** do Windows para contornar permissões de NTFS (*Access Control Lists*) e ler pastas de sistema protegidas (ex: `System Volume Information`, `WindowsApps`, perfis de outros usuários).
- **Processo Invisível com Saída no Terminal Original**:
  - O processo elevado roda em segundo plano com a flag `SW_HIDE` (sem abrir uma segunda janela de console preta).
  - Um canal IPC local via TCP transmite todos os logs e saídas em tempo real diretamente para o seu terminal original.
- **Proteção Anti-Zumbi (Parent Process PID Watcher)**:
  - O processo filho monitora o PID do processo pai via `windows.WaitForSingleObject`.
  - Se o terminal pai for fechado, receber `Ctrl+C` ou for finalizado pelo Gerenciador de Tarefas, o processo elevado é **encerrado instantaneamente**, garantindo zero processos órfãos na memória.

---

### 4. 💾 Configurações Persistentes (`scanfile_config.json`)
- Todas as suas escolhas são gravadas automaticamente em arquivo JSON:
  - Discos selecionados para varredura.
  - Threads de CPU, algoritmos de hash e limites de tamanho.
  - Profundidade do treemap, esquemas de cores e modo de visualização.
  - Nível de zoom da interface.
  - Filtros de ordenação de duplicatas e arquivos ociosos.
- Ao abrir o aplicativo, seu ambiente é restaurado exatamente como você deixou.

---

### 5. 🖥️ Layout Full-Width & Controle de Zoom da UI
- **100% da Largura da Tela**: Layout fluido otimizado para monitores Ultrawide, 1080p, 1440p e 4K.
- **Controles de Zoom no Cabeçalho**:
  - `➖` Diminuir Zoom (passo de 5%).
  - `100%` Indicador de Zoom (clique para redefinir instantaneamente).
  - `➕` Aumentar Zoom (passo de 5%).
  - **Atalhos de Teclado**: `Ctrl +`, `Ctrl -` e `Ctrl 0`.
  - O Treemap se recalcula e reescala automaticamente em qualquer nível de zoom.

---

### 6. 📁 Comparador de Pastas & Arquivos Ociosos
- **Comparador de Pastas**:
  - Detecta pastas inteiras clonadas/duplicadas e calcula o total de espaço desperdiçado.
  - Comparação lado a lado entre dois diretórios com visualização de arquivos idênticos, modificados e exclusivos.
- **Localizador de Arquivos Ociosos**:
  - Identifica arquivos sem modificação há mais de 1, 2, 3 ou 5 anos com filtros de tamanho mínimo configuráveis.

---

### 7. 🗑️ Lixeira Oficial do Windows & Snapshots de Cache
- **Lixeira do Windows Segura**:
  - Integração com a API Win32 `SHFileOperationW` (`FOF_ALLOWUNDO`), permitindo restaurar qualquer arquivo excluído acidentalmente direto da Lixeira do sistema.
- **Snapshots de Cache em Disco**:
  - Salve o estado completo da árvore e hashes em disco (`.scanfile.cache.json.gz`) para consultas instantâneas no futuro sem precisar reescanear o disco.

---

### 8. 📡 Monitor do SO em Tempo Real (Hooks do Windows)
- Monitora os discos ativos via `ReadDirectoryChangesW` (`fsnotify`), atualizando a árvore e os hashes na memória em tempo real quando arquivos são criados, modificados, renomeados ou excluídos.

---

## 🚀 Como Executar

### 1. Execução Padrão (Interface Gráfica)
```powershell
.\scanfile.exe
```

### 2. Execução como Administrador (Acesso Total / SeBackupPrivilege)
```powershell
.\scanfile.exe --admin
```

### 3. Execução com Gravação de Logs em Disco (`logs/`)
```powershell
.\scanfile.exe --log
```
ou combinando Administrador e Logs:
```powershell
.\scanfile.exe --admin --log
```

### 4. Modo Headless (Apenas Servidor Backend / Sem Janela)
```powershell
.\scanfile.exe --no-window --port=8080
```

---

## 🛠️ Compilação do Código Fonte

Requisitos: **Go 1.22+** no Windows.

### Usando o Script PowerShell de Compilação Automatizada:
```powershell
powershell -ExecutionPolicy Bypass -File .\build.ps1
```
*(Executa análise estática com `go vet`, roda os testes unitários automatizados e compila o `scanfile.exe`).*

### Usando o Script Batch Tradicional:
```cmd
build.bat
```

### Compilação Manual via Go CLI:
```powershell
go build -ldflags="-s -w" -o scanfile.exe main.go
```

---

## 🧪 Executando os Testes Unitários

Para rodar todos os testes automatizados dos pacotes (`pkg/config`, `pkg/hasher`, `pkg/indexer`, `pkg/scanner`):

```powershell
go test -v ./...
```

---

## 📜 Licença

Distribuído sob licença MIT. Sinta-se livre para usar, modificar e distribuir.

# Arquitetura do ScanFile Pro

Como o programa funciona por dentro. Os termos em maiúscula estão definidos em [`CONTEXT.md`](../CONTEXT.md). O mapa de arquivos, pacotes e endpoints está em [`ESTRUTURA_DO_PROJETO.md`](ESTRUTURA_DO_PROJETO.md). A visão de produto está no [`README.md`](../README.md).

## 1. Forma do executável

`scanfile.exe` é um binário estático em Go, sem dependência de .NET, Node.js, Python, Java ou Electron, e sem DLL de terceiros. A compilação de release usa `CGO_ENABLED=0`. Copiar o arquivo para outra máquina Windows é suficiente para executá-lo.

A interface é embutida na compilação com `//go:embed ui/*`: `index.html`, `css/styles.css`, `js/app.js`, `js/core.js` e as duas fontes `.woff2` viram dados na seção somente leitura do executável. O servidor os entrega da memória do processo via `io/fs` e `http.FS`. **A interface nunca é extraída para disco.**

Isso não significa que o programa não use nenhuma pasta temporária. Duas ressalvas:

- A **Janela** é um Microsoft Edge em modo aplicativo, iniciado com um perfil dedicado em `%TEMP%\ScanFile_Webview_Profile`. O perfil é do Edge, não da interface, e serve para que uma segunda abertura reaproveite a mesma Janela.
- `instance.json`, com porta, PID e token da instância viva, fica em `%LOCALAPPDATA%\ScanFile\` (ou, na ausência dessa variável, no diretório de configuração do usuário e por último em `%TEMP%\ScanFile`).

Snapshots e Autosave vão para `saved_scans/`, e os logs para `logs/`, ambos ao lado do executável.

## 2. Visão geral

```
+--------------------------------------------------------------------------+
|                    SCANFILE PRO (processo único, .exe)                    |
|                                                                           |
|  +---------------------------------------------------------------------+  |
|  |                        CAMADA DE APRESENTAÇÃO                       |  |
|  |  Janela: Microsoft Edge em modo aplicativo (--app=http://127.0.0.1) |  |
|  |  ui/index.html + ui/css/styles.css + ui/js/app.js + ui/js/core.js   |  |
|  |  Treemap squarified em Canvas 2D                                    |  |
|  +---------------------------------------------------------------------+  |
|             ^                                          ^                  |
|             | REST/JSON com token da Sessão            | SSE /api/events  |
|             | (X-ScanFile-Token, sem CORS)             | (4 a 5 msg/s)    |
|             v                                          |                  |
|  +---------------------------------------------------------------------+  |
|  |                       CAMADA DE BACKEND                             |  |
|  |                                                                     |  |
|  |  [ pkg/server ]  HTTP em 127.0.0.1:47321, Sessão, rotas, SSE,       |  |
|  |                  ciclo de vida, relógio do Autosave                 |  |
|  |        |                                                            |  |
|  |        +--> [ pkg/scanner ]   Fase 1, árvore em memória, Snapshot,  |  |
|  |        |                      Itens Pulados, log de erros           |  |
|  |        +--> [ pkg/hasher ]    Fase 2: Pré-hash e Hash Completo      |  |
|  |        +--> [ pkg/indexer ]   duplicados, Pastas Clones, ociosos    |  |
|  |        +--> [ pkg/watcher ]   Monitoramento recursivo com           |  |
|  |        |                      coalescência e hash em segundo plano  |  |
|  |        +--> [ pkg/recycle ]   Reciclagem, Exclusão, Pasta Protegida |  |
|  |        +--> [ pkg/privileges ] UAC, privilégios, IPC, guarda de PID |  |
|  |        +--> [ pkg/config ]    Configuração atômica, segredo DPAPI   |  |
|  |        +--> [ pkg/drives ]    volumes, capacidade, detecção de WSL  |  |
|  |        +--> [ pkg/ai ]        Provedores e Comandos Rápidos         |  |
|  |        +--> [ pkg/mcp ]       ferramentas, Propostas, escopo        |  |
|  |                                                                     |  |
|  |  main.go --mcp -----------> [ pkg/mcp ] servidor stdio, sem HTTP    |  |
|  +---------------------------------------------------------------------+  |
+--------------------------------------------------------------------------+
```

`pkg/mcp` é usado de dois jeitos: dentro do servidor HTTP, como motor de ferramentas do Assistente, e sozinho no modo `--mcp`, falando por stdio com um cliente externo, sem nenhum servidor HTTP no ar.

## 3. Sessão

A **Sessão** é o vínculo autenticado entre a Janela e o servidor local. Sem ela, qualquer página web aberta no computador poderia descobrir a porta e mandar apagar arquivos.

Como funciona (`pkg/server/auth.go` e `pkg/server/static.go`):

1. Na primeira leitura, o servidor gera 32 bytes de `crypto/rand` e os grava em hexadecimal. Esse é o token da Sessão, válido enquanto o processo viver.
2. `index.html` traz o literal `{{SCANFILE_TOKEN}}`. O servidor renderiza a página à mão e troca o literal pelo token. A página vai com `Cache-Control: no-store`, para que uma cópia velha nunca carregue o token de outra execução. Todo o resto dos assets segue pelo `http.FileServer` normal.
3. Toda chamada a `/api/*` precisa apresentar o token no cabeçalho `X-ScanFile-Token`. Dois caminhos aceitam o token na query, porque o navegador não permite cabeçalhos neles: `/api/events` (`EventSource`) e `/api/ui/closed` (`sendBeacon`).
4. A comparação usa `subtle.ConstantTimeCompare`, então um token errado não revela por onde errou. Sem token ou com token errado: **401** com corpo `{"error":"unauthorized"}`.
5. Requisição com cabeçalho `Origin` que não seja o próprio host: **403**. O valor `null` também é recusado.
6. **Nenhum cabeçalho CORS é emitido em nenhuma resposta.** A interface é servida por este mesmo servidor, então não existe caso legítimo de origem cruzada.
7. Uma única rota é isenta de token: `GET /api/instance`, que devolve `{app, version, pid}` e nada mais. É o que permite a uma segunda execução reconhecer a instância viva antes de ter qualquer credencial.

No handoff de elevação, o filho elevado adota o token do pai lido de `instance.json` (`SetSessionToken`), então a Janela já aberta continua funcionando sem recarregar a página.

## 4. Ciclo de vida do processo

O objetivo é que não sobre processo órfão nem instância duplicada. Quatro mecanismos, em `pkg/server/lifecycle.go` e `main.go`.

### 4.1 Instância única e porta fixa

O servidor escuta em `127.0.0.1:47321` por padrão (`config.DefaultServerPort`, ajustável em `serverPort` na Configuração e por `--port`). A porta fixa resolve dois problemas antigos: o `localStorage` da interface deixa de ser invalidado a cada execução, e o perfil do Edge deixa de acumular zoom por origem.

Na partida, `main.go` lê `instance.json` e sonda a porta anotada. Se a resposta for de outra instância nossa, esta execução pede o foco da Janela (`POST /api/instance/focus`) e encerra. Se o arquivo estiver obsoleto — a instância anterior morreu sem limpar — ele é ignorado.

Se a porta estiver ocupada por um processo que não é nosso, o servidor cai numa porta livre e avisa no console. `GET /api/system/info` sempre informa a porta realmente em uso.

`instance.json` é gravado atomicamente, com modo `0600` onde o sistema respeita permissões POSIX, e removido no `Stop`.

### 4.2 Presença da Janela

A conexão SSE em `/api/events` é o sinal de presença. Um middleware conta as conexões abertas: quando a última cai, um temporizador de **10 segundos** é armado. Na partida a tolerância é maior, **60 segundos**, para dar tempo de o Edge abrir.

Quando o temporizador vence sem nenhuma conexão:

- Sem Varredura em curso, o servidor encerra imediatamente.
- Com Varredura em curso, o desligamento fica **adiado**: o processo continua sem Janela, termina a Varredura, grava o Autosave e só então encerra. O gancho `onScanFinished` dispara o desligamento na hora em que o pipeline acaba, e o temporizador rearmado cobre o caso de o gancho não ser chamado.

`--no-window` desliga o mecanismo inteiro: nesse modo o servidor não encerra por ausência de cliente.

Antes de encerrar, o servidor envia um evento SSE `shutdown` para as Janelas ainda conectadas, e `Stop` derruba nesta ordem: temporizador de presença, `instance.json`, Varredura, Monitoramento, relógio do Autosave, log de erros em disco e por fim o servidor HTTP, com 5 segundos de `Shutdown` gracioso.

### 4.3 Elevação e handoff

`--admin` sem elevação relança o processo pelo UAC. O filho roda com `SW_HIDE`, sem segunda janela de console, e devolve toda a saída ao terminal original por um socket IPC em loopback. O pai espera o retorno do filho por até 120 segundos.

O filho recebe `--parent-pid` e abre um handle com `windows.OpenProcess(SYNCHRONIZE)`, monitorado por `WaitForSingleObject`. Se o pai morrer, por fechamento do terminal, `Ctrl+C` ou Gerenciador de Tarefas, o filho encerra imediatamente. É a guarda anti-zumbi.

Elevar pela interface (`POST /api/system/elevate`) usa um caminho diferente, o **handoff**: a instância atual lança o filho com `--handoff`, o filho lê `instance.json`, adota a mesma porta e o mesmo token, anuncia o handoff e espera o pai liberar o listener (até 120 s, tentando a cada 200 ms). O pai encerra ao ver o PID do filho. A Janela nunca é reaberta e nunca existem duas instâncias vivas.

### 4.4 Tempos limite do HTTP

O servidor **não define `WriteTimeout` global**. Um limite global de escrita mataria a conexão SSE, o download de modelo do Ollama e o streaming do Snapshot. Ficam `ReadHeaderTimeout` de 15 s e `IdleTimeout` de 120 s; onde um handler precisa de prazo de escrita, ele o aplica sozinho via `http.ResponseController`.

## 5. Varredura

### 5.1 Fase 1: metadados

`pkg/scanner` percorre as Raízes Varridas com uma fila de diretórios (`queue.go`) coordenada por `sync.Cond`, consumida por um pool de workers. Cancelar acorda todos os que esperam com `Broadcast`, sem depender de capacidade de canal.

Para cada arquivo são registrados caminho, nome, tamanho lógico, **tamanho alocado em disco** e o atributo de **compressão NTFS** (`GetCompressedFileSizeW` e `FILE_ATTRIBUTE_COMPRESSED`), além das datas de criação, modificação e acesso. A diferença entre lógico e alocado alimenta as métricas de espaço economizado por compressão no status.

Links simbólicos e junções NTFS são identificados por reparse point (`reparse_windows.go`). O programa não os segue por padrão: seguir criaria laços e contagem duplicada.

O número de threads sai de `ResolveWorkers(requested, phase)`:

| Pedido | Fase 1 | Fase 2 |
| :--- | :--- | :--- |
| `0` (Auto) | 2 × núcleos lógicos | 1 × núcleos lógicos |
| `N > 0` | `min(N, 4 × núcleos)` | `min(N, 4 × núcleos)` |

`ThreadOptions()` gera o combo da interface: `0` mais as potências de 2 de 4 até `4 × núcleos`. A interface lê isso de `GET /api/system/info` e nunca chuta valores.

### 5.2 Itens Pulados

Um item pulado sem registro é uma contagem que não fecha. Por isso todo Item Pulado entra no anel de 500 entradas exposto por `GET /api/logs/skipped`, é somado em `skippedCount` no status e é gravado no log de erros da Varredura, com um motivo em pt-BR:

| Motivo | Quando |
| :--- | :--- |
| Arquivo ou pasta interna do sistema Windows | `System Volume Information`, `$Recycle.Bin`, `pagefile.sys`, `hiberfil.sys` e afins |
| Pseudo-sistema de arquivos do WSL | `proc`, `sys`, `dev`, `mnt`, `kcore` — **apenas** dentro de uma Raiz Varrida detectada como WSL |
| Profundidade máxima de pastas atingida | mais de 50 níveis |
| Junção ou link para pasta já mapeada | reparse point cujo alvo já foi visitado |
| Junção aponta para uma pasta ancestral | laço |
| Pseudo-arquivo sem conteúdo real | entrada não regular |

A detecção de WSL é feita uma vez por Raiz Varrida, por caminho UNC (`\\wsl$`, `\\wsl.localhost`) ou por sistema de arquivos 9P. Sem esse recorte, pastas legítimas como `C:\dev` seriam descartadas. Volumes 9P aparecem na lista de discos marcados como WSL e não são recomendados para Varredura.

### 5.3 Log de erros

`DiskErrorLogger` grava em `logs/scan_errors_<data>.log` com escrita bufferizada, `Flush` a cada 32 linhas **e** a cada 2 segundos por um ticker próprio, e `Close` idempotente chamado por `StopBackground`. Sem o ticker e sem o `Close`, o arquivo terminava com o cabeçalho e nenhuma linha.

### 5.4 Cancelamento e concorrência de Varreduras

Cada Varredura tem um `context.Context` próprio, criado no `POST /api/scan/start` e cancelado por `POST /api/scan/cancel`. O cancelamento propaga para a Fase 1, para a Fase 2, para a indexação e para o Autosave: nada continua rodando depois.

A reserva da vaga e a troca de fase acontecem sob o mesmo lock. Um segundo `POST /api/scan/start` durante uma Varredura recebe **409** com `{"error":"scan_in_progress","phase":"..."}`. As fases publicadas no status são `idle`, `phase1_metadata`, `phase2_hashing`, `indexing`, `completed`, `cancelling`, `cancelled`, `loading_cache` e `watching`.

## 6. Hashing

`pkg/hasher` implementa a Fase 2 em três etapas. O objetivo é ler o mínimo de bytes possível.

```
              todos os arquivos da árvore
                          |
                          v
   +-----------------------------------------------+
   | 1. Filtro de tamanho                          |
   |    tamanho exclusivo em bytes -> descartado   |  0 bytes lidos
   |    tamanho < MinSizeForHash   -> descartado   |
   +-----------------------------------------------+
                          | Candidatos a Duplicado
                          v
   +-----------------------------------------------+
   | 2. Pré-hash (xxHash64, sempre)                |
   |    4 KB do início + 4 KB do fim               |  <= 8 KB por arquivo
   |    arquivos <= 8192 B pulam esta etapa        |
   +-----------------------------------------------+
                          | reagrupa por (tamanho, Pré-hash)
                          | grupos com >= 2 sobreviventes
                          v
   +-----------------------------------------------+
   | 3. Hash Completo                              |
   |    xxHash64 | BLAKE3 | MD5 | SHA-256          |  100% do arquivo
   |    leitura sequencial em buffers de 1 MiB     |
   +-----------------------------------------------+
```

Detalhes que importam:

- O **Pré-hash é sempre xxHash64**, independentemente do algoritmo escolhido para o Hash Completo, e **nunca é usado como prova de igualdade**. Ele só descarta colisões de tamanho.
- Arquivos com até 8192 bytes vão direto ao Hash Completo: as duas pontas de 4 KB já cobririam o arquivo inteiro, então o Pré-hash não economizaria leitura.
- Os buffers de 1 MiB vêm de um `sync.Pool` compartilhado. Sem o pool, cada worker alocava o seu e a memória crescia com o número de threads.
- O hash gravado carrega o prefixo do algoritmo (`xxh64:`, `blake3:`, `md5:`, `sha256:`). `HashMatchesAlgorithm` usa esse prefixo para o **Quick Scan** recusar um hash de outro algoritmo em vez de misturá-lo nos grupos. Um hash de algoritmo diferente é apagado, não reaproveitado.
- Um arquivo bloqueado por outro processo vira uma entrada de erro e não aborta a Fase 2. Os arquivos são fechados em todos os caminhos.
- O progresso é publicado por um ticker de 200 ms, com os workers ativos, o arquivo corrente de cada um, os bytes lidos, os bytes cobertos por Hash Completo e quantos candidatos o Pré-hash eliminou.
- Uma segunda chamada a `RunHashing` no mesmo `Hasher` devolve `ErrHashingInProgress`, garantida por um `CompareAndSwap`.

## 7. Índices

`pkg/indexer` mantém três índices sob `sync.RWMutex`:

- **Duplicados** (`duplicate_index.go`): agrupa por (Hash Completo, tamanho).
- **Pastas Clones** (`folder_index.go`): hash de conteúdo por pasta, derivado dos arquivos e das subpastas. Cada grupo carrega um **nível de confiança**: `hash` quando toda a comparação se apoiou em Hash Completo, `size_mtime` quando faltou hash em algum arquivo e a comparação caiu em tamanho e data de modificação. A distinção existe para que uma pasta nunca seja anunciada como idêntica com base só em metadados.
- **Arquivos Ociosos** (`idle_index.go`): consulta em streaming sobre a árvore, com filtro de idade, tamanho, extensão e busca, ordenação e paginação.

Os índices de pastas são reconstruídos apenas quando o Monitoramento os marca sujos (`RebuildIfDirty`). Uma consulta que devolve zero grupos não é sinal de índice velho: reconstruir por causa disso varria a árvore inteira a cada filtro.

## 8. Monitoramento

`pkg/watcher` observa as Raízes Varridas depois de uma Varredura Completa. O pacote separa duas responsabilidades: a parte específica do sistema só informa **quais caminhos mudaram**; toda a decisão fica na parte portátil.

### 8.1 Assinatura recursiva

No Windows, `watcher_windows.go` abre uma assinatura `ReadDirectoryChangesW` por Raiz Varrida com `bWatchSubtree = true`. É a raiz inteira, não só o primeiro nível.

Fora do Windows, `watcher_other.go` usa `fsnotify`, que não tem modo recursivo, e registra cada diretório da árvore manualmente.

### 8.2 Coalescência

Cada notificação bruta entra numa tabela de pendências por caminho. Um caminho só é despachado depois de **2 segundos de silêncio**. Um arquivo escrito 50 vezes seguidas é processado uma vez.

Uma válvula de segurança evita a inanição do caminho que nunca fica em silêncio: passados `2 s × 15`, com piso de 30 segundos, o caminho é despachado mesmo continuando a mudar. O piso existe porque um arquivo ainda em escrita não vale o hash: cortar a rajada cedo demais desperdiça leitura.

### 8.3 Hash fora do laço de eventos

Despachar não significa ler. O caminho entra numa fila limitada a 4096 entradas, consumida por **2 workers** de segundo plano que fazem o hash e atualizam a árvore e os índices. O laço de eventos nunca faz I/O de conteúdo: se fizesse, uma cópia grande encheria o buffer de notificação do Windows e o programa perderia eventos.

### 8.4 Estouro do buffer

Uma cópia muito grande pode estourar o buffer do sistema mesmo assim. Nesse caso o `OnOverflow` da raiz afetada dispara uma revarredura só daquela raiz, o status volta para `watching` quando termina e a interface é informada pelo texto do status. O programa nunca fica com uma árvore silenciosamente desatualizada.

## 9. Persistência: Snapshot e Autosave

### 9.1 Formato

Snapshot e Autosave usam o mesmo formato: JSON comprimido com gzip, versão 2 do cabeçalho. As extensões são `.sfz` para o Autosave e `.scanfile.gz` para o Snapshot salvo pelo usuário; a importação também aceita `.scanfile`, `.json.gz` e `.json`.

A escrita e a leitura são **em streaming, arquivo por arquivo**. Nem a gravação bufferiza o documento inteiro, nem a importação devolve a lista de arquivos à memória. `ImportCache` não preenche mais `Files` nem `Directories`: quem quiser percorrer arquivos usa o `TreeManager` reconstruído.

`POST /api/cache/load` e `POST /api/cache/autosave/restore` devolvem um **resumo** (`CacheSnapshotSummary`): raízes, contagens, bytes, algoritmo e data. Antes, restaurar devolvia centenas de megabytes de JSON à interface.

### 9.2 Um único relógio

O Autosave tem **um só ticker no processo**, com passo de 30 segundos, que apenas decide o que já venceu. Quem define o intervalo é a fase corrente:

| Situação | Intervalo | Condição |
| :--- | :--- | :--- |
| Durante a Varredura | `autoSaveIntervalMinutes` da Configuração, 5 min por padrão | sempre |
| Ao concluir a Varredura | imediato | sempre |
| Com Monitoramento ativo | 10 min | **só se o contador de mudanças andou** |
| Durante ou após um Cancelamento | nunca grava | — |

`ensureAutoSaveLoop` chamado de novo com outra Configuração troca a Configuração, nunca cria um segundo relógio. Antes, cada Varredura deixava uma goroutine de Autosave vazando.

O sinal de mudança soma o contador do `TreeManager` com o do Monitoramento, porque as atualizações incrementais do watcher não avançam o contador da árvore.

A gravação é atômica: `autosave_temp.sfz` no mesmo diretório e depois rotação para `autosave_latest.sfz`, com o anterior virando `autosave_previous.sfz`.

### 9.3 Quick Scan

O índice de reaproveitamento do Quick Scan sai da árvore em memória quando ela existe, e do último Autosave lido em streaming quando não existe. Em nenhum dos dois caminhos a lista de arquivos do Snapshot é materializada. Um arquivo é reaproveitado quando caminho, tamanho e data de modificação batem **e** o hash gravado foi calculado com o algoritmo atual.

## 10. Assistente e MCP

### 10.1 Provedores

`pkg/ai` define três Provedores: Ollama local, OpenRouter na nuvem e **Comandos Rápidos**, um roteador por palavras-chave que aciona as ferramentas sem nenhum modelo de linguagem. Comandos Rápidos não é um provedor de inferência e é apresentado exatamente assim na interface — o nome antigo, "Direto (In-Process)", sugeria um modelo local que nunca existiu. O identificador `direct` gravado por versões anteriores continua sendo aceito e é migrado para `quick`.

O catálogo local é limitado a cerca de 14 GB, com selo de visão e de ferramentas por modelo. Um modelo sem visão perde as ferramentas de imagem, com aviso.

### 10.2 Aprovação em duas fases

Esta é a regra mais importante do Assistente: **o modelo propõe, a pessoa executa**.

**Fase 1 — proposta.** A ferramenta `propose_actions` nunca toca o disco. O argumento `dry_run` enviado pelo modelo é **ignorado**: a Proposta é registrada sempre pendente, com `dryRun: true` na resposta e validade de 30 minutos. Já nessa fase todo caminho é validado contra as Raízes Varridas, e um MOVE sem destino ou com destino fora das raízes é recusado.

**Fase 2 — aprovação.** A interface mostra a Proposta com o tipo de ação, a contagem de arquivos e o tamanho total. Só quando a pessoa aprova é que o servidor chama `ExecuteProposal`, por `POST /api/ai/actions/execute`. As execuções são serializadas, e o trabalho de disco acontece fora do lock que protege a tabela de Propostas, para a interface continuar lendo.

Uma Proposta aprovada não é um atalho: ela passa pelas mesmas regras das ações manuais — Pasta Protegida, escopo das Raízes Varridas e verificação prévia da Lixeira.

Não existe caminho pelo qual uma chamada de ferramenta vinda do modelo alcance `ExecuteProposal`. Um PDF com instrução embutida, no máximo, faz o modelo criar uma Proposta que ficará parada até alguém aprovar.

### 10.3 Escopo de leitura

Todas as ferramentas que leem arquivo passam por `ensurePathAllowed`, que exige que o caminho esteja dentro de uma Raiz Varrida. Sem raízes carregadas a ferramenta recusa com `ErrNoAllowedRoots`, em vez de ler o disco inteiro. As raízes são atualizadas a cada Varredura e a cada Snapshot carregado.

### 10.4 Modo MCP

`scanfile.exe --mcp` não sobe servidor HTTP. Ele carrega `saved_scans/autosave_latest.sfz` (ou `autosave_previous.sfz`), reconstrói os índices de duplicados e de pastas, adota as Raízes Varridas do Snapshot e serve as ferramentas por stdio. Sem Autosave o processo termina com erro em vez de subir um servidor cujas ferramentas recusariam tudo.

O servidor MCP é criado com `server.WithRecovery()`: um panic dentro de uma ferramenta não derruba o processo inteiro.

## 11. Configuração e segredos

`pkg/config` grava `scanfile_config.json` de forma **atômica e síncrona**: arquivo temporário no mesmo diretório, `Write`, `Sync`, `Close` e `Rename` por cima do destino. Não há gravação assíncrona: a função só retorna quando o arquivo está no disco.

O caminho é resolvido uma vez por processo: o diretório de trabalho quando já existe um `scanfile_config.json` lá, senão ao lado do executável.

`POST /api/config` aceita um **documento parcial**. `MergeJSON` aplica só as chaves presentes no corpo, o que impede que uma tela salvando o zoom apague as opções do Assistente.

A chave do OpenRouter tem tratamento próprio:

- É gravada protegida por **DPAPI** em `aiOpenRouterKeyEnc` (`secret_windows.go`). Fora do Windows o valor é apenas codificado em base64 e o campo `aiOpenRouterKeyPlain` marca isso explicitamente, para que ninguém confunda codificação com proteção.
- `GET /api/config` devolve `Public()`: a chave e o blob protegido são removidos e sobra `hasOpenRouterKey`, um booleano.
- Enviar `aiOpenRouterKey` com valor troca a chave; enviar vazio a remove; **omitir o campo preserva** a que está guardada. Assim a interface pode devolver o documento que recebeu sem destruir o segredo.
- Uma chave gravada em texto puro por versão anterior é migrada para o campo protegido na primeira carga.

## 12. Reciclagem e Exclusão Permanente

`pkg/recycle` separa política de execução.

`IsProtectedPath` é a política, e vale mesmo com o processo elevado. São **Pastas Protegidas**: a raiz de qualquer volume, a pasta `Windows` de qualquer volume e tudo abaixo dela, e qualquer `System Volume Information` em qualquer nível. A comparação normaliza separadores, resolve `.` e `..` e ignora caixa. Um caminho que não seja absoluto com letra de unidade ou UNC também é tratado como protegido: quem chama não consegue provar para onde ele aponta.

`IsWithinRoots` é o escopo: o caminho precisa estar dentro de uma das Raízes Varridas, com comparação em fronteira de separador, para que `C:\Users2` nunca conte como dentro de `C:\Users`.

`Preflight` é a verificação prévia da Lixeira, e é o que impede a Reciclagem de virar Exclusão Permanente por acidente. Ela recusa, com o motivo em pt-BR:

- volume sem Lixeira: rede, CD-ROM, mídia removível formatada sem Lixeira;
- volume cuja Lixeira está configurada para destruir em vez de reciclar (`NukeOnDelete` no registro do volume);
- item maior que a capacidade da Lixeira daquele volume, caso em que o Windows apagaria em vez de reciclar.

`SendToRecycleBin` usa `SHFileOperationW` com `FOF_ALLOWUNDO` e `FOF_WANTNUKEWARNING`, para que o shell ainda avise nos casos que a verificação prévia não cobre.

No servidor, `POST /api/files/recycle` e `POST /api/files/delete` acrescentam duas regras que a biblioteca não tem como checar: o escopo das Raízes Varridas e as confirmações digitadas — o nome da pasta para reciclar uma pasta, e a palavra `EXCLUIR` para a Exclusão Permanente. A resposta traz um item por caminho pedido, na mesma ordem, com `recycled`, `deleted`, `refused` ou `failed` e o motivo.

## 13. Telemetria

A interface recebe atualizações por Server-Sent Events em `GET /api/events`. É um fluxo **orientado a eventos**, não um relógio de vídeo:

| Evento | Quando |
| :--- | :--- |
| `scan_progress` | ticker de 250 ms na Fase 1 e de 200 ms na Fase 2, e a cada troca de fase. 4 a 5 mensagens por segundo. |
| `fs_event` | uma mudança detectada pelo Monitoramento, já coalescida |
| `autosave_done` | um Autosave gravado |
| `shutdown` | o servidor vai encerrar, com o motivo |

Cada cliente SSE tem um canal bufferizado de 100 mensagens. O envio é não bloqueante sob `RLock`: um cliente lento perde mensagens em vez de travar o servidor, e é removido do mapa ao desconectar. É a mesma conexão que serve de sinal de presença da Janela, descrito na seção 4.2.

## 14. Limites em vigor

Os tetos abaixo existem porque a alternativa era estourar a memória do servidor ou da página. Estão todos no código, não são recomendações.

| Limite | Valor | Onde |
| :--- | :--- | :--- |
| Arquivos diretos devolvidos por `/api/tree` | 500 por pasta (50 nos níveis fundos) | `scanner.DefaultSummaryMaxFiles` |
| Profundidade de `/api/tree` | 8 | `handleGetTree` |
| Página de `/api/tree/files` | 100 padrão, 500 máximo | `scanner.MaxFilesPageLimit` |
| Página de duplicados | 50 padrão, 500 máximo | `handleGetDuplicates` |
| Página de Pastas Clones | 100 padrão, 500 máximo | `handleGetFolderDuplicates` |
| Anel de Itens Pulados | 500 entradas | `scanner.maxSkippedRing` |
| Anel de eventos do Monitoramento | 200 entradas | `server.maxRecentLogs` |
| Fila de hash do Monitoramento | 4096 caminhos | `watcher.hashQueueCapacity` |
| Profundidade máxima da Varredura | 50 níveis | `scanner.MaxScanDepth` |
| Threads | 4 × núcleos lógicos | `scanner.MaxThreads` |
| Validade de uma Proposta | 30 minutos | `mcp.ProposalTTL` |
| Corpo JSON das ações de arquivo e do Assistente | 8 MiB | `server.maxRequestBody` |

## 15. O que ainda não está implementado

O **nó compacto em memória** de [`adr/0001-no-compacto-em-memoria.md`](adr/0001-no-compacto-em-memoria.md) — caminho derivado do pai, extensão internada, hash em bytes fixos — ainda não foi feito. É dele que depende a meta de 50 milhões de itens. Enquanto isso, o `FileNode` guarda o caminho completo como string.

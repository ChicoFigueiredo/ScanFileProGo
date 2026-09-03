# ScanFile Pro

Analisador de espaço em disco para Windows, escrito em Go. Varre volumes NTFS, identifica duplicados por conteúdo, pastas clones e arquivos ociosos, e permite liberar espaço com Reciclagem ou Exclusão Permanente.

O programa é um único executável. Ele sobe um servidor HTTP em `127.0.0.1` e abre a interface numa Janela do Microsoft Edge em modo aplicativo. Não há instalador, runtime externo nem serviço em segundo plano.

O vocabulário usado aqui (Varredura, Fase 1, Fase 2, Pré-hash, Hash Completo, Raízes Varridas, Snapshot, Autosave, Monitoramento, Reciclagem, Exclusão Permanente, Pasta Protegida, Proposta, Comandos Rápidos, Janela, Sessão, Item Pulado) está definido em [`CONTEXT.md`](CONTEXT.md).

## Documentação

| Documento | Conteúdo |
| :--- | :--- |
| [`CONTEXT.md`](CONTEXT.md) | Glossário do domínio. É a fonte dos termos usados no código e na interface. |
| [`docs/ARQUITETURA.md`](docs/ARQUITETURA.md) | Como o programa funciona por dentro: Sessão, ciclo de vida do processo, pipeline de Varredura e de hashing, Monitoramento, Autosave, Assistente. |
| [`docs/ESTRUTURA_DO_PROJETO.md`](docs/ESTRUTURA_DO_PROJETO.md) | Árvore de diretórios, tabela dos 11 pacotes Go e catálogo completo dos endpoints. |
| [`docs/DECISOES.md`](docs/DECISOES.md) | As 39 decisões de produto e de engenharia tomadas na avaliação de 2 de setembro de 2026. |
| [`docs/AVALIACAO.md`](docs/AVALIACAO.md) | Relatório de avaliação que originou as correções. Registro histórico. |
| [`docs/adr/0001-no-compacto-em-memoria.md`](docs/adr/0001-no-compacto-em-memoria.md) | Decisão arquitetural sobre o nó compacto em memória. |

## Como executar

```powershell
.\scanfile.exe
```

O programa escuta na porta fixa **47321** e abre a Janela apontando para ela. Se a porta estiver ocupada por outro processo, o servidor cai numa porta livre e registra o aviso no console. Se estiver ocupada por outra instância do próprio ScanFile Pro, esta execução traz a Janela existente para frente e encerra: só existe uma instância viva por vez.

Fechar a Janela encerra o backend. Se houver Varredura em curso, o processo continua sem Janela até terminar, grava o Autosave e só então encerra.

### Flags

| Flag | Efeito |
| :--- | :--- |
| `--version` | Imprime versão, commit e data de build e encerra. |
| `--log` | Grava o log geral em `logs/scanfile_app_<data>.log`, além do console. |
| `--debug` | Log detalhado de requisições HTTP e um retrato de memória e goroutines a cada 10 s, em `logs/scanfile_debug_<data>.log`. |
| `--admin` | Executa como Administrador. Sem elevação, solicita o UAC e relança o processo. |
| `--port=N` | Porta HTTP. `0` (padrão) usa a porta da Configuração, 47321. |
| `--no-window` | Sobe apenas o servidor, sem abrir a Janela. Nesse modo o backend não encerra por ausência de cliente. |
| `--mcp` | Executa o servidor Model Context Protocol por stdio, para Claude Desktop e clientes equivalentes. |

Quatro flags são internas e não devem ser usadas na linha de comando. `--elevated-child`, `--ipc-addr` e `--parent-pid` são passadas pelo processo pai ao filho elevado. `--handoff` é usada quando a instância atual entrega a porta e o token da Sessão ao filho elevado.

Exemplos:

```powershell
.\scanfile.exe --admin --log
.\scanfile.exe --no-window --port=8080
.\scanfile.exe --mcp
```

## Funcionalidades

### Varredura em duas fases

A **Fase 1** percorre as Raízes Varridas e registra metadados: caminho, tamanho lógico, tamanho alocado em disco, atributo de compressão NTFS, datas de criação, modificação e acesso. Não lê conteúdo. O percurso usa uma fila de diretórios com `sync.Cond` e um pool de workers.

A **Fase 2** lê conteúdo apenas dos Candidatos a Duplicado. Depois vem a indexação de duplicados, de Pastas Clones e de Arquivos Ociosos.

O número de threads é escolhido na interface. O combo é derivado do processador: `Auto` mais as potências de 2 de 4 até 4 × núcleos lógicos. Numa máquina com 16 threads lógicos as opções são Auto, 4, 8, 16, 32 e 64. `Auto` resolve para 2 × núcleos na Fase 1, limitada por I/O, e 1 × núcleos na Fase 2, limitada por CPU.

Cancelar aborta o pipeline inteiro: Fase 1, Fase 2, Autosave e índices. Uma segunda Varredura pedida durante uma em curso recebe HTTP 409.

Todo **Item Pulado** é contabilizado no status e registrado com motivo em pt-BR: pasta interna do Windows, pseudo-sistema do WSL, profundidade máxima de 50 níveis, junção para pasta já mapeada, junção apontando para ancestral, pseudo-arquivo sem conteúdo. A heurística do WSL só vale dentro de uma Raiz Varrida detectada como WSL, então `C:\dev` e `D:\sys` não são descartados em silêncio.

### Hashing: Pré-hash e Hash Completo

O gargalo da deduplicação é a leitura. O pipeline reduz bytes lidos em três etapas:

1. **Filtro de tamanho.** Um arquivo com tamanho exclusivo em bytes nas Raízes Varridas nunca é aberto.
2. **Pré-hash.** xxHash64 dos primeiros 4 KB e dos últimos 4 KB do Candidato a Duplicado. Arquivos com até 8192 bytes pulam esta etapa e vão direto ao Hash Completo, porque as duas pontas já cobririam o arquivo inteiro.
3. **Hash Completo.** Só para os arquivos que continuam colidindo em (tamanho, Pré-hash) com pelo menos um outro. Leitura sequencial em buffers de 1 MiB, reciclados num pool compartilhado.

O Pré-hash é sempre xxHash64 e nunca é usado como prova de igualdade: serve só para descartar colisões de tamanho sem ler o arquivo inteiro.

O Hash Completo aceita quatro algoritmos, todos implementados e selecionáveis na interface:

| Algoritmo | Prefixo gravado | Uso |
| :--- | :--- | :--- |
| xxHash64 | `xxh64:` | Padrão. Não criptográfico. |
| BLAKE3 | `blake3:` | Criptográfico. Biblioteca Go pura (`lukechampine.com/blake3`), sem DLL. |
| MD5 | `md5:` | Compatibilidade com listas de hash existentes. |
| SHA-256 | `sha256:` | Criptográfico. |

O prefixo fica gravado na string do hash. É ele que permite ao **Quick Scan** recusar um hash calculado com outro algoritmo em vez de misturá-lo nos grupos.

### Duplicados, Pastas Clones e Arquivos Ociosos

- **Duplicados por hash**: grupos de dois ou mais arquivos com o mesmo Hash Completo e o mesmo tamanho, com o espaço desperdiçado por grupo.
- **Pastas Clones**: deduplicação hierárquica. Uma pasta recebe um hash de conteúdo derivado dos arquivos e das subpastas. Cada grupo declara o **nível de confiança**: `hash` quando todos os arquivos das pastas comparadas tinham Hash Completo, `size_mtime` quando faltou hash em algum e a comparação caiu em tamanho e data. Um grupo com confiança `size_mtime` nunca é apresentado como pasta idêntica.
- **Comparador de pastas**: comparação lado a lado de dois diretórios, com arquivos idênticos, modificados e exclusivos, também com nível de confiança.
- **Arquivos Ociosos**: sem modificação há mais de 6 meses, 1 ano, 2 anos ou 5 anos, com tamanho mínimo configurável.

### Gráfico da Estrutura

Treemap squarified (Bruls, Huizing, van Wijk) em Canvas 2D, com sombreamento de almofada e resolução ajustada ao `devicePixelRatio`.

- Três modos de visualização: dividido, apenas gráfico, apenas tabela.
- Três modos de cor: por tipo de arquivo, por nível de profundidade e por idade.
- Slider de profundidade de **1 a 10 níveis**. A API limita a consulta a 8 níveis.
- Drill-down com duplo clique, breadcrumbs, botão de subir nível e menu de contexto.
- O treemap desenha no máximo **500 arquivos diretos** da pasta atual. A lista completa fica na tabela paginada pelo servidor (`GET /api/tree/files`, 100 itens por padrão, teto de 500).

### Snapshots, Autosave e Quick Scan

- **Snapshot**: cópia da árvore e dos hashes salva pelo usuário sob o nome que escolher, em `saved_scans/`, com extensão `.scanfile.gz`.
- **Autosave**: gravado durante a Varredura, ao concluí-la e, com Monitoramento ativo, a cada 10 minutos apenas quando houve mudança. Fica em `saved_scans/autosave_latest.sfz`, com `autosave_previous.sfz` como rollback. Existe um único relógio de Autosave no processo.
- Os dois formatos são JSON comprimido com gzip, escrito e lido **em streaming**, arquivo por arquivo. Restaurar devolve à interface um resumo (raízes, contagens, algoritmo, data), nunca o documento inteiro.
- **Quick Scan**: reaproveita o hash de um arquivo cujo caminho, tamanho e data de modificação não mudaram desde o último Snapshot, e apenas se o hash gravado foi calculado com o algoritmo atual.

### Monitoramento

Depois de uma Varredura Completa, as Raízes Varridas passam a ser observadas **recursivamente** com `ReadDirectoryChangesW` (`bWatchSubtree`), uma assinatura por raiz.

- Coalescência de 2 segundos por caminho: um arquivo escrito 50 vezes seguidas é processado uma vez. Uma válvula de segurança despacha o caminho depois de 30 segundos mesmo que ele continue mudando, para que um log em crescimento contínuo não fique parado para sempre.
- O hash roda em 2 workers de segundo plano, fora do laço de eventos, numa fila limitada, para que uma rajada de escritas não bloqueie o buffer de notificação do sistema.
- Estouro do buffer do Windows numa cópia grande não perde o estado: a raiz afetada é revarrida e a interface é avisada.
- Fora do Windows há um substituto com `fsnotify`, que registra a árvore de diretórios manualmente porque `fsnotify` não tem modo recursivo.

### Reciclagem, Exclusão Permanente e Pastas Protegidas

- **Reciclagem** usa `SHFileOperationW` com `FOF_ALLOWUNDO`. Antes de qualquer operação roda uma **verificação prévia**: se o volume não tem Lixeira (rede, CD-ROM, mídia removível sem Lixeira), se a Lixeira está configurada para destruir em vez de reciclar, ou se o item é maior que a capacidade da Lixeira do volume, a operação é **recusada com o motivo**. Nunca vira Exclusão Permanente por acidente.
- **Exclusão Permanente** é uma ação separada, e o pedido só é aceito com a palavra `EXCLUIR` digitada.
- Reciclar uma pasta exige digitar o nome da própria pasta na confirmação.
- **Pastas Protegidas** são recusadas mesmo em Modo Administrador: a raiz de qualquer volume, a pasta `Windows` de qualquer volume e tudo abaixo dela, e qualquer `System Volume Information`. Um caminho que não seja absoluto com letra de unidade ou UNC também é recusado.
- Toda Reciclagem e toda Exclusão Permanente só valem **dentro das Raízes Varridas** da Varredura corrente.

### Assistente de IA

O Assistente consulta a árvore varrida, inspeciona conteúdo e formula Propostas. Ele nunca age por conta própria.

Três Provedores:

| Provedor | O que é |
| :--- | :--- |
| Ollama (local) | Modelo rodando na máquina, endpoint padrão `http://127.0.0.1:11434`, modelo padrão `qwen3-vl:8b`. |
| OpenRouter (nuvem) | Modelo na nuvem. A chave é guardada protegida por DPAPI e nunca volta pela API. |
| Comandos Rápidos | Roteador por palavras-chave que aciona as ferramentas sem modelo de linguagem. Não é um provedor de inferência e é apresentado como tal. |

O catálogo local é limitado a cerca de 14 GB, com selo de visão e de ferramentas por modelo: `qwen3-vl:8b`, `qwen2.5vl:7b`, `gemma3:12b`, `qwen3:14b`, `gpt-oss:20b` e `devstral:24b`. Um modelo sem visão perde as ferramentas de imagem, com aviso.

**Aprovação humana é obrigatória.** A ferramenta `propose_actions` nunca toca o disco: o argumento `dry_run` enviado pelo modelo é ignorado e a Proposta nasce sempre pendente, com validade de 30 minutos. A execução só acontece por `POST /api/ai/actions/execute`, que a interface chama depois que a pessoa aprova. A Proposta aprovada passa pelas mesmas regras de Pasta Protegida, escopo e verificação prévia da Lixeira das ações manuais.

O Assistente **lê arquivos apenas dentro das Raízes Varridas**. Sem raízes carregadas, toda ferramenta que lê arquivo recusa.

### Servidor MCP

`scanfile.exe --mcp` sobe um servidor Model Context Protocol por stdio. Ele carrega o último Autosave (`autosave_latest.sfz`, com `autosave_previous.sfz` como alternativa), reconstrói os índices de duplicados e de pastas e adota as Raízes Varridas do Snapshot como escopo permitido. Sem Autosave o servidor não sobe: sem raízes, todas as ferramentas recusariam.

Seis ferramentas: `classify_files`, `analyze_file_content`, `analyze_image_visual`, `compare_visual_similarity`, `write_file_metadata` e `propose_actions`. Um panic dentro de uma ferramenta não derruba o servidor.

### Modo Administrador

Com `--admin`, o processo habilita seis privilégios do token do Windows: `SeBackupPrivilege`, `SeRestorePrivilege`, `SeSecurityPrivilege`, `SeTakeOwnershipPrivilege`, `SeIncreaseBasePriorityPrivilege` e `SeProfileSingleProcessPrivilege`. Isso permite ler pastas protegidas por ACL sem erro de acesso negado.

A partir de um terminal comum, a instância original dispara a instância elevada com `SW_HIDE` e recebe de volta toda a saída por um canal IPC em loopback, no terminal original. O filho monitora o PID do pai com `WaitForSingleObject` e encerra junto com ele. A espera pelo UAC tem limite de 120 segundos.

Elevar pela interface usa handoff: a instância atual entrega a porta e o token da Sessão ao filho elevado e encerra. Nunca existem duas instâncias vivas.

### Interface

- 12 temas: quatro ocres, quatro claros e quatro escuros.
- Zoom de 5% em 5%, com `Ctrl +`, `Ctrl -` e `Ctrl 0`. O treemap se recalcula em qualquer nível.
- Oito abas: Discos e Varredura, Gráfico e Árvore, Duplicados por Hash, Comparador de Pastas, Distribuição, Arquivos Ociosos, Monitor do SO e Assistente IA.
- Paginação pelo servidor em todas as listas longas.
- Barra de memória no cabeçalho, com o consumo do processo e o da máquina.
- Ícones SVG escritos direto no `index.html`, sem arquivos de ícone.
- Fontes Inter e JetBrains Mono embutidas no binário. Nenhuma requisição externa na inicialização.
- Telemetria por Server-Sent Events em `/api/events`, orientada a eventos: a Fase 1 emite um retrato a cada 250 ms e a Fase 2 a cada 200 ms, ou seja 4 a 5 atualizações por segundo, mais os eventos avulsos de Monitoramento, Autosave e desligamento. Não há polling.

### Segurança

- O servidor escuta apenas em `127.0.0.1`.
- Toda chamada a `/api/*` exige o token da **Sessão**, gerado com 32 bytes de `crypto/rand` a cada execução e injetado no `index.html` servido. O token vai no cabeçalho `X-ScanFile-Token`. Só `/api/events` e `/api/ui/closed` o aceitam na query, porque `EventSource` e `sendBeacon` não permitem cabeçalhos. A comparação é em tempo constante.
- **Não há CORS.** Nenhum cabeçalho `Access-Control-Allow-*` é emitido. Uma requisição com `Origin` de outra origem recebe 403.
- Única exceção ao token: `GET /api/instance`, que devolve só `{app, version, pid}` e serve para uma segunda execução reconhecer a instância viva.
- A chave do OpenRouter é gravada protegida por DPAPI. `GET /api/config` informa apenas se existe uma chave configurada, nunca o valor. Fora do Windows a chave fica só codificada em base64, e o arquivo marca isso explicitamente.
- Salvar uma preferência de uma tela não apaga as opções de outra: `POST /api/config` aceita um documento parcial e altera apenas as chaves presentes.

## Configuração

As preferências ficam em `scanfile_config.json`, no diretório de trabalho quando já existe um arquivo lá, senão ao lado do executável. O arquivo é gravado de forma **atômica e síncrona**: um temporário no mesmo diretório, `Sync()` e `Rename()` por cima do destino. Uma gravação interrompida nunca deixa configuração truncada.

O arquivo não é versionado, porque pode conter segredo. Use [`scanfile_config.example.json`](scanfile_config.example.json) como modelo.

`instance.json`, com a porta, o PID e o token da instância viva, fica em `%LOCALAPPDATA%\ScanFile\`, com permissão restrita ao usuário.

## Compilação

Requisitos: a versão de Go declarada em [`go.mod`](go.mod) — hoje **1.26.7** — no Windows. O `go.mod` é a fonte da verdade, inclusive para a CI.

Os dois scripts fazem a mesma sequência: `go vet ./...`, `go test ./...`, testes da interface com `node --test` quando o Node existe, e `go build -ldflags="-s -w" -o scanfile.exe .`

```powershell
.\build.ps1
.\build.ps1 -Race    # usa go test -race ./... (exige CGO e um compilador C)
```

```cmd
build.bat
```

Compilação manual:

```powershell
go build -ldflags="-s -w" -o scanfile.exe .
```

A CI de referência é o GitHub Actions ([`.github/workflows/ci-cd.yml`](.github/workflows/ci-cd.yml)): `go vet`, `go test -v -race ./...` com `CGO_ENABLED=1`, testes da interface no Node 22 e build com `CGO_ENABLED=0` e a versão injetada por `-ldflags`. A versão do Go vem de `go-version-file: go.mod`. O pipeline do GitLab ([`.gitlab-ci.yml`](.gitlab-ci.yml)) é um espelho.

## Testes

```powershell
go test ./... -count=1
node --test "ui/tests/*.test.mjs"
```

Números medidos neste repositório em 3 de setembro de 2026, com os comandos acima:

| Suíte | Resultado |
| :--- | :--- |
| Go | 12 pacotes, 47 arquivos de teste, 300 funções de teste (335 casos contando subtestes). Zero falhas. |
| Interface | 6 arquivos, 94 testes. Zero falhas. |

Os testes da interface rodam no Node porque `ui/js/core.js` é escrito sem ESM: o mesmo arquivo é servido ao navegador como `window.ScanFileCore` e carregado no Node por `module.exports`.

## Escala

A máquina de referência do projeto tem 128 GB de RAM, 8 núcleos e 16 threads, e cerca de 20 TB em NTFS. Só o volume `C:` dela contém **2,96 milhões de arquivos e 534 mil pastas**, contagem obtida por uma Varredura Completa em 2 de setembro de 2026 e registrada em [`docs/AVALIACAO.md`](docs/AVALIACAO.md).

A meta de escala do projeto é 50 milhões de itens nessa máquina. O caminho para chegar lá é o nó compacto em memória descrito em [`docs/adr/0001-no-compacto-em-memoria.md`](docs/adr/0001-no-compacto-em-memoria.md), ainda não implementado. Nenhum outro número de desempenho é afirmado aqui.

## Licença

MIT. Veja [`LICENSE`](LICENSE).

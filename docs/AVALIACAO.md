# Avaliação — ScanFile Pro

Avaliação do branch `feat/ai-assistant-mcp-and-folder-hierarchy` (HEAD `c59a704`, PR #1) contra a spec oficial, README + docs/, em 2 de setembro de 2026. Base: leitura integral do código Go e da UI, três auditorias independentes (concorrência Go, UI/Edge, docs↔código), logs reais de execução em `logs/` e o snapshot do C: em `saved_scans/`. As decisões que orientam as correções estão em [`DECISOES.md`](DECISOES.md).

## 1. Veredicto

**O produto não está conforme o README nos pontos que o README mais destaca.** O hashing progressivo em quatro estágios, o BLAKE3 e o MD5, o monitoramento em tempo real, a "lixeira segura" e o "zero processos órfãos" não existem ou não funcionam como descritos. Ao mesmo tempo, o código entrega funcionalidades grandes que o README não menciona: Assistente de IA, servidor MCP, autosave, Quick Scan, 12 temas, deduplicação hierárquica de pastas, compressão NTFS, links simbólicos e paginação.

**A base é boa.** Arquitetura em pacotes clara, treemap fiel ao WinDirStat, Fase 1 rápida com fila sem deadlock, elevação com IPC e monitor de PID, sondagem de discos com timeout, SSE sem bloqueio sob lock, testes nos pacotes centrais. O que falta é disciplina de estado, segurança do servidor local e escala de memória.

**Cinco problemas críticos** podem apagar dados ou expor o computador. **Oito problemas altos** travam a interface ou deixam o motor em estado inconsistente. Todos têm correção decidida.

| Auditoria | Crítico | Alto | Médio | Baixo |
|---|---|---|---|---|
| Código Go | 1 | 6 | 13 | 8 |
| UI e Edge | 3 | 7 | 13 | 15 |
| Docs ↔ código | 21 promessas sem código · 20 funcionalidades sem doc · 13 inconsistências internas | | | |
| **Consolidado (sem duplicatas)** | **5** | **8** | **15** | **~25** |

## 2. Evidências de execução (logs reais desta máquina)

| Medida | Valor | Onde |
|---|---|---|
| Processo vivo após fechar a janela | `scanfile.exe` PID 75492, 1,9 GB, horas depois do último pedido da UI | `tasklist`, log de 2026-08-31 21:59 |
| Resposta de `POST /api/cache/autosave/restore` | 930 MB de JSON, 36 a 42 s, três vezes | log de debug |
| `POST /api/cache/save` | 23 a 42 s | log de debug |
| `GET /api/tree` na raiz, profundidade 5 | 25 MB por chamada | log de debug |
| Memória para 2,96 M arquivos + 534 k pastas | 1,4 GB alocados · 8,5 GB reservados · 20 GB alocados no total (churn) | `[DEBUG MEM]` |
| Logs de erro da varredura | Só o cabeçalho, zero linhas | `logs/scan_errors_*.log` |
| Config atual | `aiOllamaEndpoint`, `aiOllamaModel` vazios e `aiDryRunDefault: false` | `scanfile_config.json` |

## 3. Conformidade com o README, funcionalidade por funcionalidade

| README | Status | O que foi verificado |
|---|---|---|
| 1. Treemap cushion, 3 visões, drill-down, slider, 3 modos de cor, menu de contexto | **Atende** | Tudo presente e funcional. Slider é 1 a 10, não 2 a 20. |
| 2. Scanner em duas fases, Smart Hashing em 4 estágios, xxHash/BLAKE3/MD5/SHA-256 | **Parcial** | Fase 1 atende. Fase 2 só agrupa por tamanho e faz hash completo; sem pré-hash nem mid-hash. Só xxHash e SHA-256 existem. |
| 3. Modo admin com IPC invisível e proteção anti-zumbi | **Parcial** | Funciona no caminho normal. Filho fica órfão se o UAC passar de 35 s. Fechar a janela deixa o processo principal vivo. Habilita 6 privilégios, docs citam 1 ou 2. |
| 4. Configurações persistentes | **Parcial** | Persiste, mas cada preferência salva apaga os campos de IA. Gravação não é atômica. |
| 5. Layout 100% e zoom da UI | **Atende** | Zoom via `body.style.zoom` pode escalar o canvas duas vezes; a validar em execução. |
| 6. Comparador de pastas e arquivos ociosos | **Atende** | Comparação sem paginação na tabela. Idades são 6 m, 1, 2, 5 anos, não 1, 2, 3, 5. |
| 7. Lixeira oficial e snapshots | **Parcial** | Lixeira usa `FOF_NOCONFIRMATION`: vira exclusão permanente quando a Lixeira não recebe. Snapshots existem, mas restore devolve o arquivo inteiro à UI; extensão documentada não existe. |
| 8. Monitor do SO em tempo real | **Não atende** | Observa só a raiz de cada disco. A cada evento faz hash síncrono e reconstrói o índice inteiro. A aba da UI chama três funções que não existem. |
| Não documentado, mas presente | — | Assistente de IA (Ollama, OpenRouter, roteador local), MCP stdio, `--debug`, `--mcp`, `--version`, autosave, Quick Scan, 12 temas, pastas clones hierárquicas, compressão NTFS, links simbólicos, paginação, barra de memória, splitter, logs de erro em disco. |

## 4. Achados consolidados

### Críticos: perda de dados ou exposição

| # | Achado | Evidência | Cenário |
|---|---|---|---|
| C1 | Exclusão permanente por qualquer origem local | `server.go:319` CORS `*`; `server.go:1125` delete sem escopo nem token | Página web ou processo local descobre a porta, faz `POST /api/files/delete` e apaga como Administrador. |
| C2 | "Lixeira" que exclui permanentemente | `recycle_windows.go:47` `FOF_NOCONFIRMATION`; `app.js:2066-2090` pastas inteiras com um `confirm` | USB exFAT, rede ou item maior que a Lixeira: apagado sem volta enquanto a UI promete restauração. |
| C3 | Config de IA apagada a cada preferência | `app.js:925-963` sem campos `ai*`; `server.go:1230` grava struct inteira | Confirmado no seu `scanfile_config.json`: Dry-Run já está desligado. |
| C4 | Autosave bom destruído após restaurar e varrer | `server.go:655` não troca `Scanner.Tree` | Varredura grava numa árvore fantasma; autosave vazio substitui o bom e rotaciona o backup para fora. |
| C5 | IA age sem aprovação e aceita injeção | `tools.go:828` executa com `dry_run=false`; `tools.go:873` MOVE sobrescreve; `app.js:4087` `innerHTML` com texto do modelo | PDF com instrução embutida faz o modelo mover ou reciclar, e injeta script com acesso à API local. |

### Altos: travamentos e estado inconsistente

| # | Achado | Evidência |
|---|---|---|
| H1 | Cancelar não cancela; scan sobre scan corrompe a árvore | `server.go:455` `context.Background()`; `server.go:442` `Reset` durante workers; `scanner.go:166`; `app.js:1164` esconde o botão; UI nunca lê `/api/scan/status` |
| H2 | `WriteTimeout` de 60 s derruba SSE, chat e download; UI presa em "Varrendo" | `server.go:250`; `app.js:1276` `onerror` sem ressincronizar |
| H3 | Restore e save pesados: 930 MB, 36 a 42 s; encoder bufferiza o documento inteiro | `server.go:696`; `cache.go:50-92`; `GetAllFiles` copia todos os ponteiros |
| H4 | Monitoramento inoperante e aba morta | `watcher.go:60` só raiz; `watcher.go:125,137` hash síncrono e `RebuildIndex` O(N); `app.js:563,819,1273` funções inexistentes |
| H5 | Processo não morre com a janela; sem instância única | `main.go:227` `Start` sem vínculo; PID 75492 com 1,9 GB |
| H6 | `/api/tree` sem teto de arquivos e squarify quadrático | `tree.go:437`; `app.js:2329` |
| H7 | Data races | `hasher.go:353` escreve `Hash` enquanto handlers leem; `cancelFunc`, `s.Tree`, `s.Watcher`, `ActiveWorker` sem sincronização |
| H8 | Memória para 50 M itens | ~400 B por item medido; 20 GB alocados mais folga do GC; picos de serialização. Resolvido por ADR-0001 |

### Médios

| # | Achado | Evidência |
|---|---|---|
| M1 | `dev`, `sys`, `proc`, `mnt` na raiz de qualquer volume e nomes repetidos 3 vezes são pulados sem log | `scanner.go:544`, `scanner.go:425` |
| M2 | `EvalSymlinks` em todo diretório; `visitedDirs` retém uma string por pasta | `scanner.go:430` |
| M3 | `RebuildIndex` completo a cada consulta que devolve zero grupos | `server.go:1054`, `server.go:876` |
| M4 | `workerThreads` sem teto: goroutines e buffers de 1 MB ilimitados | `scanner.go:220`, `hasher.go:155` |
| M5 | `DiskErrorLogger` nunca faz flush nem close | `error_logger.go`; logs vazios em `logs/` |
| M6 | Quick Scan mantém duas árvores completas em memória | `server.go:427` |
| M7 | Filho elevado órfão após 35 s; elevar pela UI duplica instâncias | `privileges_windows.go:584`, `server.go:1208` |
| M8 | MCP stdio sem recuperação de panic e com árvore nula | `mcp/server.go:15`, `main.go:59` |
| M9 | Chave OpenRouter em texto puro e devolvida por `GET /api/config` | `config.go:42`, `server.go:1243` |
| M10 | SQLite aberto em leitura e escrita; driver ignora `?mode=ro` | `tools.go:405` |
| M11 | `localStorage` preso à porta aleatória; zoom pode duplicar escala do canvas | `app.js:2106`, `app.js:829`, `app.js:2174` |
| M12 | Tabela de comparação sem paginação; estratégias de seleção só na página atual | `app.js:3421`, `app.js:2797` |
| M13 | Falha no GET de config leva a sobrescrever o arquivo com defaults | `app.js:857` |
| M14 | Hash reaproveitado sem checar algoritmo; Merkle de pastas usa tamanho e data quando falta hash, gerando "100% idêntica" falsa | `scanner.go:478`, `folder_index.go:179` |
| M15 | Goroutine de autosave vaza uma por varredura | `server.go:455-487` |

### Baixos (resumo)

Cerca de 25 itens: 28 `getElementById` para ids inexistentes, listeners duplicados nos filtros de pastas, `JSON.parse` sem proteção, `ev.ToolName` versus `ev.toolName`, resíduos em inglês, acessibilidade de menus e modais, fontes do Google bloqueando a primeira pintura, IPC sem autenticação, argumentos do relançamento sem aspas, erros engolidos em autosave e watcher, `os.Exit` pulando o `Stop`, `/api/tree` até profundidade 8, `restore` marcando discos por `data-mount` inexistente.

### Documentação (54 itens)

Promessas sem código: BLAKE3, MD5, pré-hash e mid-hash, "SSE a 60 FPS", "Go 1.22+", LICENSE MIT, extensão `.scanfile.cache.json.gz`, slider 2 a 20, ociosos de 3 anos, arquivos inexistentes em ESTRUTURA, "salvamento atômico", "nenhuma pasta temporária", "ícones SVG", watcher recursivo, 2 privilégios em vez de 6.
Sem documentação: IA, MCP, flags `--debug`, `--mcp`, `--version`, autosave, Quick Scan, temas, pastas clones hierárquicas, compressão NTFS, links simbólicos, paginação, barra de memória, splitter, SQLite e PDF, `models/`, logs de erro, stubs multiplataforma.
Inconsistências: quatro comandos de build diferentes, três de teste, GitLab testa Linux com disco fictício, `-race` sem CGO quebra o pipeline do GitLab de forma determinística, sufixos `-pr` e `-mr`, data de build local rotulada como UTC.

## 5. O que está bem feito

- Servidor só em `127.0.0.1`; IPC também em loopback; recover de panic no middleware HTTP.
- SSE com canal bufferizado, envio não bloqueante sob `RLock` e remoção do cliente ao desconectar.
- Árvore sem `RLock` recursivo nem inversão de locks; travessias copiam filhos sob lock curto.
- Fila de diretórios com `sync.Cond` e cancelamento por `Broadcast`; sem deadlock por capacidade de canal.
- Hasher fecha arquivos em todos os caminhos, pool limitado, arquivos bloqueados viram `LOCKED` sem abortar.
- Autosave atômico com temp e rotação; sondagem de discos com timeout por unidade.
- Treemap com legenda iterativa, profundidade máxima 8, teto de 2000 nós, canvas com DPR.
- Paginação em todas as listas, `debounce` nas buscas, `AbortController` nos discos, `confirm` antes de reciclar.
- 15 testes em 9 arquivos passando; `go vet` limpo.

## 6. Plano de correção em ondas (etapa b)

Cada onda fecha com critério de verificação automatizado ou reproduzível. Tamanhos: S até 1 dia, M 2 a 3 dias, L 1 semana, XL 2 semanas.

| Onda | Foco | Achados | Decisões | Tamanho | Verificação |
|---|---|---|---|---|---|
| 1 | Segurança e perda de dados | C1, C2, C3, C4, C5, M9, M10 | Q8, Q10, Q19, Q22, Q23, Q32 | L | `POST` sem token devolve 401; reciclar em volume sem Lixeira devolve erro e nada é apagado; config faz ida e volta preservando `ai*`; proposta com `dry_run=false` fica pendente; texto do modelo aparece escapado; restaurar e varrer preserva o autosave. |
| 2 | Estado e travamentos | H1, H2, H3, H6, M3, M4, M5, M13, M15 | Q6, Q7, Q20, Q21, Q25, Q36 | L | Cancelar leva o status a `cancelled` em menos de 2 s e nada roda depois; segundo start devolve 409; SSE sobrevive 10 min; restore devolve menos de 50 KB; `/api/tree` nunca passa de 500 arquivos; logs de erro têm linhas. |
| 3 | Ciclo de vida do processo | H5, M7 | Q3, Q13, Q24, Q33 | M | Fechar a janela encerra o processo em até 10 s sem varredura; com varredura, encerra ao terminar; segunda execução foca a janela; elevar deixa uma instância. |
| 4 | Motor de varredura e memória | H4, H7, H8, M1, M2, M6, M14 | Q4, Q5, Q18, Q31, Q35, Q37, Q39 | XL | `go test -race` limpo; 50 M itens sintéticos abaixo de 10 GB; pré-hash reduz bytes lidos em teste com colisões de tamanho; criar arquivo em subpasta aparece no monitor em até 5 s; todo item pulado consta no log. |
| 5 | IA e MCP | M8 | Q11, Q27, Q28, Q29, Q38 | M | `--mcp` lista arquivos do autosave; catálogo só com modelos até 14 GB e selo correto; roteador aparece como "Comandos Rápidos". |
| 6 | Docs, CI e acabamento da UI | Baixos, 54 itens de docs | Q14, Q15, Q30, Q34 | M | README reflete flags, endpoints e formatos reais; LICENSE presente; GitHub e GitLab verdes com Go de `go.mod`; nenhuma requisição externa na inicialização; zero ids mortos. |

Ordem recomendada: 1, 2, 3, 4, 5, 6. As ondas 1 a 3 tornam o produto seguro e estável no que já existe. A onda 4 é a maior e é onde o README passa a ser verdadeiro. As ondas 5 e 6 fecham a experiência e a documentação.

## 7. Riscos a acompanhar durante a etapa b

- **ADR-0001** toca toda leitura de `FileNode.Path`: scanner, cache, indexadores, MCP e servidor. Fazer em branch próprio com os testes de `tree`, `cache` e `indexer` revisados antes.
- **Monitoramento recursivo em 20 TB**: buffers do `ReadDirectoryChangesW` estouram em cópias grandes; a revarredura da raiz afetada precisa ser barata, o que depende do nó compacto.
- **BLAKE3**: escolher biblioteca Go pura com SIMD e travar a versão; verificar velocidade contra xxHash antes de oferecer como padrão.
- **DPAPI** só existe no Windows; o stub `_other.go` precisa guardar a chave em claro com aviso ou recusar.
- **Porta fixa** pode colidir com outro serviço; manter fallback e mostrar a porta real na janela.
- **Ollama e tool calling**: validar cada modelo do catálogo na versão instalada antes de dar o selo.

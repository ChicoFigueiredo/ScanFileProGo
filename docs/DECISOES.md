# Decisões — ScanFile Pro

Registro das decisões tomadas na sessão de avaliação de 2 de setembro de 2026 (branch `feat/ai-assistant-mcp-and-folder-hierarchy`, PR #1). Foram 39 perguntas em duas rodadas, todas decididas. Vocabulário em [`CONTEXT.md`](../CONTEXT.md); decisões arquiteturais em [`docs/adr/`](adr/).

Máquina de referência: 128 GB RAM · AMD Ryzen 7 3700X (8 núcleos / 16 threads) · RTX 4080 SUPER 16 GB · ~20 TB em NTFS (C 1,9 · E 1,9 · P 1,9 · T 2,8 · U 7,4 · Y 3,7 TB) · L: e Z: em 9P (WSL) · só o C: tem 2,96 M arquivos e 534 k pastas.

## 1. Escopo e entrega

| # | Decisão |
|---|---|
| Q1 | A spec oficial é o **README + docs/**. Conformidade é medida contra eles; requisitos novos entram nos docs. |
| Q2 | "Edge" significa a **janela do Microsoft Edge em modo aplicativo** aberta pelo `main.go`. |
| Q12 | Meta de escala: **50 milhões de itens** (arquivos + pastas) na máquina de referência acima. |
| Q16 | Entrega em duas etapas: **(a) relatório de avaliação priorizado**, depois **(b) correções** somente após confirmação. Alvo é este branch, não `main`. |
| Q15 | **Reescrever** README, ARQUITETURA e ESTRUTURA para refletir o código; adicionar **LICENSE (MIT)** e **CONTEXT.md**. |
| Q17 | `.agents/` e `skills-lock.json` **commitados** (feito em d5be7ba). |
| Q26 | `scanfile_config.json` **fora do git**, com `scanfile_config.example.json` versionado sem segredos (feito em d5be7ba). |

## 2. Ciclo de vida do processo e da janela

| # | Decisão |
|---|---|
| Q3 | **Fechar a janela encerra o backend**: heartbeat da UI e desligamento gracioso após alguns segundos sem cliente. `--no-window` continua como modo servidor explícito. |
| Q33 | Se houver **Varredura em curso** ao fechar a janela: continua sem janela até terminar, grava Autosave e encerra. Reabrir o programa reconecta na instância viva. |
| Q24 | **Porta fixa** padrão com fallback e **instância única**: segunda execução foca a janela existente. Corrige `localStorage` preso à porta aleatória e o zoom acumulado do perfil do Edge. |
| Q13 | Elevar pela UI: a instância atual **entrega o controle** ao filho elevado (mesma porta, fluxo IPC já existente) e **encerra**. Nunca duas instâncias vivas. |
| Q9 | Modo Administrador com **privilégios irrestritos** (SeBackup, SeRestore, SeTakeOwnership, SeSecurity e demais). Acesso total é requisito. |

## 3. Varredura e hashing

| # | Decisão |
|---|---|
| Q4 | Implementar o **Pré-hash** (4 KB do início + 4 KB do fim) **e** adicionar **BLAKE3 e MD5** aos algoritmos, além de xxHash64 e SHA-256. |
| Q37 | Pré-hash **sempre xxHash64**. Algoritmo final padrão xxHash64. **Quick Scan só reaproveita hash do mesmo algoritmo** do Snapshot. BLAKE3 por biblioteca Go pura, sem DLL. |
| Q6 | **Cancelar aborta o pipeline inteiro** (Fase 1, Fase 2, Autosave, índices). Segundo `scan/start` durante uma Varredura → **HTTP 409** até o Cancelamento concluir; estado "cancelando" no SSE. |
| Q18 | Heurísticas de WSL e de loop **restritas** a montagens WSL reais e a reparse points. **Todo Item Pulado é registrado** no log de erros e contabilizado no status. Subcontagem invisível é inaceitável. |
| Q20 | **Teto de threads** em 4 × NumCPU; UI com combo derivado do processador; buffers de 1 MB em pool compartilhado. |
| Q36 | Combo de threads com **Auto, 4, 8, 16, 32 e 64** (teto 64 = 4 × 16 lógicos). **"Auto" = 32 na Fase 1** (limitada por I/O) **e 16 na Fase 2** (limitada por CPU). Em outras máquinas os valores derivam de NumCPU. |
| Q31 | **Nó compacto em memória** ([ADR-0001](adr/0001-no-compacto-em-memoria.md)): caminho derivado do pai, extensão internada, hash em bytes fixos. Meta ~150 B/item (~7 GB para 50 M). API e Snapshot continuam com caminhos completos. |
| Q39 | Volumes **9P do WSL** listados, **desmarcados por padrão**, com aviso de lentidão e duplicação. |

## 4. Monitoramento e persistência

| # | Decisão |
|---|---|
| Q5 | **Monitoramento recursivo real** das Raízes Varridas, com coalescência e índice incremental, **ligado por padrão** após a Varredura. |
| Q35 | Parâmetros: coalescência de **2 s** por caminho; **2 workers** de hash em segundo plano usando Pré-hash; índices incrementais; estouro do buffer do Windows → revarrer só a raiz afetada. |
| Q7 | **Autosave** durante a Varredura, ao concluir e, com Monitoramento ativo, **a cada 10 min apenas se houve mudança**. Um único ticker. `restore` devolve um **resumo**, nunca o snapshot inteiro. |
| Q21 | Snapshot continua **JSON + gzip**, mas **serializado em streaming** por arquivo, sem buffer único. Formatos `.sfz` e `.scanfile.gz` mantidos. |
| Q11 | `--mcp` (Claude Desktop e afins) **carrega `autosave_latest.sfz`** em modo somente leitura ao iniciar. |

## 5. Segurança

| # | Decisão |
|---|---|
| Q8 | **Token por sessão** injetado na UI e exigido em toda chamada; **sem CORS**; Reciclagem e Exclusão **só dentro das Raízes Varridas**. |
| Q19 | Chave OpenRouter protegida por **DPAPI**; `GET /api/config` devolve apenas "configurada", nunca a chave. |
| Q22 | Lixeira indisponível → **recusar e informar**. Nunca excluir permanentemente por esse caminho. **Exclusão Permanente** é ação separada, com confirmação digitada. |
| Q23 | Reciclar **pastas pelo treemap** mantido, com confirmação que mostra tamanho e quantidade, exige **digitar o nome da pasta** e respeita as Pastas Protegidas. |
| Q32 | **Pastas Protegidas** mesmo em Modo Administrador: **raiz de cada volume, `\Windows`, `System Volume Information`**. Todo o resto liberado com confirmação digitada. |

## 6. Assistente de IA

| # | Decisão |
|---|---|
| Q10 | **Aprovação humana obrigatória** para RECYCLE/MOVE/DELETE, independentemente do `dry_run` enviado pelo modelo. IA **lê arquivos só dentro das Raízes Varridas**. MOVE falha se o destino já existir. |
| Q27 | Provedor "Direto (In-Process)" vira **"Comandos Rápidos"**: roteador por palavras-chave, apresentado como tal, não como modelo. |
| Q28 | MCP stdio **expõe também** `analyze_image_visual` e `compare_visual_similarity`. |
| Q29 / Q38 | Modelo padrão **`qwen3-vl:8b`**. Catálogo limitado a **~14 GB** com selo de capacidade (visão / ferramentas): `qwen3-vl:8b`, `qwen2.5vl:7b`, `gemma3:12b`, `qwen3:14b`, `gpt-oss:20b`, `devstral:24b`. Modelos sem visão perdem as ferramentas de imagem, com aviso. Verificar suporte a tool calling no Ollama antes de implementar. |

## 7. Interface

| # | Decisão |
|---|---|
| Q25 | Treemap mostra os **500 maiores arquivos** da pasta atual; tabela **paginada pelo servidor em 100**. |
| Q30 | Fontes Inter e JetBrains Mono **embutidas no binário**; nenhum CDN. |

## 8. Build, CI e documentação

| # | Decisão |
|---|---|
| Q14 | **GitHub Actions** é a fonte da verdade de CI. Versão do Go lida de `go.mod` (`go-version-file`). |
| Q34 | Pipeline do **GitLab mantido como espelho**, corrigido: Go de `go.mod`; `-race` só com CGO habilitado ou removido. |

## 9. Bugs sem decisão pendente (entram direto na etapa b)

- Config de IA apagada a cada preferência salva (`saveCurrentConfig` sem campos `ai*`; servidor grava a struct inteira).
- Texto do modelo injetado como HTML no chat (`innerHTML`).
- Funções `fetchEventLogs`, `addFSEvent`, `renderEventLogs` inexistentes; aba Monitor do SO morta.
- SSE sem ressincronização ao reconectar; `/api/scan/status` nunca consultado; F5 permite scan sobre scan.
- `WriteTimeout` de 60 s derruba SSE, chat e download de modelos.
- `handleRestoreAutoSave` não troca `Scanner.Tree` nem o contexto MCP; autosave bom é sobrescrito por um vazio.
- `DiskErrorLogger` nunca faz flush/close; logs de erro ficam vazios.
- `inspectSQLite` abre em leitura e escrita (driver ignora `?mode=ro`).
- Goroutine de autosave vaza uma por Varredura (`context.Background()`).
- Filho elevado órfão se o UAC demorar mais de 35 s.
- Data races em `cancelFunc`, `s.Tree`, `s.Watcher`, `FileNode.Hash`, `ActiveWorker`.
- Fallback `RebuildIndex` a cada consulta que retorna 0 grupos.
- `EvalSymlinks` em todo diretório; `visitedDirs` retém uma string por pasta.
- Docs: BLAKE3/MD5 e Pré-hash prometidos sem código; "60 FPS"; "Go 1.22+" vs `go 1.26.7`; arquivos inexistentes em ESTRUTURA; extensão `.scanfile.cache.json.gz`; slider 2–20; ociosos "3 anos"; LICENSE ausente.

## 10. Já executado nesta sessão

- Commit `d5be7ba`: config fora do git, example versionado, `.agents/` e `skills-lock.json` commitados.
- `CONTEXT.md` criado com o glossário do domínio.
- `docs/adr/0001-no-compacto-em-memoria.md`.
- Este registro.

## 11. Próximos passos

1. Etapa (a): relatório de avaliação priorizado (`docs/AVALIACAO.md`).
2. Etapa (b): correções na ordem confirmada no relatório.

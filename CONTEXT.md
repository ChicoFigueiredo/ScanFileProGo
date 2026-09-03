# ScanFile Pro

Analisador de espaço em disco para Windows: varre volumes, identifica duplicados por conteúdo, pastas clones e arquivos ociosos, e permite liberar espaço com segurança. Este glossário fixa o vocabulário do domínio; decisões de implementação ficam em `docs/adr/`.

## Language

### Varredura

**Varredura Completa**:
Execução das duas fases sobre as Raízes Varridas: Fase 1 (metadados) e Fase 2 (hashing), seguida da indexação de duplicados.
_Avoid_: scan, full scan, escaneamento

**Fase 1**:
Etapa da Varredura que percorre as pastas e registra metadados de todos os arquivos, sem ler conteúdo.
_Avoid_: mapeamento, discovery

**Fase 2**:
Etapa da Varredura que lê o conteúdo dos Candidatos a Duplicado para calcular hashes.
_Avoid_: hashing phase

**Quick Scan**:
Varredura Completa que reaproveita o hash de um arquivo cujo caminho, tamanho e data de modificação não mudaram desde o último Snapshot.
_Avoid_: varredura incremental, atualização rápida

**Raízes Varridas**:
Conjunto de volumes ou pastas escolhidos pelo usuário para uma Varredura. Delimita também onde a Reciclagem, a Exclusão Permanente e a leitura de arquivos pelo Assistente são permitidas.
_Avoid_: discos selecionados, roots

**Cancelamento**:
Interrupção pelo usuário que aborta todo o pipeline da Varredura: fases, Autosave e indexação. Uma nova Varredura só pode iniciar após o Cancelamento concluir.
_Avoid_: parar, abortar

**Item Pulado**:
Pasta ou arquivo que a Varredura decidiu não percorrer, por exemplo junção NTFS já visitada ou pseudo-arquivo do WSL. Todo Item Pulado é registrado no Log de Erros e contabilizado no status.
_Avoid_: ignorado silenciosamente

### Hashing

**Candidato a Duplicado**:
Arquivo que compartilha o tamanho exato em bytes com pelo menos um outro arquivo nas Raízes Varridas.
_Avoid_: arquivo suspeito

**Pré-hash**:
Hash xxHash64 dos primeiros 4 KB e dos últimos 4 KB de um Candidato a Duplicado. Descarta colisões de tamanho sem ler o arquivo inteiro.
_Avoid_: quick hash, smart hash, mid-hash

**Hash Completo**:
Hash de 100% do conteúdo, calculado no algoritmo escolhido pelo usuário (xxHash64, BLAKE3, MD5 ou SHA-256) apenas para arquivos que colidem no Pré-hash.
_Avoid_: full hash

**Grupo de Duplicados**:
Conjunto de dois ou mais arquivos com o mesmo Hash Completo e o mesmo tamanho.
_Avoid_: duplicata, clone de arquivo

**Pasta Clone**:
Pasta cujo conteúdo, arquivos e subpastas, é idêntico ao de outra pasta segundo o hash de conteúdo da pasta.
_Avoid_: pasta duplicada, pasta espelho

**Arquivo Ocioso**:
Arquivo sem modificação nem acesso há mais tempo que o limite escolhido pelo usuário.
_Avoid_: arquivo dormente, arquivo antigo

### Persistência

**Snapshot**:
Cópia em disco da árvore varrida e dos hashes, salva pelo usuário sob um nome escolhido, restaurável a qualquer momento.
_Avoid_: cache, backup

**Autosave**:
Snapshot gravado automaticamente durante a Varredura, ao concluí-la e, com o Monitoramento ativo, a cada 10 minutos quando houve mudanças. Mantém apenas o último e o anterior.
_Avoid_: snapshot automático, checkpoint

**Configuração**:
Preferências do usuário persistidas entre sessões: Raízes Varridas, algoritmos, filtros, tema, zoom e opções do Assistente. Segredos são guardados protegidos e nunca devolvidos pela API.
_Avoid_: settings, config

### Monitoramento

**Monitoramento**:
Estado após uma Varredura Completa em que o sistema observa recursivamente as Raízes Varridas e atualiza a árvore e os índices conforme arquivos mudam.
_Avoid_: watcher, tempo real, hooks

### Ações sobre arquivos

**Reciclagem**:
Envio de arquivos ou pastas para a Lixeira do Windows. Se a Lixeira não puder receber o item, a operação falha e informa; nunca vira Exclusão Permanente.
_Avoid_: deletar, apagar, mandar para a lixeira

**Exclusão Permanente**:
Remoção irreversível, disponível apenas como ação separada com confirmação digitada.
_Avoid_: delete, remoção definitiva

**Pasta Protegida**:
Raiz de um volume, a pasta `Windows` do sistema ou `System Volume Information`. Nunca pode ser alvo de Reciclagem ou Exclusão Permanente, mesmo em Modo Administrador.
_Avoid_: blocklist

**Modo Administrador**:
Execução elevada em que o processo habilita os privilégios de backup, restauração e posse para ler e agir sobre qualquer pasta do sistema.
_Avoid_: modo root, elevado

### Assistente

**Assistente**:
Agente de IA integrado que consulta a árvore varrida, inspeciona conteúdo dentro das Raízes Varridas e formula Propostas.
_Avoid_: chat, IA, copiloto

**Proposta**:
Ação sobre arquivos formulada pelo Assistente, sempre pendente até aprovação explícita do usuário na interface. O Assistente nunca executa ações por conta própria.
_Avoid_: dry-run, sugestão, plano

**Provedor**:
Origem da inferência do Assistente: Ollama local, OpenRouter na nuvem ou Comandos Rápidos.
_Avoid_: engine, backend de IA

**Comandos Rápidos**:
Roteador por palavras-chave que aciona ferramentas do Assistente sem modelo de linguagem. Não é um Provedor de inferência.
_Avoid_: motor direto, in-process, modelo local

### Interface

**Janela**:
Instância única do Microsoft Edge em modo aplicativo apontando para a porta fixa do servidor local. Fechar a Janela encerra o servidor após a Varredura em curso terminar.
_Avoid_: webview, navegador

**Sessão**:
Vínculo autenticado por token entre a Janela e o servidor local. Toda chamada à API exige o token da Sessão.
_Avoid_: origem, CORS

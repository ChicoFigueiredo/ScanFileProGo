---
status: accepted
date: 2026-09-02
---
# Nó de arquivo compacto em memória

A meta é manter 50 milhões de itens em memória na máquina de referência (128 GB). A estrutura atual guarda, por arquivo, caminho completo, nome, extensão e hash como strings independentes: medimos cerca de 400 bytes por item (1,4 GB para 3,5 milhões de itens no snapshot do C:), e cada Autosave ou `GetAllFiles` copia tudo, gerando picos de dezenas de gigabytes e autosaves de 40 s. Decidimos representar cada arquivo por nome mais referência ao diretório pai, com caminho derivado sob demanda, extensão internada e hash em bytes fixos, mirando ~150 bytes por item (~7 GB para 50 milhões). A API HTTP e os Snapshots continuam expondo caminhos completos.

## Considered Options

Manter a estrutura atual e depender dos 128 GB, configurando apenas o limite de memória do runtime. Cabe hoje, mas não reduz o custo de serialização nem os picos, e não deixa margem para o Monitoramento e o Assistente rodarem ao lado.

## Consequences

Renomear ou mover um diretório passa a reposicionar um nó em vez de reescrever strings de todos os descendentes; todo ponto que hoje lê `FileNode.Path` diretamente precisa pedir o caminho ao nó pai. Os testes de `tree`, `cache` e `indexer` precisam ser revisados.

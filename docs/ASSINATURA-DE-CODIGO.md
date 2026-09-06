# Assinatura de código do ScanFile Pro

Como obter um certificado de assinatura de código — de graça na SignPath Foundation
ou pago na Certum — e usá-lo para assinar o `scanfile.exe`. Escrito para ser
seguido de cima para baixo, uma vez.

## 1. Por que isto existe

O binário atual não tem assinatura Authenticode nem metadados de versão. Verificado:

```
Get-AuthenticodeSignature scanfile.exe  ->  Status: NotSigned
(Get-Item scanfile.exe).VersionInfo     ->  CompanyName, ProductName,
                                            FileDescription, LegalCopyright vazios
                                            FileVersion 0.0.0.0
```

O `-X main.Version` que o CI injeta vive numa string Go: o Windows e os antivírus
não enxergam isso. Para o sistema, o `scanfile.exe` é um executável de 15 MB sem
autor, sem produto e sem assinatura.

Isso tem consequência concreta na Kaspersky. Pela documentação dela, um aplicativo
entra no grupo **Confiável** se for assinado por fornecedor confiável, estiver na
base de aplicativos confiáveis da KSN, ou for colocado lá à mão pelo usuário. Não
atendendo a nenhum dos três, cai em **Altamente Restrito** — e o ScanFile pede
SeBackupPrivilege, se auto-eleva com `runas`, enumera a árvore inteira do disco e
apaga arquivos via `SHFileOperationW`. É o perfil de um software de backup, que a
heurística tolera quando o binário tem identidade, e não tolera quando não tem.

A varredura estática já passa limpa (`avp.com SCAN` devolve `0 detected`). O que
falta é identidade, não limpeza.

## 2. Qual certificado

Duas rotas servem ao ScanFile, e a decisão da Q40 — gratuito para sempre —
habilita as duas. Tente a gratuita primeiro.

| | SignPath Foundation | Certum Open Source |
|---|---|---|
| Custo | **grátis** | a partir de US$ 58 |
| Publicador exibido no Windows | `SignPath Foundation` | `Open Source Developer <seu nome>` |
| Reputação inicial | herda a do certificado, já usado por muitos projetos | zero; acumula com o tempo |
| Documentos pessoais | nenhum | documento com foto + conta de consumo |
| Automação no CI | nativa, conector do GitHub | manual, ou runner próprio (seção 7) |
| O que é verificado | que o binário saiu do **seu repositório** | que **você** é quem diz ser |
| Depende de | aprovação do projeto | seu pagamento e seus documentos |
| Licenciamento duplo | proibido | permitido |

### 2.1 Rota gratuita: SignPath Foundation

A SignPath Foundation assina código de projetos open source **sem cobrar**. O
modelo é diferente do de uma CA comum: em vez de verificar a sua identidade
pessoal, ela verifica que o binário foi construído a partir do seu repositório
público e **avaliza isso com o nome dela**. Por consequência, o certificado é
emitido para a *SignPath Foundation*, e é esse o publicador que o Windows mostra —
não o seu nome.

Isso tem uma vantagem que dinheiro não compra: **o certificado já tem reputação**.
Um certificado Certum novo em folha começa do zero no SmartScreen e na KSN e leva
meses de downloads para aquecer. O da SignPath já é usado por muitos projetos.

Condições de elegibilidade:

- licença **aprovada pela OSI, sem licenciamento duplo comercial** — a GPL-3.0 da
  Q41 atende, e a Q40 (gratuito para sempre) também;
- **sem componentes proprietários** (bibliotecas de sistema são permitidas, na
  definição da GPL v3);
- código público, e o time que assina precisa ser o mesmo que desenvolve e
  **é dono do repositório**;
- projeto **ativamente mantido, com releases já existentes** e funcionalidade
  documentada na página de download;
- **reputação verificável do projeto** — o ponto que mais pesa contra o ScanFile
  hoje, que é novo e ainda tem a PR #1 sem merge;
- os scripts de build e a configuração de CI passam por revisão de código deles;
- obrigações do time: **MFA** na SignPath e no repositório, papéis definidos
  (autor, revisor, aprovador) e uma **política de assinatura publicada**;
- todo binário assinado precisa ter os metadados preenchidos (nome do produto,
  versão) — é a pendência 1 da seção 8, que vira obrigatória nesta rota.

Aplicação em <https://signpath.org/>, com avaliação de alguns dias a algumas
semanas. Assinam EXE, MSI e outros formatos, com conector nativo de GitHub — o que
resolve de graça o problema de automação da seção 7.

**O risco desta rota é a recusa**, ou o pedido para voltar quando o projeto tiver
tração. Se isso acontecer, siga para a 2.2 e reaplique mais tarde.

### 2.2 Rota paga: Certum Open Source Code Signing in the Cloud

| | |
|---|---|
| Preço | a partir de US$ 58 |
| Cartão físico | não é necessário — chave em HSM da Certum |
| Nome do publicador | `Open Source Developer <seu nome>` |
| Limite | 5.000 assinaturas por mês |
| Emitido para | pessoa física apenas |
| SmartScreen | acumula reputação ao longo do tempo |

**A restrição que decide tudo:** o certificado open source é **estritamente para
projetos open source não comerciais**. A própria Certum diz que, se identificar que
o certificado está assinando software distribuído comercialmente, **revoga**. O
certificado não pode conter domínio nem endereço IP, e o campo Common Name sempre
traz o prefixo `Open Source Developer`.

Isso é compatível com a GPL-3.0 do projeto (Q41): a GPL permite que **terceiros**
usem e vendam; a restrição da Certum recai sobre **você**, titular do certificado,
distribuir comercialmente. Enquanto suas releases forem gratuitas, não há conflito.

Se um dia a Q40 mudar e o ScanFile virar produto pago, o certificado certo passa a
ser o **Certum Code Signing in the Cloud** padrão (a partir de ~US$ 116) ou o **EV**
(a partir de ~US$ 226). O EV é o único que dá reputação de SmartScreen imediata,
sem período de aquecimento. Trocar de certificado **zera a reputação acumulada**,
então é uma decisão de ida só. (A SignPath também deixa de servir nesse cenário: a
proibição de licenciamento duplo comercial é condição de elegibilidade.)

Existe também o produto com cartão criptográfico (a partir de US$ 29 se você já
tiver cartão e leitora; o conjunto completo custa mais e o cartão não é
reembolsável). **Não vale a pena:** a versão em nuvem custa pouco mais, evita
hardware e é a única que dá para automatizar.

As seções 3 a 6 descrevem esta rota. Se você for pela SignPath, pule para a 7.

### 2.3 O que não serve

**Let's Encrypt e o certbot não emitem certificado de assinatura de código.** A
Let's Encrypt só emite certificado TLS validado por domínio: prova-se controle do
domínio, e é isso. Assinatura de código exige EKU de Code Signing e, pelas
Baseline Requirements do CA/Browser Forum, exige **validação de identidade** e
**chave em hardware FIPS ou HSM**. Não existe equivalente automatizado e gratuito
do certbot para código — a validação é feita por gente, não por desafio HTTP. É
justamente por isso que a SignPath Foundation existe: alguém precisa pagar essa
verificação, e no caso deles é uma fundação patrocinada.

**Certificado autoassinado é gratuito e inútil aqui.** Ele não encadeia numa raiz
confiável: o Windows continua mostrando "editor desconhecido" e a Kaspersky continua
sem colocar o processo no grupo Confiável. Só faz sentido em ambiente administrado,
com o certificado empurrado por GPO para o repositório de Editores Confiáveis das
máquinas — o oposto de distribuição pública.

### Validade encurtou (rota Certum)

A partir de **1º de março de 2026** a validade máxima de um certificado de
assinatura de código caiu de ~3 anos para **460 dias** (~15 meses). O aviso da
Certum cita os produtos Standard e EV de 2 e 3 anos; produtos de 1 ano não mudam.
Confirme na compra o que se aplica ao open source. Consequência prática: planeje
renovar todo ano, e **renove sempre o mesmo certificado** — a reputação na KSN e no
SmartScreen se acumula no certificado, não no binário.

## 3. Rota Certum: o que você precisa ter em mãos

A verificação de identidade aceita uma destas formas:

- verificação automática de identidade (recomendada pela Certum);
- confirmação em Ponto de Registro;
- confirmação notarial;
- **documentação fotográfica**: fotos do titular segurando o documento de
  identidade (RG, passaporte, CNH ou carteira de residência permanente).

Além disso, obrigatoriamente:

1. **Comprovante de endereço** — conta de consumo (luz, água, gás, telefone)
   emitida em nome do titular.
2. **URL do projeto open source em andamento**, publicamente acessível, que
   demonstre com clareza a sua ligação com ele. Se a Certum não conseguir
   identificar o projeto pela informação pública, **o certificado não é emitido**.

O item 2 é o que exige preparo do repositório — veja a seção 8.

O envio mais rápido é pelo upload direto na conta da Certum Store.

## 4. Rota Certum: pedido, passo a passo

1. Criar conta em `certum.store` e comprar o **Open Source Code Signing in the Cloud**.
2. Preencher o formulário do certificado com os dados da pessoa física.
3. Enviar os documentos da seção 3 pela conta.
4. Aguardar a verificação da Certum (identidade + existência do projeto).
5. Receber o e-mail de ativação e ativar o certificado no SimplySign.

## 5. Rota Certum: ativação do SimplySign

1. Instalar o **SimplySign Mobile** no celular (Android ou iOS) — é ele que gera o
   token TOTP de 6 dígitos.
2. Baixar e instalar o **SimplySign Desktop** para Windows, em
   <https://support.certum.eu/en/cert-offer-software-and-libraries/>.
   Nenhum software da Certum está instalado nesta máquina hoje.
3. O ícone aparece na bandeja do sistema. Botão direito → **Conectar ao SimplySign**.
4. Informar o identificador (e-mail da conta) e o token gerado no celular.
5. Confirmada a conexão, o SimplySign Desktop passa a expor o certificado ao
   Windows **como se fosse um cartão inteligente**. O `signtool` não sabe que o
   cartão é virtual — é isso que faz a assinatura funcionar sem hardware.

Para conferir: botão direito no ícone → **Gerenciar certificados → Lista de
certificados**.

## 6. Rota Certum: como assinar

### Obter o thumbprint

Duplo clique no certificado na lista do SimplySign Desktop → aba **Detalhes** →
campo **Thumbprint**. Copie o valor (40 caracteres hexadecimais, sem espaços).

Alternativa por linha de comando, com o SimplySign conectado:

```powershell
Get-ChildItem Cert:\CurrentUser\My -CodeSigningCert |
  Select-Object Thumbprint, Subject, NotAfter
```

### O comando

Este é o comando da documentação oficial da Certum:

```
signtool sign /sha1 "<THUMBPRINT>" /tr http://time.certum.pl /td sha256 /fd sha256 /v "scanfile.exe"
```

- `/sha1` — thumbprint do certificado
- `/tr http://time.certum.pl` — carimbo de tempo RFC 3161 da Certum. **Obrigatório.**
  Sem ele, o binário deixa de ser confiável no dia em que o certificado expirar.
- `/td sha256` — algoritmo do carimbo de tempo
- `/fd sha256` — algoritmo da assinatura
- `/v` — modo verboso; aceita vários arquivos em sequência (assinatura em lote,
  com um único PIN para todos)

O `signtool.exe` já existe nesta máquina, vindo do Windows SDK:

```
C:\Program Files (x86)\Windows Kits\10\bin\10.0.26100.0\x64\signtool.exe
```

Se o cartão tiver PIN, o SimplySign Desktop pede o PIN na primeira assinatura da
sessão.

### Verificar

```
signtool verify /pa /all scanfile.exe
```

Saída esperada:

```
File: scanfile.exe
Index  Algorithm  Timestamp
========================================
0      sha256     RFC3161

Successfully verified: scanfile.exe
```

E, para conferir pelo lado do Windows:

```powershell
Get-AuthenticodeSignature .\scanfile.exe | Format-List Status, SignerCertificate
```

`Status` precisa ser `Valid`.

### Não faça assinatura dupla

O manual da Certum documenta assinatura dupla SHA-1 + SHA-256 para compatibilidade
com Windows 7. O ScanFile exige Windows 10 ou superior; SHA-1 hoje só atrapalha.

## 7. Automação no CI

**Pela SignPath, isto já vem resolvido.** A assinatura acontece na infraestrutura
deles, disparada pelo conector de GitHub: o workflow envia o artefato, a plataforma
confere que ele saiu do repositório, do branch e do build agent esperados
(*origin verification*), assina e devolve. Nada de segredo de assinatura nos
GitHub Secrets, e o registro de auditoria é automático. É o argumento técnico mais
forte a favor dessa rota, além do preço.

### Rota Certum — por que ela não é direta

O SimplySign exige um **token TOTP gerado no celular** para abrir a sessão. Isso é,
de propósito, hostil à automação: a chave privada só é liberada depois da
autenticação. Três caminhos:

**(a) Assinar localmente — recomendado agora.** O CI compila e publica o artefato;
você baixa, assina na sua máquina com o SimplySign conectado, e sobe o binário
assinado na Release. Uma vez por versão, custa dois minutos. É o caminho honesto
enquanto o volume de releases for baixo.

**(b) Runner auto-hospedado com SimplySign Desktop.** O QR code da configuração do
SimplySign contém um URI `otpauth://` padrão, do qual dá para gerar o TOTP por
script e alimentar o SimplySign Desktop automaticamente. Funciona, e existe gente
fazendo. Mas guarda o segredo TOTP numa máquina de build: só faça isso num runner
que **você** controla, nunca num runner compartilhado, e nunca com o segredo em
GitHub Secrets de repositório público.

**(c) Azure Artifact Signing** (ex-Trusted Signing), US$ 9,99/mês, integra
nativamente no GitHub Actions e resolve a automação de vez. Hoje aceita
desenvolvedor individual apenas nos EUA e no Canadá; para o Brasil, só como pessoa
jurídica na UE/EUA/Canadá/Reino Unido. Fica como plano B para o dia em que o
projeto virar comercial e o certificado open source deixar de servir.

Quando o certificado existir, o passo no `.github/workflows/ci-cd.yml` entra logo
após "Compilar scanfile.exe com Injeção de Versão" e antes de "Empacotar Release",
para que o ZIP e o checksum SHA-256 cubram o binário **já assinado**. A ordem
importa: assinar depois de gerar o checksum invalida o checksum.

## 8. Pendências no repositório antes de pedir o certificado

1. **Recurso VERSIONINFO e manifesto.** Sem custo e independente do certificado —
   e **obrigatório** na rota SignPath, que exige nome de produto e versão
   preenchidos em todo binário assinado.
   Gerar um `.syso` com `go-winres` ou `goversioninfo`, preenchendo CompanyName,
   ProductName, FileDescription, LegalCopyright, OriginalFilename e FileVersion
   casada com a tag da release. No manifesto, `requestedExecutionLevel` em
   `asInvoker` — nunca `requireAdministrator`: o app já eleva sob demanda, que é o
   padrão correto e o mais bem visto pela heurística.

2. **Trocar o fallback `rundll32 url.dll,FileProtocolHandler`** por `ShellExecuteW`
   com o verbo `open` (`main.go`). `rundll32` executando handler de URL é padrão
   LOLBin catalogado; a API normal não pontua.

3. **Página pública do projeto**, com informação sobre o autor e o produto. A
   Certum exige URL de projeto open source publicamente verificável, e o Programa
   de Allowlist da Kaspersky (gratuito) exige site ativo com informação da empresa
   e endereço legal. Um README no GitHub provavelmente basta para a Certum; para a
   Kaspersky, provavelmente não.

4. **Licença OSI clara e visível** na raiz do repositório — é o que sustenta a
   afirmação de que o projeto é open source. **Feito:** `LICENSE` traz a
   GPL-3.0 completa (Q41), o `README.md` explica os termos, e `main.go` carrega o
   aviso de copyright. O `--version` imprime o aviso legal exigido pela GPL para
   um programa interativo que se anuncia.

## 9. Depois de assinar

1. `signtool verify /pa /all scanfile.exe` → `Successfully verified`.
2. `Get-AuthenticodeSignature` → `Status: Valid`.
3. Rodar o binário assinado e conferir o grupo de confiança na Kaspersky:
   **Configurações → Segurança → Prevenção de Intrusões → Gerenciar aplicativos**,
   procurando `scanfile.exe`.
4. Inscrever o projeto no **Programa de Allowlist da Kaspersky** — gratuito,
   pensado exatamente para evitar falso positivo em software legítimo. Requisitos:
   site ativo com dados e endereço legal da empresa; o software não pode imitar
   interface de terceiros nem mensagens do sistema; e a assinatura digital não pode
   ser compartilhada com outro fornecedor nem estar comprometida.
5. Manter o **mesmo certificado** nas releases seguintes. Trocar de certificado
   zera a reputação acumulada.

## 10. Efeito colateral na máquina de desenvolvimento

Vale saber, porque custa tempo e parece bug do projeto: nesta máquina, com a
Kaspersky ativa, `go test ./pkg/indexer/` falha de forma reprodutível com

```
scanfile/pkg/indexer.test: open ...\go-build...\b001\indexer.test.exe: Acesso negado.
FAIL	scanfile/pkg/indexer [build failed]
```

O que apurei:

- **Não é detecção.** `avp.com SCAN` no mesmo binário devolve `ok`, `0 detected`.
- **Não é o código.** Compilando o teste com `go test -c` e rodando o executável à
  mão, ele passa: `PASS`.
- **Não é a pasta.** Acontece igual com `GOTMPDIR` apontando para outro volume.

É a proteção em tempo real segurando o executável recém-escrito o tempo suficiente
para o `go test` falhar ao executá-lo — a mesma desconfiança com binário novo e sem
assinatura que este documento inteiro trata. O `indexer.test.exe` é o maior da
suíte, porque linka `modernc.org/sqlite`, e é o único que estoura o tempo.

Solução na sua máquina: excluir a pasta de cache de build do Go da varredura em
tempo real (`go env GOCACHE` e `%LOCALAPPDATA%\Temp\go-build*`). Isso é
configuração de máquina de desenvolvimento, não muda nada no produto — e some
sozinho quando os binários passarem a ser assinados.

## Fontes

- [SignPath Foundation](https://signpath.org/) e [condições para projetos open source](https://signpath.org/terms.html)
- [SignPath — plataforma para a comunidade open source](https://signpath.io/solutions/open-source-community)
- [CA/Browser Forum — Code Signing Baseline Requirements](https://cabforum.org/working-groups/code-signing/requirements/)
- [Certum — Code Signing: documentos exigidos](https://support.certum.eu/en/code-signing-required-documents/)
- [Certum Store — Open Source Code Signing in the Cloud](https://certum.store/open-source-code-signing-on-simplysign.html)
- [Certum — Code Signing in the cloud: assinatura com signtool e jarsigner (PDF)](https://www.files.certum.eu/documents/manual_en/Signing_with_the_use_of_jarsigner_tool_and_signtool.pdf)
- [Certum — encurtamento da validade dos certificados de Code Signing](https://www.certum.eu/en/news/shortening-code-signing-certificate-validity/)
- [Certum — software e bibliotecas (SimplySign Desktop)](https://support.certum.eu/en/cert-offer-software-and-libraries/)
- [Kaspersky — grupos de confiança de aplicativos](https://support.kaspersky.com/KESWin/12.0/en-US/193496.htm)
- [Kaspersky Allowlist Program](https://www.kaspersky.co.uk/partners/allowlist-program)
- [Azure Artifact Signing — preços](https://azure.microsoft.com/en-us/pricing/details/artifact-signing/)

package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"scanfile/pkg/recycle"
)

func newCtxWithRoot(t *testing.T) (*MCPToolsContext, string) {
	t.Helper()
	dir := t.TempDir()
	tc := NewMCPToolsContext(nil, nil, nil, nil, "")
	tc.SetAllowedRoots([]string{dir})
	tc.RecycleFunc = func(paths []string) recycle.BatchDeleteResult {
		t.Fatalf("RecycleFunc não deveria ser chamada neste teste (paths=%v)", paths)
		return recycle.BatchDeleteResult{}
	}
	return tc, dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// C5: propose_actions must never act on disk, even when the model insists on dry_run=false.
func TestProposeActions_NuncaExecuta(t *testing.T) {
	tc, dir := newCtxWithRoot(t)
	victim := filepath.Join(dir, "vitima.txt")
	writeFile(t, victim, "conteudo que deve sobreviver")

	prop, err := tc.ProposeActions(ProposeActionParams{
		ActionType: "RECYCLE",
		Files:      []string{victim},
		DryRun:     false, // injeção: o modelo pede execução imediata
	})
	if err != nil {
		t.Fatalf("proposta deveria ser registrada: %v", err)
	}

	if !prop.DryRun {
		t.Fatal("a resposta da proposta deve sempre reportar dryRun: true")
	}
	if prop.Executed {
		t.Fatal("a proposta nunca pode nascer executada")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("o arquivo não pode ser tocado no turno do modelo: %v", err)
	}
	if prop.ExpiresAt.Sub(prop.CreatedAt) != ProposalTTL {
		t.Fatalf("a proposta deveria expirar em %v, obtido %v", ProposalTTL, prop.ExpiresAt.Sub(prop.CreatedAt))
	}
}

func TestProposeActions_RecusaCaminhoForaDasRaizes(t *testing.T) {
	tc, dir := newCtxWithRoot(t)
	inside := filepath.Join(dir, "dentro.txt")
	writeFile(t, inside, "ok")

	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "fora.txt")
	writeFile(t, outside, "ok")

	if _, err := tc.ProposeActions(ProposeActionParams{
		ActionType: "RECYCLE",
		Files:      []string{inside, outside},
	}); err == nil {
		t.Fatal("proposta com caminho fora das Raízes Varridas deveria ser recusada")
	}
}

func TestProposeActions_SemRaizesRecusa(t *testing.T) {
	tc := NewMCPToolsContext(nil, nil, nil, nil, "")
	_, err := tc.ProposeActions(ProposeActionParams{
		ActionType: "RECYCLE",
		Files:      []string{`C:\qualquer.txt`},
	})
	if err == nil || !ErrorIsNoRoots(err) {
		t.Fatalf("esperado recusa por ausência de Raízes Varridas, obtido: %v", err)
	}
}

func TestProposeActions_TipoInvalido(t *testing.T) {
	tc, dir := newCtxWithRoot(t)
	f := filepath.Join(dir, "a.txt")
	writeFile(t, f, "x")

	if _, err := tc.ProposeActions(ProposeActionParams{ActionType: "DELETE", Files: []string{f}}); err == nil {
		t.Fatal("DELETE não é um tipo de ação suportado pelo Assistente")
	}
}

func TestExecuteProposal_RecycleUsaFuncaoInjetada(t *testing.T) {
	dir := t.TempDir()
	tc := NewMCPToolsContext(nil, nil, nil, nil, "")
	tc.SetAllowedRoots([]string{dir})

	f := filepath.Join(dir, "lixo.txt")
	writeFile(t, f, "1234567890")

	var got []string
	tc.RecycleFunc = func(paths []string) recycle.BatchDeleteResult {
		got = append(got, paths...)
		return recycle.BatchDeleteResult{TotalRequested: len(paths), SuccessCount: len(paths), FreedBytes: 10}
	}

	prop, err := tc.ProposeActions(ProposeActionParams{ActionType: "RECYCLE", Files: []string{f}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatal("propor não pode chamar a Reciclagem")
	}

	done, err := tc.ExecuteProposal(prop.ID)
	if err != nil {
		t.Fatalf("execução aprovada deveria funcionar: %v", err)
	}
	if !done.Executed {
		t.Fatal("proposta deveria estar marcada como executada")
	}
	if len(got) != 1 || got[0] != filepath.Clean(f) {
		t.Fatalf("RecycleFunc recebeu %v", got)
	}
}

func TestExecuteProposal_RecycleFalhaNaoMarcaExecutada(t *testing.T) {
	dir := t.TempDir()
	tc := NewMCPToolsContext(nil, nil, nil, nil, "")
	tc.SetAllowedRoots([]string{dir})
	f := filepath.Join(dir, "lixo.txt")
	writeFile(t, f, "x")

	tc.RecycleFunc = func(paths []string) recycle.BatchDeleteResult {
		return recycle.BatchDeleteResult{
			TotalRequested: len(paths),
			FailedCount:    len(paths),
			Errors:         []string{"volume sem Lixeira disponível"},
		}
	}

	prop, err := tc.ProposeActions(ProposeActionParams{ActionType: "RECYCLE", Files: []string{f}})
	if err != nil {
		t.Fatal(err)
	}
	done, err := tc.ExecuteProposal(prop.ID)
	if err == nil {
		t.Fatal("erro da Lixeira deveria ser propagado")
	}
	if done.Executed {
		t.Fatal("proposta com falha não pode ser marcada como executada")
	}
	if len(done.Errors) == 0 {
		t.Fatal("os erros por arquivo deveriam ser coletados")
	}
}

func TestExecuteProposal_MoveRecusaDestinoExistente(t *testing.T) {
	dir := t.TempDir()
	tc := NewMCPToolsContext(nil, nil, nil, nil, "")
	tc.SetAllowedRoots([]string{dir})

	src := filepath.Join(dir, "origem", "relatorio.pdf")
	writeFile(t, src, "conteudo novo")

	destDir := filepath.Join(dir, "destino")
	existing := filepath.Join(destDir, "relatorio.pdf")
	writeFile(t, existing, "conteudo antigo que nao pode ser perdido")

	prop, err := tc.ProposeActions(ProposeActionParams{
		ActionType:  "MOVE",
		Files:       []string{src},
		Destination: destDir,
	})
	if err != nil {
		t.Fatal(err)
	}

	done, err := tc.ExecuteProposal(prop.ID)
	if err == nil {
		t.Fatal("MOVE com destino existente deveria falhar")
	}
	if done.Executed {
		t.Fatal("proposta com erro não pode ser marcada como executada")
	}

	data, readErr := os.ReadFile(existing)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "conteudo antigo que nao pode ser perdido" {
		t.Fatal("o arquivo de destino foi sobrescrito")
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("a origem deveria permanecer intacta")
	}
	if len(done.Errors) != 1 || !strings.Contains(done.Errors[0], "destino já existe") {
		t.Fatalf("erro por arquivo inesperado: %v", done.Errors)
	}
}

func TestExecuteProposal_MoveSucesso(t *testing.T) {
	dir := t.TempDir()
	tc := NewMCPToolsContext(nil, nil, nil, nil, "")
	tc.SetAllowedRoots([]string{dir})

	src := filepath.Join(dir, "origem", "nota.txt")
	writeFile(t, src, "dados")
	destDir := filepath.Join(dir, "destino")

	prop, err := tc.ProposeActions(ProposeActionParams{ActionType: "MOVE", Files: []string{src}, Destination: destDir})
	if err != nil {
		t.Fatal(err)
	}
	done, err := tc.ExecuteProposal(prop.ID)
	if err != nil {
		t.Fatalf("MOVE válido deveria funcionar: %v", err)
	}
	if !done.Executed {
		t.Fatal("proposta deveria estar executada")
	}
	if _, err := os.Stat(filepath.Join(destDir, "nota.txt")); err != nil {
		t.Fatalf("arquivo deveria ter sido movido: %v", err)
	}
}

func TestExecuteProposal_TagGravaSidecar(t *testing.T) {
	dir := t.TempDir()
	tc := NewMCPToolsContext(nil, nil, nil, nil, "")
	tc.SetAllowedRoots([]string{dir})

	f := filepath.Join(dir, "doc.txt")
	writeFile(t, f, "x")

	prop, err := tc.ProposeActions(ProposeActionParams{ActionType: "TAG", Files: []string{f}, Category: "Financeiro"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f + ".scanfile_meta.json"); err == nil {
		t.Fatal("propor TAG não pode gravar sidecar")
	}

	if _, err := tc.ExecuteProposal(prop.ID); err != nil {
		t.Fatalf("TAG aprovada deveria funcionar: %v", err)
	}
	if _, err := os.Stat(f + ".scanfile_meta.json"); err != nil {
		t.Fatalf("sidecar deveria existir após aprovação: %v", err)
	}
}

func TestExecuteProposal_Expirada(t *testing.T) {
	dir := t.TempDir()
	tc := NewMCPToolsContext(nil, nil, nil, nil, "")
	tc.SetAllowedRoots([]string{dir})
	f := filepath.Join(dir, "a.txt")
	writeFile(t, f, "x")

	prop, err := tc.ProposeActions(ProposeActionParams{ActionType: "MARK_REVIEW", Files: []string{f}})
	if err != nil {
		t.Fatal(err)
	}

	// Envelhece a proposta artificialmente além do TTL.
	tc.proposalsMu.Lock()
	tc.proposals[prop.ID].CreatedAt = time.Now().Add(-ProposalTTL - time.Minute)
	tc.proposals[prop.ID].ExpiresAt = time.Now().Add(-time.Minute)
	tc.proposalsMu.Unlock()

	if _, ok := tc.GetProposal(prop.ID); ok {
		t.Fatal("proposta expirada não deveria ser encontrada")
	}
	if _, err := tc.ExecuteProposal(prop.ID); err == nil {
		t.Fatal("proposta expirada não pode ser executada")
	}
}

// Duas Propostas criadas no mesmo tique do relógio não podem colidir de ID: o
// relógio do Windows tem resolução de milissegundos e uma colisão faria a segunda
// Proposta substituir a primeira no mapa de pendentes.
func TestProposeActions_IDsUnicosEmRajada(t *testing.T) {
	dir := t.TempDir()
	tc := NewMCPToolsContext(nil, nil, nil, nil, "")
	tc.SetAllowedRoots([]string{dir})

	f := filepath.Join(dir, "a.txt")
	writeFile(t, f, "x")

	const total = 200
	vistos := make(map[string]bool, total)
	for i := 0; i < total; i++ {
		prop, err := tc.ProposeActions(ProposeActionParams{ActionType: "MARK_REVIEW", Files: []string{f}})
		if err != nil {
			t.Fatal(err)
		}
		if vistos[prop.ID] {
			t.Fatalf("ID de Proposta repetido na rajada: %s", prop.ID)
		}
		vistos[prop.ID] = true
	}

	// Todas continuam recuperáveis: nenhuma foi sobrescrita.
	for id := range vistos {
		if _, ok := tc.GetProposal(id); !ok {
			t.Fatalf("proposta %s sumiu do mapa de pendentes", id)
		}
	}
}

// A interface pode ler uma Proposta enquanto o servidor executa outra aprovada.
func TestExecuteProposal_ConcorrenciaSegura(t *testing.T) {
	dir := t.TempDir()
	tc := NewMCPToolsContext(nil, nil, nil, nil, "")
	tc.SetAllowedRoots([]string{dir})

	var recycled int64
	tc.RecycleFunc = func(paths []string) recycle.BatchDeleteResult {
		atomic.AddInt64(&recycled, int64(len(paths)))
		return recycle.BatchDeleteResult{TotalRequested: len(paths), SuccessCount: len(paths)}
	}

	ids := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		f := filepath.Join(dir, fmt.Sprintf("arquivo_%02d.txt", i))
		writeFile(t, f, "x")
		prop, err := tc.ProposeActions(ProposeActionParams{ActionType: "RECYCLE", Files: []string{f}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, prop.ID)
	}

	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, _ = tc.ExecuteProposal(id)
		}(id)

		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_, _ = tc.GetProposal(id)
			}
		}(id)
	}
	wg.Wait()

	if got := atomic.LoadInt64(&recycled); got != 20 {
		t.Fatalf("cada Proposta deveria reciclar exatamente uma vez, total %d", got)
	}
	for _, id := range ids {
		p, ok := tc.GetProposal(id)
		if !ok || !p.Executed {
			t.Fatalf("proposta %s deveria constar como executada", id)
		}
	}
}

func TestExecuteProposal_Idempotente(t *testing.T) {
	dir := t.TempDir()
	tc := NewMCPToolsContext(nil, nil, nil, nil, "")
	tc.SetAllowedRoots([]string{dir})
	f := filepath.Join(dir, "a.txt")
	writeFile(t, f, "x")

	calls := 0
	tc.RecycleFunc = func(paths []string) recycle.BatchDeleteResult {
		calls++
		return recycle.BatchDeleteResult{TotalRequested: len(paths), SuccessCount: len(paths)}
	}

	prop, err := tc.ProposeActions(ProposeActionParams{ActionType: "RECYCLE", Files: []string{f}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tc.ExecuteProposal(prop.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tc.ExecuteProposal(prop.ID); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("a Reciclagem deveria rodar uma única vez, rodou %d", calls)
	}
}

// Um lote inteiramente recusado (Pasta Protegida, volume sem Lixeira) não tirou
// nada do disco: a Proposta não pode ser dada como executada, o espaço liberado
// é zero, o motivo da recusa chega a quem chamou e a aprovação pode ser
// tentada de novo depois que a causa for resolvida.
func TestExecuteProposal_RecycleTudoRecusadoNaoMarcaExecutada(t *testing.T) {
	dir := t.TempDir()
	tc := NewMCPToolsContext(nil, nil, nil, nil, "")
	tc.SetAllowedRoots([]string{dir})

	protegido := filepath.Join(dir, "protegido.txt")
	writeFile(t, protegido, "conteudo que nao pode sumir")
	semLixeira := filepath.Join(dir, "rede.txt")
	writeFile(t, semLixeira, "conteudo que nao pode sumir")

	chamadas := 0
	tc.RecycleFunc = func(paths []string) recycle.BatchDeleteResult {
		chamadas++
		res := recycle.BatchDeleteResult{TotalRequested: len(paths), RefusedCount: len(paths)}
		for i, p := range paths {
			motivo := "a pasta é uma Pasta Protegida"
			if i > 0 {
				motivo = "o volume não tem Lixeira disponível"
			}
			res.Items = append(res.Items, recycle.ItemResult{
				Path:   p,
				Status: recycle.StatusRefused,
				Reason: motivo,
			})
		}
		return res
	}

	prop, err := tc.ProposeActions(ProposeActionParams{
		ActionType: "RECYCLE",
		Files:      []string{protegido, semLixeira},
	})
	if err != nil {
		t.Fatal(err)
	}

	done, err := tc.ExecuteProposal(prop.ID)
	if err == nil {
		t.Fatal("um lote totalmente recusado deveria devolver erro a quem chamou")
	}
	if done.Executed {
		t.Fatal("recusa não é execução: a Proposta não pode ficar marcada como executada")
	}
	if done.SucceededCount != 0 || done.RefusedCount != 2 {
		t.Fatalf("contagens erradas: %d executados, %d recusados", done.SucceededCount, done.RefusedCount)
	}
	if done.FreedBytes != 0 {
		t.Fatalf("nada saiu do disco, freedBytes deveria ser 0, obtive %d", done.FreedBytes)
	}
	motivos := strings.Join(done.Errors, " | ")
	if !strings.Contains(motivos, "Pasta Protegida") || !strings.Contains(motivos, "Lixeira") {
		t.Fatalf("o motivo das recusas não chegou a quem chamou: %v", done.Errors)
	}
	for _, f := range []string{protegido, semLixeira} {
		if _, statErr := os.Stat(f); statErr != nil {
			t.Fatalf("arquivo recusado não pode sumir do disco: %v", statErr)
		}
	}

	// A Proposta continua pendente: o usuário pode aprovar de novo depois de
	// resolver a causa da recusa, e a Reciclagem roda outra vez.
	if _, err := tc.ExecuteProposal(prop.ID); err == nil {
		t.Fatal("a segunda tentativa também deveria reportar a recusa")
	}
	if chamadas != 2 {
		t.Fatalf("a Proposta ficou travada como executada: RecycleFunc rodou %d vez(es)", chamadas)
	}
}

// Numa execução parcial o espaço liberado é só o dos itens que realmente
// saíram, e a Proposta fica executada (não dá para reciclar de novo o que já foi).
func TestExecuteProposal_RecycleParcialContaSomenteOQueSaiu(t *testing.T) {
	dir := t.TempDir()
	tc := NewMCPToolsContext(nil, nil, nil, nil, "")
	tc.SetAllowedRoots([]string{dir})

	saiu := filepath.Join(dir, "saiu.txt")
	writeFile(t, saiu, "0123456789")
	ficou := filepath.Join(dir, "ficou.txt")
	writeFile(t, ficou, "0123456789")

	tc.RecycleFunc = func(paths []string) recycle.BatchDeleteResult {
		return recycle.BatchDeleteResult{
			TotalRequested: len(paths),
			SuccessCount:   1,
			RefusedCount:   1,
			FreedBytes:     10,
			Items: []recycle.ItemResult{
				{Path: paths[0], Status: recycle.StatusRecycled},
				{Path: paths[1], Status: recycle.StatusRefused, Reason: "a pasta é uma Pasta Protegida"},
			},
		}
	}

	prop, err := tc.ProposeActions(ProposeActionParams{ActionType: "RECYCLE", Files: []string{saiu, ficou}})
	if err != nil {
		t.Fatal(err)
	}
	if prop.TotalBytes != 20 {
		t.Fatalf("a Proposta deveria somar 20 bytes, obtive %d", prop.TotalBytes)
	}

	done, err := tc.ExecuteProposal(prop.ID)
	if err == nil {
		t.Fatal("a recusa parcial deveria ser reportada a quem chamou")
	}
	if !done.Executed {
		t.Fatal("um item saiu de verdade: a Proposta está executada")
	}
	if done.FreedBytes != 10 {
		t.Fatalf("freedBytes deveria refletir só o que saiu (10), obtive %d", done.FreedBytes)
	}
	if done.SucceededCount != 1 || done.RefusedCount != 1 {
		t.Fatalf("contagens erradas: %d executados, %d recusados", done.SucceededCount, done.RefusedCount)
	}
}

// MOVE e TAG seguem a mesma regra: nada saiu do lugar, nada foi executado.
func TestExecuteProposal_MoveTudoRecusadoPermiteNovaTentativa(t *testing.T) {
	dir := t.TempDir()
	tc := NewMCPToolsContext(nil, nil, nil, nil, "")
	tc.SetAllowedRoots([]string{dir})

	src := filepath.Join(dir, "origem", "nota.txt")
	writeFile(t, src, "dados")
	destDir := filepath.Join(dir, "destino")
	writeFile(t, filepath.Join(destDir, "nota.txt"), "ja existe")

	prop, err := tc.ProposeActions(ProposeActionParams{ActionType: "MOVE", Files: []string{src}, Destination: destDir})
	if err != nil {
		t.Fatal(err)
	}
	done, err := tc.ExecuteProposal(prop.ID)
	if err == nil {
		t.Fatal("MOVE com destino ocupado deveria devolver erro")
	}
	if done.Executed || done.SucceededCount != 0 || done.RefusedCount != 1 {
		t.Fatalf("MOVE recusado marcou execução: executed=%v, %d ok, %d recusados",
			done.Executed, done.SucceededCount, done.RefusedCount)
	}
	if done.FreedBytes != 0 {
		t.Fatalf("nada saiu do lugar, freedBytes deveria ser 0, obtive %d", done.FreedBytes)
	}

	// Resolvida a causa (o destino foi liberado), a mesma Proposta executa.
	if err := os.Remove(filepath.Join(destDir, "nota.txt")); err != nil {
		t.Fatal(err)
	}
	done, err = tc.ExecuteProposal(prop.ID)
	if err != nil {
		t.Fatalf("a nova tentativa deveria funcionar: %v", err)
	}
	if !done.Executed || done.SucceededCount != 1 {
		t.Fatalf("a nova tentativa não moveu o arquivo: %+v", done)
	}
	if _, err := os.Stat(filepath.Join(destDir, "nota.txt")); err != nil {
		t.Fatalf("arquivo deveria ter sido movido: %v", err)
	}
}

func TestExecuteProposal_TagTudoFalhouNaoMarcaExecutada(t *testing.T) {
	dir := t.TempDir()
	tc := NewMCPToolsContext(nil, nil, nil, nil, "")
	tc.SetAllowedRoots([]string{dir})

	f := filepath.Join(dir, "sumiu.txt")
	writeFile(t, f, "x")

	prop, err := tc.ProposeActions(ProposeActionParams{ActionType: "TAG", Files: []string{f}, Category: "Financeiro"})
	if err != nil {
		t.Fatal(err)
	}
	// O arquivo some entre a Proposta e a aprovação: a marcação não tem alvo.
	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}

	done, err := tc.ExecuteProposal(prop.ID)
	if err == nil {
		t.Fatal("TAG sem alvo deveria devolver erro")
	}
	if done.Executed || done.SucceededCount != 0 || done.FailedCount != 1 {
		t.Fatalf("TAG falha marcou execução: executed=%v, %d ok, %d falhas",
			done.Executed, done.SucceededCount, done.FailedCount)
	}
}

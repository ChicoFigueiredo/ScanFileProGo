package scanner

import (
	"runtime"
	"testing"
)

func TestThreadOptions(t *testing.T) {
	opts := ThreadOptions()
	if len(opts) == 0 || opts[0] != 0 {
		t.Fatalf("primeira opção deve ser 0 (Auto), obtive %v", opts)
	}
	max := MaxThreads()
	if max != runtime.NumCPU()*4 {
		t.Fatalf("MaxThreads = %d, quer %d", max, runtime.NumCPU()*4)
	}
	prev := 2
	for _, v := range opts[1:] {
		if v < 4 {
			t.Errorf("opção %d abaixo do mínimo 4", v)
		}
		if v > max {
			t.Errorf("opção %d acima de maxThreads %d", v, max)
		}
		if v != prev*2 {
			t.Errorf("opção %d não é o dobro da anterior %d", v, prev)
		}
		prev = v
	}
	if len(opts) > 1 && opts[len(opts)-1]*2 <= max {
		t.Errorf("faltou a potência de 2 seguinte: última %d, max %d", opts[len(opts)-1], max)
	}
}

func TestResolveWorkersClamp(t *testing.T) {
	n := runtime.NumCPU()
	max := n * 4

	if got := ResolveWorkers(0, PhaseMetadata); got != n*2 {
		t.Errorf("Auto na Fase 1 = %d, quer %d", got, n*2)
	}
	if got := ResolveWorkers(0, PhaseHashing); got != n {
		t.Errorf("Auto na Fase 2 = %d, quer %d", got, n)
	}
	if got := ResolveWorkers(-7, PhaseMetadata); got != n*2 {
		t.Errorf("negativo deve cair no Auto, obtive %d", got)
	}
	if got := ResolveWorkers(4, PhaseMetadata); got != 4 {
		t.Errorf("pedido 4 = %d, quer 4", got)
	}
	if got := ResolveWorkers(max*10, PhaseHashing); got != max {
		t.Errorf("pedido acima do teto = %d, quer %d", got, max)
	}
	if got := ResolveWorkers(1, PhaseHashing); got != 1 {
		t.Errorf("pedido 1 = %d, quer 1", got)
	}
}

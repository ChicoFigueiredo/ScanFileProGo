package recycle

import "testing"

func TestCapacityRefusal(t *testing.T) {
	if msg := capacityRefusal(10, 100); msg != "" {
		t.Errorf("item que cabe na Lixeira nao deve ser recusado: %q", msg)
	}
	if msg := capacityRefusal(100, 100); msg != "" {
		t.Errorf("item do tamanho exato da Lixeira deve caber: %q", msg)
	}
	if msg := capacityRefusal(101, 100); msg == "" {
		t.Error("item maior que a Lixeira deve ser recusado")
	}
	if msg := capacityRefusal(1, 0); msg == "" {
		t.Error("Lixeira sem capacidade deve recusar qualquer item")
	}
	if msg := capacityRefusal(1, -1); msg != "" {
		t.Errorf("capacidade desconhecida (negativa) nao deve recusar: %q", msg)
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[int64]string{
		0:             "0 B",
		512:           "512 B",
		1024:          "1,0 KB",
		1536:          "1,5 KB",
		1048576:       "1,0 MB",
		3221225472:    "3,0 GB",
		1099511627776: "1,0 TB",
	}
	for in, want := range cases {
		if got := formatBytes(in); got != want {
			t.Errorf("formatBytes(%d) = %q, esperado %q", in, got, want)
		}
	}
}

func TestBatchDeleteItemsRefusesProtectedPaths(t *testing.T) {
	res := BatchDeleteItems([]string{`C:\Windows`, `C:\`, `D:\System Volume Information`}, true)

	if res.TotalRequested != 3 {
		t.Errorf("TotalRequested = %d, esperado 3", res.TotalRequested)
	}
	if res.RefusedCount != 3 {
		t.Errorf("RefusedCount = %d, esperado 3", res.RefusedCount)
	}
	if res.SuccessCount != 0 || res.FailedCount != 0 {
		t.Errorf("nenhum item deveria ter sido processado: %+v", res)
	}
	if len(res.Items) != 3 {
		t.Fatalf("Items = %d, esperado 3", len(res.Items))
	}
	for _, item := range res.Items {
		if item.Status != StatusRefused {
			t.Errorf("%s: status = %q, esperado %q", item.Path, item.Status, StatusRefused)
		}
		if item.Reason == "" {
			t.Errorf("%s: recusa sem motivo", item.Path)
		}
	}
}

func TestBatchDeleteItemsRefusesProtectedPathsOnPermanentDelete(t *testing.T) {
	res := BatchDeleteItems([]string{`C:\Windows\System32`}, false)
	if res.RefusedCount != 1 || res.Items[0].Status != StatusRefused {
		t.Fatalf("Exclusão Permanente deve recusar Pasta Protegida, obtido %+v", res)
	}
}

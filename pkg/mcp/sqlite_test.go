package mcp

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func makeTestDB(t *testing.T, rows int) string {
	t.Helper()
	dir := t.TempDir()
	// Nome com espaço e acento para exercitar a montagem do DSN file:.
	dbPath := filepath.Join(dir, "base de dados.sqlite")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE notas (id INTEGER PRIMARY KEY, titulo TEXT, valor REAL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE anexos (id INTEGER PRIMARY KEY, nota_id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < rows; i++ {
		if _, err := db.Exec(`INSERT INTO notas (titulo, valor) VALUES (?, ?)`, fmt.Sprintf("nota %d", i), float64(i)); err != nil {
			t.Fatal(err)
		}
	}
	return dbPath
}

// M10: the modernc driver ignores a query string without the file: prefix, so the
// database used to be opened read/write. This test proves INSERT now fails.
func TestOpenReadOnlySQLite_InsertFalha(t *testing.T) {
	dbPath := makeTestDB(t, 3)

	db, err := openReadOnlySQLite(dbPath)
	if err != nil {
		t.Fatalf("abrir somente leitura falhou: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`INSERT INTO notas (titulo, valor) VALUES ('intruso', 1)`); err == nil {
		t.Fatal("INSERT deveria falhar num banco aberto somente para leitura")
	}
	if _, err := db.Exec(`DROP TABLE notas`); err == nil {
		t.Fatal("DROP TABLE deveria falhar num banco aberto somente para leitura")
	}
	if _, err := db.Exec(`UPDATE notas SET titulo = 'x'`); err == nil {
		t.Fatal("UPDATE deveria falhar num banco aberto somente para leitura")
	}

	// A leitura continua funcionando.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notas`).Scan(&n); err != nil {
		t.Fatalf("SELECT deveria funcionar: %v", err)
	}
	if n != 3 {
		t.Fatalf("esperado 3 linhas, obtido %d", n)
	}
}

func TestReadOnlySQLiteDSN(t *testing.T) {
	dsn := readOnlySQLiteDSN(`C:\dados\meu banco.sqlite`)
	if !strings.HasPrefix(dsn, "file:") {
		t.Fatalf("o DSN precisa do prefixo file: para o driver honrar a query string, obtido %q", dsn)
	}
	if !strings.Contains(dsn, "mode=ro") || !strings.Contains(dsn, "immutable=1") {
		t.Fatalf("DSN sem mode=ro/immutable=1: %q", dsn)
	}
}

func TestValidateUserSQLiteQuery(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{"select simples", "SELECT * FROM notas", false},
		{"select minúsculo", "select id from notas where valor > 1", false},
		{"select com espaços à esquerda", "   SELECT 1", false},
		{"insert bloqueado", "INSERT INTO notas VALUES (1)", true},
		{"update bloqueado", "UPDATE notas SET titulo='x'", true},
		{"delete bloqueado", "DELETE FROM notas", true},
		{"drop bloqueado", "DROP TABLE notas", true},
		{"ponto e vírgula bloqueado", "SELECT 1;", true},
		{"encadeamento bloqueado", "SELECT 1; DROP TABLE notas", true},
		{"attach bloqueado", "SELECT * FROM notas WHERE 1=1 ATTACH DATABASE 'x' AS y", true},
		{"attach minúsculo bloqueado", "select * from notas attach database 'x' as y", true},
		{"pragma bloqueado", "SELECT * FROM pragma_table_info('notas')", true},
		{"load_extension bloqueado", "SELECT load_extension('evil.dll')", true},
		{"LOAD_EXTENSION maiúsculo bloqueado", "SELECT LOAD_EXTENSION('evil.dll')", true},
		{"vazio bloqueado", "   ", true},
		{"with cte bloqueado", "WITH x AS (SELECT 1) SELECT * FROM x", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateUserSQLiteQuery(tc.query)
			if tc.wantErr && err == nil {
				t.Fatalf("consulta %q deveria ser recusada", tc.query)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("consulta %q deveria ser aceita: %v", tc.query, err)
			}
		})
	}
}

func TestInspectSQLite_TabelasELimiteDeLinhas(t *testing.T) {
	dbPath := makeTestDB(t, 50)

	res, err := inspectSQLite(context.Background(), dbPath, "SELECT id, titulo FROM notas")
	if err != nil {
		t.Fatalf("inspeção falhou: %v", err)
	}
	if len(res.Tables) != 2 {
		t.Fatalf("esperadas 2 tabelas, obtido %v", res.Tables)
	}
	if len(res.QueryRows) != maxSQLiteQueryRows {
		t.Fatalf("a consulta deveria devolver no máximo %d linhas, obtido %d", maxSQLiteQueryRows, len(res.QueryRows))
	}
	if res.QueryError != "" {
		t.Fatalf("consulta válida não deveria reportar erro: %s", res.QueryError)
	}
}

func TestInspectSQLite_ConsultaProibidaReportaErro(t *testing.T) {
	dbPath := makeTestDB(t, 2)

	res, err := inspectSQLite(context.Background(), dbPath, "DELETE FROM notas")
	if err != nil {
		t.Fatalf("a inspeção do esquema deve continuar funcionando: %v", err)
	}
	if res.QueryError == "" {
		t.Fatal("a consulta proibida deveria ser reportada ao modelo")
	}
	if len(res.QueryRows) != 0 {
		t.Fatal("nenhuma linha deveria ser devolvida para consulta proibida")
	}

	// E o banco permanece intacto.
	db, err := openReadOnlySQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notas`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("o banco foi alterado: %d linhas", n)
	}
}

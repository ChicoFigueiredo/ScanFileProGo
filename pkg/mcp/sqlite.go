package mcp

import (
	"database/sql"
	"fmt"
	"strings"
)

const (
	// maxSQLiteQueryRows caps how many rows a model-supplied query may return.
	maxSQLiteQueryRows = 20
	// sqliteQueryTimeoutSeconds bounds the execution of a model-supplied query.
	sqliteQueryTimeoutSeconds = 5
)

// blockedSQLiteTokens are refused anywhere inside a user query, case-insensitively.
var blockedSQLiteTokens = []string{"attach", "load_extension", "pragma"}

// readOnlySQLiteDSN builds a DSN the modernc driver actually honours. Without the
// "file:" prefix the driver treats the whole string as a plain path and silently
// discards the query string, opening the database read/write (finding M10).
func readOnlySQLiteDSN(dbPath string) string {
	return "file:" + escapeSQLiteURIPath(dbPath) + "?mode=ro&immutable=1"
}

// escapeSQLiteURIPath percent-encodes the characters SQLite's URI parser treats as
// delimiters. Spaces and backslashes are valid inside a SQLite file: URI.
func escapeSQLiteURIPath(p string) string {
	var sb strings.Builder
	sb.Grow(len(p) + 8)
	for i := 0; i < len(p); i++ {
		switch c := p[i]; c {
		case '%':
			sb.WriteString("%25")
		case '?':
			sb.WriteString("%3f")
		case '#':
			sb.WriteString("%23")
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// openReadOnlySQLite opens a SQLite database that cannot be written to.
func openReadOnlySQLite(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", readOnlySQLiteDSN(dbPath))
	if err != nil {
		return nil, err
	}
	// sql.Open is lazy; force a connection so an unreadable file fails here.
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// validateUserSQLiteQuery enforces the read-only contract for queries suggested by
// the model: a single SELECT statement, without statement separators, ATTACH,
// load_extension or PRAGMA.
func validateUserSQLiteQuery(query string) error {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return fmt.Errorf("consulta vazia")
	}

	lower := strings.ToLower(trimmed)

	if !strings.HasPrefix(lower, "select") {
		return fmt.Errorf("apenas consultas SELECT são permitidas nesta ferramenta")
	}
	if strings.ContainsRune(trimmed, ';') {
		return fmt.Errorf("a consulta não pode conter ';' (apenas um único SELECT é aceito)")
	}
	for _, token := range blockedSQLiteTokens {
		if strings.Contains(lower, token) {
			return fmt.Errorf("a consulta contém o termo proibido %q", token)
		}
	}

	return nil
}

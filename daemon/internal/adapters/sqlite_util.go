package adapters

import (
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

func openReadOnlySQLite(path string) (*sql.DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dsn := sqliteReadOnlyDSN(abs)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func sqliteReadOnlyDSN(abs string) string {
	p := filepath.ToSlash(abs)
	query := "mode=ro&_pragma=busy_timeout(2000)"
	if strings.HasPrefix(p, "//") {
		return "file://" + escapeSQLiteURIPath(p) + "?" + query
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + escapeSQLiteURIPath(p) + "?" + query
}

func escapeSQLiteURIPath(p string) string {
	parts := strings.Split(p, "/")
	for i, part := range parts {
		if part == "" {
			continue
		}
		if i == 1 && len(part) == 2 && part[1] == ':' {
			continue
		}
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

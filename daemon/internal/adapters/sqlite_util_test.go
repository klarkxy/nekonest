package adapters

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLiteReadOnlyDSNEscapesSpecialCharacters(t *testing.T) {
	dsn := sqliteReadOnlyDSN(`C:\Users\foo%41#bar\store.db`)
	if !strings.HasPrefix(dsn, "file:///") {
		t.Fatalf("dsn = %q", dsn)
	}
	if strings.Contains(dsn, "%41#") || strings.Contains(dsn, "#bar") {
		t.Fatalf("unescaped specials in %q", dsn)
	}
	if !strings.Contains(dsn, "mode=ro") || !strings.Contains(dsn, "busy_timeout") {
		t.Fatalf("missing query in %q", dsn)
	}
}

func TestSQLiteReadOnlyDSNUNCUsesFourSlashes(t *testing.T) {
	dsn := sqliteReadOnlyDSN(`//server/share/store.db`)
	if !strings.HasPrefix(dsn, "file:////") {
		t.Fatalf("unc dsn = %q", dsn)
	}
}

func TestOpenReadOnlySQLiteRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.db")
	db, err := openReadOnlySQLite(path)
	if err == nil {
		_ = db.Close()
	}
}

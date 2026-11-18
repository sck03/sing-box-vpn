package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func assertConnectionPolicy(t *testing.T, db *sql.DB) {
	t.Helper()
	for name, want := range map[string]int{"foreign_keys": 1, "trusted_schema": 0, "busy_timeout": 5000, "synchronous": 2} {
		var got int
		if err := db.QueryRow("PRAGMA " + name).Scan(&got); err != nil || got != want {
			t.Fatalf("%s = %d, want %d: %v", name, got, want, err)
		}
	}
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal mode %q: %v", mode, err)
	}
	if _, err := db.Exec("INSERT INTO child(parent_id) VALUES(999)"); err == nil {
		t.Fatal("connection accepted an orphan row")
	}
}

func TestPolicySurvivesCancelledQueryAndConnectionReplacement(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "database # with spaces.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE parent(id INTEGER PRIMARY KEY); CREATE TABLE child(parent_id INTEGER REFERENCES parent(id))"); err != nil {
		t.Fatal(err)
	}
	assertConnectionPolicy(t, db)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	var sum int64
	err = db.QueryRowContext(ctx, "WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<100000000) SELECT SUM(x) FROM n").Scan(&sum)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("query cancellation: %v", err)
	}
	assertConnectionPolicy(t, db)
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	err = conn.Raw(func(any) error { return driver.ErrBadConn })
	_ = conn.Close()
	if !errors.Is(err, driver.ErrBadConn) {
		t.Fatal(err)
	}
	assertConnectionPolicy(t, db)
}

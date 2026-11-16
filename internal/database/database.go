// Package database owns the single local control database. Each module owns
// its tables; the connection policy and on-disk format have one owner.
package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const Filename = "control.db"
const SchemaVersion = 6

func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	if err = os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA trusted_schema=OFF; PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA foreign_keys=ON;`); err == nil {
		var version int
		err = db.QueryRow(`PRAGMA user_version`).Scan(&version)
		if err == nil && version != 0 && version != SchemaVersion {
			err = fmt.Errorf("unsupported database; this release requires a new installation directory (schema %d)", SchemaVersion)
		}
		if err == nil {
			_, err = db.Exec(fmt.Sprintf("PRAGMA user_version=%d;", SchemaVersion))
		}
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

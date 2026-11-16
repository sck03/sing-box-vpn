// Package database owns the single local control database. Each module owns
// its tables; the connection policy and on-disk format have one owner.
package database

import (
	"database/sql"
	"errors"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
)

const Filename = "control.db"

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
		if err == nil && version != 0 && version != 4 {
			err = errors.New("unsupported database; v4 requires a new installation directory")
		}
		if err == nil {
			_, err = db.Exec(`PRAGMA user_version=4;`)
		}
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

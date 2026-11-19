// Package database owns the single local control database. Each module owns
// its tables; the connection policy and on-disk format have one owner.
package database

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

const Filename = "control.db"
const SchemaVersion = 15

func Open(path string) (*sql.DB, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
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
	// Driver DSN pragmas run for every physical connection, including replacements
	// after cancellation. A one-off db.Exec silently loses connection-local policy.
	query := url.Values{"_pragma": {"busy_timeout(5000)", "trusted_schema(OFF)", "journal_mode(WAL)", "synchronous(FULL)", "foreign_keys(ON)"}}
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	uri := url.URL{Scheme: "file", Path: uriPath, RawQuery: query.Encode()}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	var version int
	err = db.QueryRow(`PRAGMA user_version`).Scan(&version)
	if err == nil && version != 0 && version != SchemaVersion {
		err = fmt.Errorf("unsupported database; this release requires a new installation directory (schema %d)", SchemaVersion)
	}
	if err == nil && version == 0 {
		_, err = db.Exec(fmt.Sprintf("PRAGMA user_version=%d;", SchemaVersion))
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

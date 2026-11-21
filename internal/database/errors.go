package database

import (
	"errors"

	"github.com/sck03/sing-box-vpn/internal/domain"
	sqlite "modernc.org/sqlite/lib"
)

// CreationError translates only identity collisions. Foreign keys, corruption,
// disk failures and other constraints retain their original diagnostic cause.
func CreationError(err error) error {
	var coded interface{ Code() int }
	if errors.As(err, &coded) {
		switch coded.Code() {
		case sqlite.SQLITE_CONSTRAINT_PRIMARYKEY, sqlite.SQLITE_CONSTRAINT_UNIQUE:
			return domain.ErrAlreadyExists
		}
	}
	return err
}

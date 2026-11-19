package manage

import (
	"errors"
	"path/filepath"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
)

// Standalone refresh/probe operations own this explicit repository lifetime.
// Panel requests instead borrow the repositories from PlatformService.
func openAccountSource(root string) (AccountService, func(), error) {
	accounts, err := accounting.Open(filepath.Join(root, "control.db"))
	if err != nil {
		return AccountService{}, func() {}, err
	}
	control, err := controlstore.Open(filepath.Join(root, "control.db"))
	if err != nil {
		accounts.Close()
		return AccountService{}, func() {}, err
	}
	return AccountService{Root: root, Store: accounts, Control: control}, func() { _ = errors.Join(accounts.Close(), control.Close()) }, nil
}

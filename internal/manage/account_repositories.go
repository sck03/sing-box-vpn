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

// Long-lived services lend repositories; standalone operations own one pair for
// their whole execution, including generation rechecks and account captures.
func subscriptionSource(root string, services []AccountService) (AccountService, func(), error) {
	if len(services) == 0 {
		return openAccountSource(root)
	}
	if len(services) != 1 || services[0].Root != root || services[0].Store == nil || services[0].Control == nil {
		return AccountService{}, func() {}, errors.New("subscription operation requires matching repositories")
	}
	return services[0], func() {}, nil
}

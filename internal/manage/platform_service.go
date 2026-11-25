package manage

import (
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/adminauth"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
	"github.com/sck03/sing-box-vpn/internal/database"
)

type PlatformService struct {
	Root, Core, Runtime string
	Store               *controlstore.Store
	Auth                *adminauth.Store
	Accounts            *accounting.Store
	NoSystem            bool
	mu                  sync.Mutex
	wake                chan struct{}
	statusMu            sync.Mutex
	statusAt            time.Time
	status              []byte
	statusVersion       uint64
	statusLoading       chan struct{}
	certificateMu       sync.Mutex
	certificateRevision int64
	certificateNext     time.Time
	backupBusy          atomic.Bool
	panelMu             sync.RWMutex
	activePanel         *PanelOptions
	workerReady         atomic.Bool
	exportView          subscriptionView
	exportPlans         accountExportCache
	bindingView         accountBindingCache
}

func OpenPlatform(root, core, runtime string, noSystem bool) (*PlatformService, error) {
	path := filepath.Join(root, database.Filename)
	store, err := controlstore.Open(path)
	if err != nil {
		return nil, err
	}
	auth, err := adminauth.Open(path)
	if err != nil {
		store.Close()
		return nil, err
	}
	accounts, err := accounting.Open(path)
	if err != nil {
		store.Close()
		auth.Close()
		return nil, err
	}
	return &PlatformService{Root: root, Core: core, Runtime: runtime, Store: store, Auth: auth, Accounts: accounts, NoSystem: noSystem, wake: make(chan struct{}, 1)}, nil
}
func (s *PlatformService) Close() error {
	return errors.Join(s.Store.Close(), s.Auth.Close(), s.Accounts.Close())
}
func (s *PlatformService) signal() {
	s.invalidateOverview()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

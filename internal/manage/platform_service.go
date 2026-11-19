package manage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/adminauth"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
	"github.com/sck03/sing-box-vpn/internal/database"
	"github.com/sck03/sing-box-vpn/internal/domain"
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
	backupBusy          atomic.Bool
	panelMu             sync.RWMutex
	activePanel         *PanelOptions
	workerReady         atomic.Bool
	exportView          subscriptionView
	exportPlans         accountExportCache
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

func (s *PlatformService) State(ctx context.Context, secret bool) (Object, error) {
	state, err := s.Store.DesiredState(ctx)
	if err != nil {
		return nil, err
	}
	c, err := decodePlatform(state.Config)
	if err != nil {
		return nil, err
	}
	var value any = c
	if !secret {
		data, _ := json.Marshal(c)
		var object any
		if err = json.Unmarshal(data, &object); err != nil {
			return nil, err
		}
		redactPlatform(object)
		value = object
	}
	return Object{"revision": state.Revision, "applied_revision": state.AppliedRevision, "config": value, "root": s.Root}, nil
}
func redactPlatform(v any) {
	switch x := v.(type) {
	case map[string]any:
		for k, value := range x {
			if secretField(k) {
				x[k] = "[redacted]"
			} else {
				redactPlatform(value)
			}
		}
	case []any:
		for _, item := range x {
			redactPlatform(item)
		}
	}
}

func (s *PlatformService) Run(ctx context.Context) error {
	workerLock, err := os.OpenFile(filepath.Join(s.Root, ".platform-worker.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer workerLock.Close()
	if err = lockFile(workerLock); err != nil {
		return errors.New("another platform worker is already running")
	}
	if err := s.reconcilePanelRestart(ctx); err != nil {
		_ = s.Auth.Audit(ctx, "scheduler", "panel-restart-receipt", "", "failed")
	}
	if err := s.reconcileProgramUpgrade(ctx); err != nil {
		_ = s.Auth.Audit(ctx, "scheduler", "upgrade-receipt", "", "failed")
	}
	if err := s.Store.ExpireExternal(ctx, time.Now().Add(-upgradeWaitBudget)); err != nil {
		return err
	}
	if err := s.Store.Recover(ctx); err != nil {
		return err
	}
	// Restore projections from the confirmed database state before serving tasks.
	state, err := s.Store.State(ctx)
	if err != nil {
		return err
	}
	c, err := decodePlatform(state.Applied)
	if err != nil {
		return err
	}
	if err = withStartupLock(ctx, s.Root, func() error {
		if e := recoverSubscriptionRuntime(s.Root); e != nil {
			return e
		}
		files, e := s.build(ctx, c)
		if e != nil {
			return e
		}
		if e = files.json("runtime-revision.json", state.AppliedRevision); e != nil {
			return e
		}
		return publish(s.Root, files)
	}); err != nil {
		return err
	}
	s.workerReady.Store(true)
	defer s.workerReady.Store(false)
	return s.runWorkers(ctx)
}

func (s *PlatformService) Preset(ctx context.Context, protocol, id string) (ManagedNode, error) {
	if !domain.ResourceID.MatchString(id) || !slices.Contains(Protocols, protocol) {
		return ManagedNode{}, errors.New("invalid preset")
	}
	state, err := s.Store.State(ctx)
	if err != nil {
		return ManagedNode{}, err
	}
	c, err := decodePlatform(state.Desired)
	if err != nil {
		return ManagedNode{}, err
	}
	used, ranges, err := allocationPorts(s.Root, c.App, c.VPN)
	if err != nil {
		return ManagedNode{}, err
	}
	p, err := NewProfile(s.Root, used, "", ranges...)
	if err != nil {
		return ManagedNode{}, err
	}
	for _, n := range MakeNodes(p, "") {
		if n.Protocol == protocol {
			n.ID = id
			return n, nil
		}
	}
	return ManagedNode{}, errors.New("preset unavailable")
}

func (s *PlatformService) PrepareCountry(ctx context.Context, c PlatformConfig, cc string) (PlatformConfig, error) {
	c.normalize()
	if err := c.Validate(); err != nil {
		return c, err
	}
	if !countryPattern.MatchString(cc) {
		return c, errors.New("country must be two uppercase letters")
	}
	if _, ok := c.VPN.Profiles[cc]; !ok {
		used, ranges, e := allocationPorts(s.Root, c.App, c.VPN)
		if e != nil {
			return c, e
		}
		p, e := NewProfile(s.Root, used, cc+"-", ranges...)
		if e != nil {
			return c, e
		}
		secret := p.Secrets
		secret.SocksUser, secret.SocksPass = "ygvg-"+cc, hexSecret()
		p.Secrets = Secrets{}
		p.Country = cc
		c.VPN.Profiles[cc] = p
		c.VPNSecrets[cc] = secret
		c.VPN.Order = append(c.VPN.Order, cc)
	}
	c.VPN.ActiveCountry = cc
	p := c.VPNSecrets[cc]
	c.Gateway = merge(c.Gateway, Object{"country": cc, "enabled": true, "mode": "sticky", "fixed_host": "", "listen": fmt.Sprintf("127.0.0.1:%d", c.VPN.ProxyPort), "proxy_user": p.SocksUser, "proxy_pass": p.SocksPass, "data_dir": "/var/lib/yg-vpngate", "run_dir": "/run/yg-vpngate"})
	return c, nil
}

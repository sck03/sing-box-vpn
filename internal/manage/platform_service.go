package manage

import (
	"bytes"
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
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/command"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
	"github.com/sck03/sing-box-vpn/internal/database"
	"github.com/sck03/sing-box-vpn/internal/domain"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
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
}
type PlatformAction struct {
	Operation    string                          `json:"operation"`
	Service      string                          `json:"service,omitempty"`
	ID           string                          `json:"id,omitempty"`
	Scope        string                          `json:"scope,omitempty"`
	Value        string                          `json:"value,omitempty"`
	Config       json.RawMessage                 `json:"config,omitempty"`
	Account      application.AccountCommand      `json:"account,omitempty"`
	Subscription application.SubscriptionCommand `json:"subscription,omitempty"`
	Automatic    bool                            `json:"automatic,omitempty"`
}
type applyInput struct {
	Revision int64                      `json:"revision"`
	Active   []string                   `json:"active"`
	Enabled  []string                   `json:"enabled"`
	Sources  map[string]sub.SourceCache `json:"sources,omitempty"`
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
			if slices.Contains([]string{"uuid", "password", "passwords", "private_key", "reality_private", "private", "token", "token_hash", "key", "secret", "authorization", "headers", "vpn_secrets", "proxy_pass", "socks_pass", "url", "webhook", "content", "client_secret", "pre_shared_key"}, k) {
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

func (s *PlatformService) QueueApply(ctx context.Context, c PlatformConfig, revision int64, actor string) (int64, error) {
	c.normalize()
	if c.Argo.Mode == "fixed" {
		c.App.Argo = Object{"domain": c.Argo.Domain, "inbound": c.Argo.Inbound}
	} else if c.Argo.Mode == "off" || c.Argo.Mode == "" {
		c.App.Argo = Object{}
	}
	if err := c.Validate(); err != nil {
		return 0, err
	}
	if err := s.validateConfiguredPanelPort(c); err != nil {
		return 0, err
	}
	state, err := s.Store.Revisions(ctx)
	if err != nil {
		return 0, err
	}
	if state.Revision != revision {
		return 0, controlstore.ErrConflict
	}
	c.Subscriptions.Revision = uint64(revision + 1)
	// Validate before persisting intent. No service or public artifact changes.
	_, prepared, err := s.prepareBuild(ctx, c)
	if err != nil {
		return 0, err
	}
	// Expensive core validation does not hold the configuration mutation mutex.
	// Recheck the revision under the mutex before capturing/persisting intent;
	// execution validates again against then-current credentials and material.
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err = s.Store.Revisions(ctx)
	if err != nil {
		return 0, err
	}
	if state.Revision != revision {
		return 0, controlstore.ErrConflict
	}
	active := []string{}
	enabled := []string{}
	if !s.NoSystem {
		active, enabled, err = serviceSnapshot(ctx, true)
		if err != nil {
			return 0, err
		}
	}
	data, err := encodePlatform(c)
	if err != nil {
		return 0, err
	}
	input, _ := json.Marshal(applyInput{Revision: revision + 1, Active: active, Enabled: enabled, Sources: prepared})
	var id int64
	err = withStartupLock(ctx, s.Root, func() error {
		if err := s.validateConfiguredPanelPort(c); err != nil {
			return err
		}
		id, err = s.Store.Queue(ctx, "apply", actor, input, revision, data)
		return err
	})
	if err == nil {
		s.signal()
	}
	return id, err
}

func (s *PlatformService) Queue(ctx context.Context, a PlatformAction, actor string) (int64, error) {
	if a.Operation == "gateway" {
		var command struct {
			Op string `json:"op"`
		}
		if err := strictSubscriptionJSON(a.Config, &command); err != nil {
			return 0, err
		}
		if command.Op == "start" || command.Op == "stop" {
			state, err := s.Store.State(ctx)
			if err != nil {
				return 0, err
			}
			if state.Revision != state.AppliedRevision {
				return 0, errors.New("apply or resolve the pending configuration before changing gateway state")
			}
			c, err := decodePlatform(state.Applied)
			if err != nil {
				return 0, err
			}
			if c.VPN.ActiveCountry == "" {
				return 0, errors.New("configure a gateway country first")
			}
			c.Gateway["enabled"] = command.Op == "start"
			return s.QueueApply(ctx, c, state.Revision, actor)
		}
		if command.Op != "swap" && command.Op != "refresh" {
			return 0, errors.New("unsupported gateway action")
		}
	}
	if !slices.Contains([]string{"service", "gateway", "account", "subscription", "certificate-renew", "bbr", "program-upgrade", "runtime-rollback", "outbound-probe"}, a.Operation) {
		return 0, errors.New("unknown platform action")
	}
	if a.Operation == "service" && (!slices.Contains(platformServices, a.Service) || !slices.Contains([]string{"start", "stop", "restart", "enable", "disable"}, a.Value)) {
		return 0, errors.New("unsupported service action")
	}
	if a.Operation == "account" && !slices.Contains([]string{"bind", "rotate", "unbind"}, a.Account.Operation) {
		return 0, errors.New("account task must bind, unbind or rotate credentials")
	}
	if a.Operation == "subscription" && !slices.Contains([]string{"refresh", "check", "deliver"}, a.Subscription.Operation) {
		return 0, errors.New("unsupported subscription task")
	}
	data, err := json.Marshal(a)
	if err != nil {
		return 0, err
	}
	var id int64
	if a.Operation == "outbound-probe" || (a.Operation == "subscription" && a.Subscription.Operation == "check") {
		id, err = s.Store.QueueBackground(ctx, a.Operation, actor, data)
	} else {
		id, err = s.Store.Queue(ctx, a.Operation, actor, data, 0, nil)
	}
	if err == nil {
		s.signal()
	}
	return id, err
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

func (s *PlatformService) apply(ctx context.Context, id int64, input applyInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.Store.State(ctx)
	if err != nil {
		return err
	}
	if input.Revision != state.Revision {
		return controlstore.ErrConflict
	}
	c, err := decodePlatform(state.Desired)
	if err != nil {
		return err
	}
	previous, err := decodePlatform(state.Applied)
	if err != nil {
		return err
	}
	files, err := s.buildConfig(ctx, c, sub.Candidate, input.Sources)
	if err != nil {
		return err
	}
	if err = s.checkRuntime(ctx, files); err != nil {
		return err
	}
	if err = files.json("runtime-revision.json", state.Revision); err != nil {
		return err
	}
	if summary, e := configurationOutcome(files); e != nil {
		return e
	} else if e = s.Store.Result(ctx, id, summary); e != nil {
		return e
	}
	if err = s.Store.Phase(ctx, id, "publishing"); err != nil {
		return err
	}
	err = withStartupLock(ctx, s.Root, func() error { return publish(s.Root, files) })
	if err != nil {
		return err
	}
	if err = s.Store.Phase(ctx, id, "applying-services"); err == nil && !s.NoSystem {
		err = s.applyHost(ctx, c, previous, input.Active, false)
	}
	if err == nil {
		err = s.Store.Applied(ctx, state.Revision)
	}
	if err != nil {
		// Desired remains reviewable. Configuration rollback never touches the
		// accounting/security tables, so revocations cannot be undone here.
		recovery, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		old, e := s.build(recovery, previous)
		if e == nil {
			e = old.json("runtime-revision.json", state.AppliedRevision)
		}
		if e == nil {
			e = withStartupLock(recovery, s.Root, func() error { return publish(s.Root, old) })
		}
		if e == nil && !s.NoSystem {
			e = s.applyHost(recovery, previous, c, input.Active, true)
			e = errors.Join(e, s.restoreEnabled(recovery, input.Enabled))
		}
		phase := "rolled-back"
		if e != nil {
			phase = "rollback-failed"
		}
		_ = s.Store.Phase(recovery, id, phase)
		return errors.Join(err, e)
	}
	return nil
}

func (s *PlatformService) checkRuntime(ctx context.Context, files fileBundle) error {
	if s.Runtime == "" {
		return errors.New("managed runtime binary is required")
	}
	for _, scope := range []string{"normal", "vpngate"} {
		name := "sb.json"
		if scope == "vpngate" {
			name = "vpngate/server.json"
		}
		data := files[name]
		if len(data) == 0 {
			continue
		}
		f, err := os.CreateTemp(s.Root, ".runtime-check-*.json")
		if err != nil {
			return err
		}
		path := f.Name()
		if err = f.Chmod(0600); err == nil {
			_, err = f.Write(data)
		}
		err = errors.Join(err, f.Close())
		if err == nil {
			_, err = command.Output(ctx, nil, s.Runtime, "--check", "--config", path, "--database", filepath.Join(s.Root, database.Filename), "--instance", scope)
		}
		_ = os.Remove(path)
		if err != nil {
			return fmt.Errorf("managed %s configuration rejected: %w", scope, err)
		}
	}
	return nil
}

func (s *PlatformService) execute(ctx context.Context, id int64, a PlatformAction) error {
	if err := s.Store.Phase(ctx, id, "executing"); err != nil {
		return err
	}
	switch a.Operation {
	case "account":
		return s.executeAccount(ctx, id, a.Account)
	case "subscription":
		if a.Automatic {
			if a.Subscription.Operation == "refresh" {
				var result application.RefreshResult
				if err := refreshSubscriptionsResult(ctx, s.Root, s.Core, false, &result); err != nil {
					return err
				}
				data, err := json.Marshal(result)
				if err != nil {
					return err
				}
				return s.Store.Result(ctx, id, data)
			}
			return deliverSubscriptions(ctx, s.Root, "", true, new(bytes.Buffer))
		}
		var out bytes.Buffer
		operationErr := (SubscriptionService{Root: s.Root, Core: s.Core}).Execute(ctx, a.Subscription, &out)
		return s.storeTaskOutput(ctx, id, out.Bytes(), operationErr)
	case "service":
		if s.NoSystem {
			return errors.New("system operations are disabled")
		}
		return s.service(ctx, a.Service, a.Value)
	case "gateway":
		_, err := s.gateway(ctx, "actions", a.Config)
		return err
	case "certificate-renew":
		return s.renewCertificate(ctx, a.ID)
	case "bbr":
		return s.setBBR(ctx, a.Value)
	case "program-upgrade":
		return s.upgradeProgram(ctx, id, a.Value)
	case "runtime-rollback":
		return s.rollbackRuntime(ctx)
	case "outbound-probe":
		return s.probeOutbound(ctx, id, a.ID, a.Value)
	}
	return errors.New("unknown task")
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
	nodes, err := managedNodes(App{Profile: p, Inbounds: MakeInbounds(p, "")})
	if err != nil {
		return ManagedNode{}, err
	}
	for _, n := range nodes {
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

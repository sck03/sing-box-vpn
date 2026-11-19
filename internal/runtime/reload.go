package runtime

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	sjson "github.com/sagernet/sing/common/json"
	"github.com/sck03/sing-box-vpn/internal/domain"
)

// ErrReloadUnrecoverable requires process shutdown: a listener could not be
// restored, so continuing would misrepresent the loaded credential revision.
var ErrReloadUnrecoverable = errors.New("credential listener recovery failed")

// Core owns the process-wide graph and independently scoped managed listeners.
// ReloadCredentials never changes routes, DNS, outbounds or unaffected inbounds.
type Core struct {
	*box.Box
	mu            sync.Mutex
	ctx           context.Context
	raw           []byte
	inbounds      map[string]*reloadableInbound
	ready, closed bool
}

func (c *Core) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return context.Canceled
	}
	if c.ready {
		return errors.New("core already started")
	}
	if err := c.Box.Start(); err != nil {
		return err
	}
	c.ready = true
	return nil
}

func (c *Core) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	err := c.Box.Close()
	for _, entry := range c.inbounds {
		err = errors.Join(err, entry.close())
	}
	return err
}

func (c *Core) ReloadCredentials(credentials []domain.Credential) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || !c.ready {
		return errors.New("core is not running")
	}
	raw, err := BindUsers(c.raw, credentials)
	if err != nil {
		return err
	}
	options, err := sjson.UnmarshalExtendedContext[option.Options](c.ctx, raw)
	if err != nil {
		return err
	}
	if len(options.Inbounds) != len(c.inbounds) {
		return errors.New("credential reload cannot change inbound topology")
	}
	for _, node := range options.Inbounds {
		entry, found := c.inbounds[node.Tag]
		if !found || entry.Type() != node.Type {
			return errors.New("credential reload cannot change inbound topology")
		}
	}
	type change struct {
		entry   *reloadableInbound
		options any
	}
	var applied []change
	for _, node := range options.Inbounds {
		entry := c.inbounds[node.Tag]
		old := entry.options
		if reflect.DeepEqual(old, node.Options) {
			continue
		}
		if err = entry.reload(node.Options); err != nil {
			for i := len(applied) - 1; i >= 0; i-- {
				if rollback := applied[i].entry.reload(applied[i].options); rollback != nil {
					err = errors.Join(err, ErrReloadUnrecoverable, rollback)
				}
			}
			return fmt.Errorf("reload inbound %s: %w", node.Tag, err)
		}
		applied = append(applied, change{entry, old})
	}
	return nil
}

type reloadableInbound struct {
	kind, tag  string
	references []string
	mu         sync.Mutex
	ctx        context.Context
	logger     log.ContextLogger
	factory    func(context.Context, any) (adapter.Inbound, error)
	current    adapter.Inbound
	scope      *adapter.Scope
	options    any
	stages     []adapter.StartStage
	closed     bool
}

func reloadable[T any](entries map[string]*reloadableInbound, constructor inbound.ConstructorFunc[T]) inbound.ConstructorFunc[T] {
	return func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options T) (adapter.Inbound, error) {
		if entries == nil {
			return constructor(ctx, router, logger, tag, options)
		}
		entry := &reloadableInbound{ctx: ctx, logger: logger, options: &options}
		entry.factory = func(child context.Context, raw any) (adapter.Inbound, error) {
			return constructor(child, router, logger, tag, *raw.(*T))
		}
		var err error
		entry.scope = adapter.NewScope(ctx, logger)
		entry.current, err = entry.factory(entry.scope.Context(), entry.options)
		if err != nil {
			_ = entry.scope.Close()
			return nil, err
		}
		entry.kind, entry.tag = entry.current.Type(), entry.current.Tag()
		if referrer, ok := entry.current.(adapter.Referrer); ok {
			entry.references = append([]string(nil), referrer.References()...)
		}
		entries[tag] = entry
		return entry, nil
	}
}

func (r *reloadableInbound) Type() string         { return r.kind }
func (r *reloadableInbound) Tag() string          { return r.tag }
func (r *reloadableInbound) References() []string { return r.references }

func (r *reloadableInbound) Start(stage adapter.StartStage, parent *adapter.Scope) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return context.Canceled
	}
	if len(r.stages) == 0 {
		parent.Add(r.close)
	}
	if err := r.current.Start(stage, r.scope); err != nil {
		return err
	}
	r.stages = append(r.stages, stage)
	return nil
}

func (r *reloadableInbound) close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	return r.scope.Close()
}

func (r *reloadableInbound) prepare(options any) (adapter.Inbound, *adapter.Scope, error) {
	scope := adapter.NewScope(r.ctx, r.logger)
	entry, err := r.factory(scope.Context(), options)
	if err != nil {
		_ = scope.Close()
	}
	return entry, scope, err
}

func (r *reloadableInbound) start(entry adapter.Inbound, scope *adapter.Scope) error {
	for _, stage := range r.stages {
		if err := entry.Start(stage, scope); err != nil {
			return err
		}
	}
	return nil
}

func (r *reloadableInbound) reload(options any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return context.Canceled
	}
	entry, scope, err := r.prepare(options)
	if err != nil {
		return err
	}
	// Construct before interrupting the old listener. Bind only after it closes.
	err = r.scope.Close()
	if err == nil {
		err = r.start(entry, scope)
	}
	if err == nil {
		r.current, r.scope, r.options = entry, scope, options
		return nil
	}
	_ = scope.Close()
	old, recovery, rollback := r.prepare(r.options)
	if rollback == nil {
		rollback = r.start(old, recovery)
	}
	if rollback != nil {
		_ = recovery.Close()
		return errors.Join(err, ErrReloadUnrecoverable, rollback)
	}
	r.current, r.scope = old, recovery
	return err
}

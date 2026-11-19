package runtime

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	sjson "github.com/sagernet/sing/common/json"
	"github.com/sck03/sing-box-vpn/internal/domain"
)

func TestCredentialReloadOnlyReplacesChangedInbound(t *testing.T) {
	raw := []byte(`{"log":{"disabled":true},"inbounds":[{"type":"vless","tag":"one","listen":"127.0.0.1","listen_port":0},{"type":"vless","tag":"two","listen":"127.0.0.1","listen_port":0}],"outbounds":[{"type":"direct","tag":"direct"}]}`)
	credential := domain.Credential{ID: "alice", AccountID: "alice", RuntimeID: "normal", InboundID: "one", Protocol: "vless", Generation: 1, UUID: "aabbccdd-1122-4333-8444-5566778899aa"}
	core, err := New(t.Context(), raw, []domain.Credential{credential}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = core.Close() })
	if err = core.Start(); err != nil {
		t.Fatal(err)
	}
	one, two := core.inbounds["one"], core.inbounds["two"]
	oldOne, oldTwo := one.scope, two.scope
	if err = core.ReloadCredentials([]domain.Credential{credential}); err != nil {
		t.Fatal(err)
	}
	if one.scope != oldOne || two.scope != oldTwo {
		t.Fatal("unchanged users replaced listeners")
	}
	credential.Generation++
	credential.UUID = "bbbbbbbb-1122-4333-8444-5566778899aa"
	if err = core.ReloadCredentials([]domain.Credential{credential}); err != nil {
		t.Fatal(err)
	}
	if one.scope == oldOne || oldOne.Context().Err() == nil {
		t.Fatal("changed inbound was not retired")
	}
	if two.scope != oldTwo || oldTwo.Context().Err() != nil {
		t.Fatal("unaffected inbound was interrupted")
	}
	if err = core.ReloadCredentials(nil); err != nil {
		t.Fatal(err)
	}
	if two.scope != oldTwo {
		t.Fatal("unbind interrupted another inbound")
	}
}

type testInbound struct {
	kind, tag string
	fail      bool
	live      *int
}

func (i *testInbound) Type() string {
	if i.kind != "" {
		return i.kind
	}
	return "test"
}
func (i *testInbound) Tag() string {
	if i.tag != "" {
		return i.tag
	}
	return "test"
}
func (*testInbound) References() []string { return []string{"proxy"} }
func (i *testInbound) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage != adapter.StartStateStart {
		return nil
	}
	if i.fail {
		return errors.New("listener unavailable")
	}
	*i.live++
	scope.Add(func() error { *i.live--; return nil })
	return nil
}

func TestCredentialReloadRecoversAndFailsClosed(t *testing.T) {
	for _, fatal := range []bool{false, true} {
		t.Run(map[bool]string{false: "recover", true: "fail-closed"}[fatal], func(t *testing.T) {
			live, calls := 0, 0
			logger := log.NewNOPFactory().Logger()
			entries := map[string]*reloadableInbound{}
			constructor := func(_ context.Context, _ adapter.Router, _ log.ContextLogger, _ string, fail bool) (adapter.Inbound, error) {
				calls++
				return &testInbound{fail: fail || fatal && calls > 1, live: &live}, nil
			}
			entry, err := reloadable(entries, constructor)(t.Context(), nil, logger, "test", false)
			if err != nil {
				t.Fatal(err)
			}
			if references := entry.(adapter.Referrer).References(); len(references) != 1 || references[0] != "proxy" {
				t.Fatal("lost inbound outbound references", references)
			}
			parent := adapter.NewScope(t.Context(), logger)
			defer parent.Close()
			if err = entry.Start(adapter.StartStateStart, parent); err != nil {
				t.Fatal(err)
			}
			fail := true
			err = entries["test"].reload(&fail)
			if err == nil || errors.Is(err, ErrReloadUnrecoverable) != fatal {
				t.Fatal(err)
			}
			if want := map[bool]int{false: 1, true: 0}[fatal]; live != want {
				t.Fatalf("live=%d want=%d", live, want)
			}
		})
	}
}

func TestRetiredInboundClosesOnlyItsConnections(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	r := &guardedRouter{lifetime: ctx}
	a, b := net.Pipe()
	defer b.Close()
	stop := r.watch(t.Context(), a)
	defer stop()
	cancel()
	if _, err := b.Read(make([]byte, 1)); err == nil {
		t.Fatal("retired connection stayed open")
	}
}

func TestReloadConstructionFailureKeepsListener(t *testing.T) {
	logger := log.NewNOPFactory().Logger()
	live := 0
	entries := map[string]*reloadableInbound{}
	constructor := func(_ context.Context, _ adapter.Router, _ log.ContextLogger, _ string, fail bool) (adapter.Inbound, error) {
		if fail {
			return nil, errors.New("invalid replacement options")
		}
		return &testInbound{live: &live}, nil
	}
	entry, err := reloadable(entries, constructor)(t.Context(), nil, logger, "test", false)
	if err != nil {
		t.Fatal(err)
	}
	parent := adapter.NewScope(t.Context(), logger)
	defer parent.Close()
	if err = entry.Start(adapter.StartStateStart, parent); err != nil {
		t.Fatal(err)
	}
	old := entries["test"].scope
	fail := true
	if err = entries["test"].reload(&fail); err == nil {
		t.Fatal("construction succeeded")
	}
	if old.Context().Err() != nil || entries["test"].scope != old || live != 1 {
		t.Fatal("construction failure interrupted existing listener")
	}
}

func TestReloadAndShutdownAreSerialized(t *testing.T) {
	raw := []byte(`{"log":{"disabled":true},"inbounds":[{"type":"vless","tag":"one","listen":"127.0.0.1","listen_port":0}],"outbounds":[{"type":"direct"}]}`)
	core, err := New(t.Context(), raw, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = core.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 30; i++ {
			_ = core.ReloadCredentials(nil)
		}
	}()
	if err = core.Close(); err != nil {
		t.Fatal(err)
	}
	<-done
	if core.inbounds["one"].scope.Context().Err() == nil {
		t.Fatal("shutdown leaked inbound")
	}
}

func TestMultiInboundFailureRestoresEarlierChanges(t *testing.T) {
	raw := []byte(`{"inbounds":[{"type":"vless","tag":"one"},{"type":"vless","tag":"two"}]}`)
	ctx := Context(t.Context(), nil)
	options, err := sjson.UnmarshalExtendedContext[option.Options](ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	logger := log.NewNOPFactory().Logger()
	live := 0
	entries := map[string]*reloadableInbound{}
	constructor := func(_ context.Context, _ adapter.Router, _ log.ContextLogger, tag string, options option.VLESSInboundOptions) (adapter.Inbound, error) {
		return &testInbound{kind: "vless", tag: tag, live: &live, fail: tag == "two" && len(options.Users) > 0}, nil
	}
	parent := adapter.NewScope(ctx, logger)
	defer parent.Close()
	for _, node := range options.Inbounds {
		entry, e := reloadable(entries, constructor)(ctx, nil, logger, node.Tag, *node.Options.(*option.VLESSInboundOptions))
		if e != nil {
			t.Fatal(e)
		}
		if e = entry.Start(adapter.StartStateStart, parent); e != nil {
			t.Fatal(e)
		}
	}
	core := &Core{ctx: ctx, raw: raw, inbounds: entries, ready: true}
	credentials := []domain.Credential{}
	for _, tag := range []string{"one", "two"} {
		credentials = append(credentials, domain.Credential{ID: tag, AccountID: "account", RuntimeID: "normal", InboundID: tag, Protocol: "vless", Generation: 1, UUID: "aabbccdd-1122-4333-8444-5566778899aa"})
	}
	err = core.ReloadCredentials(credentials)
	if err == nil || errors.Is(err, ErrReloadUnrecoverable) {
		t.Fatal(err)
	}
	if live != 2 {
		t.Fatalf("rollback left %d live inbounds", live)
	}
	for _, entry := range entries {
		if len(entry.options.(*option.VLESSInboundOptions).Users) != 0 {
			t.Fatal("earlier inbound was not rolled back")
		}
	}
}

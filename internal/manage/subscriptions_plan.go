package manage

import (
	"context"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/sck03/sing-box-vpn/internal/application"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func subscriptionPlanIdentity(root, core string) (map[string]string, error) {
	identity := map[string]string{}
	for _, name := range []string{subscriptionsConfig, subscriptionsRuntime, subscriptionsLocal} {
		digest, err := fingerprintFile(filepath.Join(root, name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		identity[name] = digest
	}
	digest, err := coreIdentity(core)
	identity["core"] = digest
	return identity, err
}

func (s SubscriptionService) Plan(ctx context.Context, command application.SubscriptionCommand, output io.Writer) error {
	return s.plan(ctx, command, output, buildSubscriptions)
}

type subscriptionCompiler func(context.Context, string, string, sub.Config, sub.Runtime, map[string][]sub.Node) (fileBundle, sub.Snapshot, map[string]sub.Preview, error)

func (s SubscriptionService) plan(ctx context.Context, command application.SubscriptionCommand, output io.Writer, compile subscriptionCompiler) error {
	var candidate *sub.Config
	var prepared map[string]sub.SourceCache
	if len(command.Config) > 0 {
		candidate = new(sub.Config)
		if err := strictSubscriptionJSON(command.Config, candidate); err != nil {
			return err
		}
		var err error
		prepared, err = prepareSubscriptionSources(ctx, s.Root, *candidate)
		if err != nil {
			return err
		}
	} else if command.Operation == "apply" {
		return errors.New("apply requires a configuration")
	}
	var c sub.Config
	var r sub.Runtime
	var local map[string][]sub.Node
	var before map[string]string
	err := withStartupLock(ctx, s.Root, func() error {
		var err error
		c, r, local, err = loadSubscriptions(s.Root)
		if err != nil {
			return err
		}
		before, err = subscriptionPlanIdentity(s.Root, s.Core)
		return err
	})
	if err != nil {
		return err
	}
	if candidate != nil {
		if err := validateSubscriptionsRevision(c, *candidate); err != nil {
			return err
		}
		c = *candidate
	}
	if r.Sources == nil {
		r.Sources = map[string]sub.SourceCache{}
	}
	for id, cache := range prepared {
		r.Sources[id] = cache
	}
	if command.Operation == "apply" {
		c.Revision++
		for key := range r.Sources {
			if !slices.ContainsFunc(c.Sources, func(source sub.Source) bool { return source.ID == key }) {
				delete(r.Sources, key)
			}
		}
		for key := range r.Resources {
			if !slices.ContainsFunc(c.Templates, func(t sub.Template) bool { return key == "template:"+t.ID }) && !slices.ContainsFunc(c.RuleSets, func(r sub.RuleSet) bool { return key == "rules:"+r.ID }) {
				delete(r.Resources, key)
			}
		}
		for key := range r.Deliveries {
			if !slices.ContainsFunc(c.Deliveries, func(d sub.Delivery) bool { return key == d.ID }) {
				delete(r.Deliveries, key)
			}
		}
		r.Record("apply", "", "configuration and subscription snapshot published", time.Now())
	}
	// Compilation and core subprocesses run without the global configuration lock.
	files, snapshot, previews, err := compile(ctx, s.Root, s.Core, c, r, local)
	if err != nil {
		return err
	}
	if command.Operation == "preview" {
		return jsonEncoder(output).Encode(subscriptionPreviewSummary(snapshot, previews))
	}
	return withStartupLock(ctx, s.Root, func() error {
		after, err := subscriptionPlanIdentity(s.Root, s.Core)
		if err != nil {
			return err
		}
		if !maps.Equal(before, after) {
			return application.ErrConflict
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return publish(s.Root, files)
	})
}

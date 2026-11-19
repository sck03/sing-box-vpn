package manage

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

type applyInput struct {
	Revision int64                      `json:"revision"`
	Active   []string                   `json:"active"`
	Enabled  []string                   `json:"enabled"`
	Sources  map[string]sub.SourceCache `json:"sources,omitempty"`
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
	return application.ApplyDeployment(ctx, application.Deployment{
		Phase: func(ctx context.Context, phase string) error { return s.Store.Phase(ctx, id, phase) },
		Publish: func(ctx context.Context) error {
			return withStartupLock(ctx, s.Root, func() error { return publish(s.Root, files) })
		},
		Activate: func(ctx context.Context) error {
			if s.NoSystem {
				return nil
			}
			return s.applyHost(ctx, c, previous, input.Active, false)
		},
		Confirm: func(ctx context.Context) error { return s.Store.Applied(ctx, state.Revision) },
		Recover: func(ctx context.Context) error {
			old, err := s.build(ctx, previous)
			if err != nil {
				return err
			}
			if err = old.json("runtime-revision.json", state.AppliedRevision); err != nil {
				return err
			}
			if err = withStartupLock(ctx, s.Root, func() error { return publish(s.Root, old) }); err != nil {
				return err
			}
			if s.NoSystem {
				return nil
			}
			return errors.Join(s.applyHost(ctx, previous, c, input.Active, true), s.restoreEnabled(ctx, input.Enabled))
		},
	})
}

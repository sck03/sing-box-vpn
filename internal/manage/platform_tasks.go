package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/sck03/sing-box-vpn/internal/application"
)

// taskInput is persisted internally. Public commands cannot set scheduling policy.
type taskInput struct {
	application.TaskCommand
	Automatic bool `json:"automatic,omitempty"`
}

func (s *PlatformService) Queue(ctx context.Context, a application.TaskCommand, actor string) (int64, error) {
	if err := a.Validate(); err != nil {
		return 0, err
	}
	if a.Operation == "gateway" {
		operation, err := a.GatewayOperation()
		if err != nil {
			return 0, err
		}
		if operation == "start" || operation == "stop" {
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
			c.Gateway["enabled"] = operation == "start"
			return s.QueueApply(ctx, c, state.Revision, actor)
		}
	}
	return s.queueTask(ctx, taskInput{TaskCommand: a}, actor)
}

func (s *PlatformService) queueAutomaticSubscription(ctx context.Context, operation string) (int64, error) {
	if operation != "refresh" && operation != "deliver" {
		return 0, errors.New("unsupported scheduled subscription task")
	}
	return s.queueTask(ctx, taskInput{TaskCommand: application.TaskCommand{Operation: "subscription", Subscription: application.SubscriptionCommand{Operation: operation}}, Automatic: true}, "scheduler")
}

func (s *PlatformService) queueTask(ctx context.Context, a taskInput, actor string) (int64, error) {
	data, err := json.Marshal(a)
	if err != nil {
		return 0, err
	}
	var id int64
	if a.Background() {
		id, err = s.Store.QueueBackground(ctx, a.Operation, actor, data)
	} else {
		id, err = s.Store.Queue(ctx, a.Operation, actor, data, 0, nil)
	}
	if err == nil {
		s.signal()
	}
	return id, err
}

func (s *PlatformService) execute(ctx context.Context, id int64, a taskInput) error {
	if err := a.Validate(); err != nil {
		return err
	}
	if a.Automatic && (a.Operation != "subscription" || (a.Subscription.Operation != "refresh" && a.Subscription.Operation != "deliver")) {
		return errors.New("invalid scheduled task")
	}
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

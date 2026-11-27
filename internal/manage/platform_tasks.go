package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/domain"
)

// taskInput is persisted internally. Public commands cannot set scheduling policy.
type taskInput struct {
	application.TaskCommand
	Automatic bool `json:"automatic,omitempty"`
}

func (a taskInput) Validate() error {
	if !a.Automatic {
		return a.TaskCommand.Validate()
	}
	return a.TaskCommand.ValidateScheduledSubscription()
}

func (s *PlatformService) Queue(ctx context.Context, a application.TaskCommand, submission domain.TaskSubmission) (int64, error) {
	if id, err := s.Store.SubmittedTask(ctx, submission); err != nil || id != 0 {
		return id, err
	}
	if err := a.Validate(); err != nil {
		return 0, err
	}
	if a.Operation == "certificate-renew" {
		if _, _, err := s.appliedCertificate(ctx, a.ID); err != nil {
			return 0, err
		}
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
				if id, err := s.Store.SubmittedTask(ctx, submission); err != nil || id != 0 {
					return id, err
				}
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
			return s.QueueApply(ctx, c, state.Revision, submission)
		}
	}
	return s.queueTask(ctx, taskInput{TaskCommand: a}, submission)
}

func (s *PlatformService) queueAutomaticSubscription(ctx context.Context, operation string) (int64, error) {
	return s.queueTask(ctx, taskInput{TaskCommand: application.TaskCommand{Operation: "subscription", Subscription: application.SubscriptionCommand{Operation: operation}}, Automatic: true}, domain.TaskSubmission{Actor: "scheduler", Source: domain.TaskScheduler})
}

func (s *PlatformService) queueTask(ctx context.Context, a taskInput, submission domain.TaskSubmission) (int64, error) {
	if err := a.Validate(); err != nil {
		return 0, err
	}
	data, err := json.Marshal(a)
	if err != nil {
		return 0, err
	}
	id, err := s.Store.QueueTask(ctx, a.Lane(), a.Operation, submission, data)
	if err == nil {
		s.signal(a.Lane())
	}
	return id, err
}

func (s *PlatformService) execute(ctx context.Context, id int64, a taskInput) error {
	if err := a.Validate(); err != nil {
		return err
	}
	if err := s.Store.Phase(ctx, id, "executing"); err != nil {
		return err
	}
	switch a.Operation {
	case "subscription":
		var out bytes.Buffer
		operationErr := application.ExecuteSubscription(ctx, subscriptionOperations{AccountService{Root: s.Root, Core: s.Core, Store: s.Accounts, Control: s.Store}}, a.Subscription, a.Automatic, &out)
		return s.storeTaskOutput(ctx, id, out.Bytes(), operationErr)
	case "service":
		if s.NoSystem {
			return errors.New("system operations are disabled")
		}
		return s.service(ctx, a.Service, a.Value)
	case "gateway":
		data, err := s.gateway(ctx, "actions", a.Config)
		return s.storeTaskOutput(ctx, id, data, err)
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

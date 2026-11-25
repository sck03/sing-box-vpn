// Package application defines transport-independent commands shared by HTTP and CLI.
package application

import (
	"context"
	"errors"
	"io"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

type SubscriptionCommand struct {
	Operation string `json:"operation"`
	ID        string `json:"id,omitempty"`
	ProbeURL  string `json:"probe_url,omitempty"`
}

// Validate rejects fields that would otherwise silently change the requested scope.
func (c SubscriptionCommand) Validate() error {
	switch c.Operation {
	case "refresh":
		if c.ID != "" || c.ProbeURL != "" {
			return errors.New("subscription refresh is global and cannot select an id or probe_url")
		}
	case "check":
		if !sub.ValidID(c.ID) {
			return errors.New("subscription check requires an output id")
		}
		if err := sub.HTTPURL(c.ProbeURL); err != nil {
			return err
		}
	case "deliver":
		if !sub.ValidID(c.ID) {
			return errors.New("subscription delivery requires a destination id")
		}
		if c.ProbeURL != "" {
			return errors.New("probe_url is only valid for subscription checks")
		}
	default:
		return errors.New("unsupported subscription operation")
	}
	return nil
}

// SubscriptionOperations is the infrastructure boundary shared by manual and
// scheduled work. Automatic scheduling is internal and is never a command field.
type SubscriptionOperations interface {
	Refresh(context.Context, bool, io.Writer) error
	Check(context.Context, string, string, io.Writer) error
	Deliver(context.Context, string, bool, io.Writer) error
}

// ValidateScheduled deliberately excludes manual target and probe parameters.
func (c SubscriptionCommand) ValidateScheduled() error {
	if (c.Operation != "refresh" && c.Operation != "deliver") || c.ID != "" || c.ProbeURL != "" {
		return errors.New("scheduled subscriptions must refresh or deliver globally without probe parameters")
	}
	return nil
}

func ExecuteSubscription(ctx context.Context, operations SubscriptionOperations, command SubscriptionCommand, automatic bool, output io.Writer) error {
	validate := command.Validate
	if automatic {
		validate = command.ValidateScheduled
	}
	if err := validate(); err != nil {
		return err
	}
	switch command.Operation {
	case "refresh":
		return operations.Refresh(ctx, !automatic, output)
	case "check":
		return operations.Check(ctx, command.ID, command.ProbeURL, output)
	case "deliver":
		return operations.Deliver(ctx, command.ID, automatic, output)
	}
	return errors.New("unsupported subscription operation")
}

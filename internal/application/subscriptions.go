// Package application defines transport-independent commands shared by HTTP and CLI.
package application

import (
	"context"
	"errors"
	"io"
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
		if c.ID == "" {
			return errors.New("subscription check requires an output id")
		}
	case "deliver":
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

func ExecuteSubscription(ctx context.Context, operations SubscriptionOperations, command SubscriptionCommand, automatic bool, output io.Writer) error {
	if err := command.Validate(); err != nil {
		return err
	}
	if automatic && (command.Operation == "check" || command.ID != "") {
		return errors.New("scheduled subscriptions must refresh or deliver globally")
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

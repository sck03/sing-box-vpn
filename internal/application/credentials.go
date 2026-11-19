package application

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type CredentialRepository interface {
	Rotate(context.Context, string, string, string, int64) error
	Unbind(context.Context, string, int64) error
}

func NormalizeCredentialInput(c AccountCommand) (AccountCommand, error) {
	if c.UUID != "" {
		id, err := uuid.Parse(c.UUID)
		if err != nil || id == uuid.Nil {
			return c, errors.New("invalid custom UUID")
		}
		c.UUID = id.String()
	}
	if c.Password != "" && (len(c.Password) < 16 || len(c.Password) > 128 || strings.ContainsAny(c.Password, "\r\n\x00")) {
		return c, errors.New("custom password must contain 16–128 bytes without control characters")
	}
	return c, nil
}

// MutateCredential contains identity policy only; the platform adapter owns
// task persistence, publication and restarting the affected runtime.
func MutateCredential(ctx context.Context, repository CredentialRepository, c AccountCommand) error {
	var err error
	c, err = NormalizeCredentialInput(c)
	if err != nil {
		return err
	}
	switch c.Operation {
	case "rotate":
		if c.UUID == "" {
			c.UUID = uuid.NewString()
		}
		if c.Password == "" {
			c.Password = rand.Text()
		}
		return repository.Rotate(ctx, c.ID, c.UUID, c.Password, c.Generation)
	case "unbind":
		return repository.Unbind(ctx, c.ID, c.Generation)
	default:
		return fmt.Errorf("unknown credential command %q", c.Operation)
	}
}

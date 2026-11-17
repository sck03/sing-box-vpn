package manage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

type SubscriptionService struct{ Root, Core string }

func subscriptionPreviewSummary(snapshot sub.Snapshot, previews map[string]sub.Preview) Object {
	collections, outputs := Object{}, Object{}
	for id, preview := range previews {
		collections[id] = Object{"input": preview.Input, "output": preview.Output, "diagnostics": preview.Diagnostics}
	}
	for id, artifact := range snapshot.Artifacts {
		outputs[id] = Object{"name": artifact.Name, "mime": artifact.MIME, "bytes": len(artifact.Data), "etag": artifact.ETag}
	}
	return Object{"collections": collections, "outputs": outputs, "diagnostics": snapshot.Diagnostics}
}

func (s SubscriptionService) Execute(ctx context.Context, command application.SubscriptionCommand, output io.Writer) error {
	root, core := s.Root, s.Core
	id, operation := command.ID, command.Operation
	switch operation {
	case "check":
		return checkSubscriptionOutput(ctx, root, core, id, command.ProbeURL, output)
	case "deliver":
		return deliverSubscriptions(ctx, root, id, false, output)
	case "refresh":
		var result application.RefreshResult
		if err := refreshSubscriptionsResult(ctx, root, core, true, &result); err != nil {
			return err
		}
		return jsonEncoder(output).Encode(result)
	case "preview", "apply":
		return s.Plan(ctx, command, output)
	}
	return withLock(root, func() error {
		c, r, local, err := loadSubscriptions(root)
		if err != nil {
			return err
		}
		write := func(value any) error { return jsonEncoder(output).Encode(value) }
		switch operation {
		case "show":
			return write(c)
		case "clients":
			return write(sub.Capabilities())
		case "logs", "grant", "revoke":
			store, err := accounting.Open(filepath.Join(root, "control.db"))
			if err != nil {
				return err
			}
			defer store.Close()
			if operation == "revoke" {
				return store.RevokeGrant(ctx, id)
			}
			if operation == "logs" {
				grants, err := store.Grants(ctx)
				if err != nil {
					return err
				}
				return write(Object{"runtime": r, "downloads": grants})
			}
			for _, id := range command.Outputs {
				if !slices.ContainsFunc(c.Outputs, func(o sub.Output) bool { return o.ID == id }) {
					return errors.New("unknown output")
				}
			}
			if command.Expires < 0 || command.Expires > 3650*24*time.Hour {
				return errors.New("invalid grant lifetime")
			}
			g := accounting.Grant{ID: id, Outputs: command.Outputs, MaxDownloads: command.MaxDownloads}
			if command.Expires > 0 {
				expiry := time.Now().Add(command.Expires)
				g.ExpiresAt = &expiry
			}
			var listener subscriptionSettings
			if _, err = ReadJSON(filepath.Join(root, "subscription.json"), &listener); err != nil {
				return err
			}
			if !listener.Enabled || listener.Port < 1 {
				return errors.New("configure and enable the subscription listener first")
			}
			var app App
			if _, err = ReadJSON(filepath.Join(root, "app.json"), &app); err != nil {
				return err
			}
			token, err := store.CreateGrant(ctx, g)
			if err != nil {
				return err
			}
			for _, id := range g.Outputs {
				if _, err = fmt.Fprintf(output, "%s/s/%s/%s\n", subscriptionBase(listener, app.Address), token, id); err != nil {
					return err
				}
			}
			return nil
		case "export":
			var snapshot sub.Snapshot
			if _, err := ReadJSON(filepath.Join(root, subscriptionsSnapshot), &snapshot); err != nil {
				return err
			}
			artifact, exists := snapshot.Artifacts[id]
			if !exists {
				return errors.New("published output not found")
			}
			if err := checkOutputAccounts(ctx, root, c, id); err != nil {
				return err
			}
			_, err = output.Write(artifact.Data)
			return err
		case "archive", "restore", "trash":
			return subscriptionArchive(root, core, operation, command.Kind, id, c, r, local, ctx, output)
		default:
			return errors.New("unknown subscriptions operation")
		}
	})
}

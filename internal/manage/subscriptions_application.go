package manage

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"github.com/sck03/sing-box-vpn/internal/application"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"time"
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
	id, kind, expires, maxDownloads := command.ID, command.Kind, command.Expires, command.MaxDownloads
	selected := strings.Join(command.Outputs, ",")
	outputs, probeURL := selected, command.ProbeURL
	operation := command.Operation
	if operation == "check" {
		return checkSubscriptionOutput(ctx, root, core, id, probeURL, output)
	}
	if operation == "deliver" {
		return deliverSubscriptions(ctx, root, id, false, output)
	}
	if operation == "refresh" {
		return refreshSubscriptions(ctx, root, core, true)
	}
	if operation == "preview" || operation == "apply" {
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
			return write(sub.Clients)
		case "logs":
			counts := map[string]uint64{}
			for _, grant := range c.Grants {
				var count uint64
				if _, err := ReadJSON(filepath.Join(root, "subscriptions/usage", grant.TokenHash+".json"), &count); err != nil {
					return err
				}
				if grant.MaxDownloads > 0 {
					counts[grant.ID] = count
				}
			}
			return write(Object{"runtime": r, "downloads": counts})
		case "grant":
			var listener subscriptionSettings
			configured, err := ReadJSON(filepath.Join(root, "subscription.json"), &listener)
			if err != nil {
				return err
			}
			if !configured || listener.Port < 1 || listener.Port > 65535 {
				return errors.New("请先配置订阅监听地址和端口，再创建分享")
			}
			var app App
			if _, err := ReadJSON(filepath.Join(root, "app.json"), &app); err != nil {
				return err
			}
			if listener.PublicBaseURL == "" && app.Address == "" {
				return errors.New("请先配置节点地址或独立公网订阅地址")
			}
			c.Grants = slices.DeleteFunc(c.Grants, func(g sub.Grant) bool { return g.Revoked && g.TokenHash != sub.Digest([]byte(listener.Token)) })
			if id == "" || !sub.ValidID(id) {
				return errors.New("grant requires a valid --id")
			}
			if expires < 0 || expires > 3650*24*time.Hour {
				return errors.New("invalid grant lifetime")
			}
			if slices.ContainsFunc(c.Grants, func(g sub.Grant) bool { return g.ID == id }) {
				return errors.New("grant ID already exists; revoke it and use a new ID")
			}
			token := randomSecret()
			g := sub.Grant{ID: id, Name: id, TokenHash: sub.Digest([]byte(token)), Outputs: strings.Split(outputs, ","), MaxDownloads: maxDownloads}
			if expires > 0 {
				g.ExpiresAt = time.Now().Add(expires)
			}
			c.Grants = append(c.Grants, g)
			c.Revision++
			if err := c.Validate(); err != nil {
				return err
			}
			if err := AtomicJSON(filepath.Join(root, subscriptionsConfig), c); err != nil {
				return err
			}
			base := subscriptionBase(listener, app.Address)
			for _, id := range g.Outputs {
				if _, err := fmt.Fprintf(output, "%s/s/%s/%s\n", base, token, id); err != nil {
					return err
				}
			}
			return nil
		case "revoke":
			found := false
			files := fileBundle{}
			for i := range c.Grants {
				if c.Grants[i].ID == id {
					c.Grants[i].Revoked = true
					files[filepath.ToSlash(filepath.Join("subscriptions/usage", c.Grants[i].TokenHash+".json"))] = nil
					found = true
				}
			}
			if !found {
				return errors.New("grant not found")
			}
			c.Revision++
			r.Record("revoke", id, "share revoked", time.Now())
			if err := files.json(subscriptionsConfig, c); err != nil {
				return err
			}
			if err := files.json(subscriptionsRuntime, r); err != nil {
				return err
			}
			return publish(root, files)
		case "export":
			var snapshot sub.Snapshot
			if _, err := ReadJSON(filepath.Join(root, subscriptionsSnapshot), &snapshot); err != nil {
				return err
			}
			a, exists := snapshot.Artifacts[id]
			if !exists {
				return errors.New("published output not found")
			}
			_, err = output.Write(a.Data)
			return err
		case "archive", "restore", "trash":
			return subscriptionArchive(root, core, operation, kind, id, c, r, local, ctx, output)
		default:
			return errors.New("unknown subscriptions operation")
		}
	})
}

func findGrant(c sub.Config, token, outputID string, now time.Time) (sub.Grant, bool) {
	hash := sub.Digest([]byte(token))
	for _, g := range c.Grants {
		if subtle.ConstantTimeCompare([]byte(hash), []byte(g.TokenHash)) == 1 && !g.Revoked && (g.ExpiresAt.IsZero() || now.Before(g.ExpiresAt)) && slices.Contains(g.Outputs, outputID) {
			return g, true
		}
	}
	return sub.Grant{}, false
}

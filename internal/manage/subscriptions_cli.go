package manage

import (
	"context"
	"crypto/subtle"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func RunSubscriptions(ctx context.Context, args []string, input io.Reader, output io.Writer) error {
	f := flag.NewFlagSet("subscriptions", flag.ContinueOnError)
	f.SetOutput(output)
	root := f.String("root", "/etc/s-box", "configuration directory")
	core := f.String("core", "", "sing-box binary")
	if err := f.Parse(args); err != nil {
		return err
	}
	args = f.Args()
	if len(args) == 0 {
		return errors.New("subscriptions: show, clients, preview, apply, refresh, grant, revoke, export, logs, archive, restore, trash, backup, restore-backup, serve, check, deliver")
	}
	if *core == "" {
		*core = filepath.Join(*root, "sing-box")
	}
	opts := flag.NewFlagSet(args[0], flag.ContinueOnError)
	opts.SetOutput(output)
	file := opts.String("file", "", "input or output file")
	id := opts.String("id", "", "object or output ID")
	kind := opts.String("kind", "", "source, collection, template, output or rules")
	outputs := opts.String("outputs", "", "comma-separated output IDs")
	expires := opts.Duration("expires", 0, "grant lifetime; zero is unlimited")
	maxDownloads := opts.Uint64("max-downloads", 0, "zero is unlimited")
	apply := opts.Bool("apply", false, "apply a validated backup")
	listen := opts.String("listen", "127.0.0.1:9099", "local administration listener")
	probeURL := opts.String("probe-url", "https://www.gstatic.com/generate_204", "HTTP(S) target for on-demand protocol checks")
	if err := parseOptions(opts, args[1:]); err != nil {
		return err
	}
	operation := args[0]
	if operation == "serve" {
		return serveSubscriptionAdmin(ctx, *root, *core, *listen, output)
	}
	if operation == "check" {
		return checkSubscriptionOutput(ctx, *root, *core, *id, *probeURL, output)
	}
	if operation == "deliver" {
		return deliverSubscriptions(ctx, *root, *id, false, output)
	}
	if operation == "refresh" {
		return refreshSubscriptions(ctx, *root, *core, true)
	}
	if operation == "backup" || operation == "restore-backup" {
		return subscriptionBackupCommand(ctx, *root, *core, operation, *file, *apply, input, output)
	}
	var candidate *sub.Config
	var prepared map[string]sub.SourceCache
	if *file != "" && (operation == "preview" || operation == "apply") {
		data, err := readSubscriptionFile(*file)
		if err != nil {
			return err
		}
		candidate = new(sub.Config)
		if err := strictSubscriptionJSON(data, candidate); err != nil {
			return err
		}
		prepared, err = prepareSubscriptionSources(ctx, *root, *candidate)
		if err != nil {
			return err
		}
	}
	return withLock(*root, func() error {
		c, r, local, err := loadSubscriptions(*root)
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
				if _, err := ReadJSON(filepath.Join(*root, "subscriptions/usage", grant.TokenHash+".json"), &count); err != nil {
					return err
				}
				if grant.MaxDownloads > 0 {
					counts[grant.ID] = count
				}
			}
			return write(Object{"runtime": r, "downloads": counts})
		case "preview", "apply":
			if r.Sources == nil {
				r.Sources = map[string]sub.SourceCache{}
			}
			for id, cache := range prepared {
				r.Sources[id] = cache
			}
			if candidate != nil {
				if err := validateSubscriptionsRevision(c, *candidate); err != nil {
					return err
				}
				// Authorization can only be changed through explicit grant/revoke.
				candidate.Grants = c.Grants
				c = *candidate
			} else if operation == "apply" {
				return errors.New("apply requires --file")
			}
			if operation == "apply" {
				c.Revision++
			}
			files, snapshot, previews, err := buildSubscriptions(ctx, *root, *core, c, r, local)
			if err != nil {
				return err
			}
			if operation == "preview" {
				return write(Object{"collections": previews, "diagnostics": snapshot.Diagnostics, "outputs": snapshot.Artifacts})
			}
			// Remove cached nodes of deleted sources; stale data never reintroduces them.
			for key := range r.Sources {
				if !slices.ContainsFunc(c.Sources, func(s sub.Source) bool { return s.ID == key }) {
					delete(r.Sources, key)
				}
			}
			for key := range r.Resources {
				found := slices.ContainsFunc(c.Templates, func(t sub.Template) bool { return key == "template:"+t.ID }) || slices.ContainsFunc(c.RuleSets, func(s sub.RuleSet) bool { return key == "rules:"+s.ID })
				if !found {
					delete(r.Resources, key)
				}
			}
			for key := range r.Deliveries {
				if !slices.ContainsFunc(c.Deliveries, func(d sub.Delivery) bool { return key == d.ID }) {
					delete(r.Deliveries, key)
				}
			}
			r.Record("apply", "", "configuration and subscription snapshot published", time.Now())
			if err := files.json(subscriptionsRuntime, r); err != nil {
				return err
			}
			return publish(*root, files)
		case "grant":
			var listener subscriptionSettings
			configured, err := ReadJSON(filepath.Join(*root, "subscription.json"), &listener)
			if err != nil {
				return err
			}
			if !configured || listener.Port < 1 || listener.Port > 65535 {
				return errors.New("请先配置订阅监听地址和端口，再创建分享")
			}
			var app App
			if _, err := ReadJSON(filepath.Join(*root, "app.json"), &app); err != nil {
				return err
			}
			if listener.PublicBaseURL == "" && app.Address == "" {
				return errors.New("请先配置节点地址或独立公网订阅地址")
			}
			c.Grants = slices.DeleteFunc(c.Grants, func(g sub.Grant) bool { return g.Revoked && g.TokenHash != sub.Digest([]byte(listener.Token)) })
			if *id == "" || !sub.ValidID(*id) {
				return errors.New("grant requires a valid --id")
			}
			if *expires < 0 || *expires > 3650*24*time.Hour {
				return errors.New("invalid grant lifetime")
			}
			if slices.ContainsFunc(c.Grants, func(g sub.Grant) bool { return g.ID == *id }) {
				return errors.New("grant ID already exists; revoke it and use a new ID")
			}
			token := randomSecret()
			g := sub.Grant{ID: *id, Name: *id, TokenHash: sub.Digest([]byte(token)), Outputs: strings.Split(*outputs, ","), MaxDownloads: *maxDownloads}
			if *expires > 0 {
				g.ExpiresAt = time.Now().Add(*expires)
			}
			c.Grants = append(c.Grants, g)
			c.Revision++
			if err := c.Validate(); err != nil {
				return err
			}
			if err := AtomicJSON(filepath.Join(*root, subscriptionsConfig), c); err != nil {
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
				if c.Grants[i].ID == *id {
					c.Grants[i].Revoked = true
					files[filepath.ToSlash(filepath.Join("subscriptions/usage", c.Grants[i].TokenHash+".json"))] = nil
					found = true
				}
			}
			if !found {
				return errors.New("grant not found")
			}
			c.Revision++
			r.Record("revoke", *id, "share revoked", time.Now())
			if err := files.json(subscriptionsConfig, c); err != nil {
				return err
			}
			if err := files.json(subscriptionsRuntime, r); err != nil {
				return err
			}
			return publish(*root, files)
		case "export":
			var snapshot sub.Snapshot
			if _, err := ReadJSON(filepath.Join(*root, subscriptionsSnapshot), &snapshot); err != nil {
				return err
			}
			a, exists := snapshot.Artifacts[*id]
			if !exists {
				return errors.New("published output not found")
			}
			if *file == "" {
				_, err := output.Write(a.Data)
				return err
			}
			return AtomicBytes(*file, a.Data, 0600)
		case "archive", "restore", "trash":
			return subscriptionArchive(*root, *core, operation, *kind, *id, c, r, local, ctx, output)
		default:
			return errors.New("unknown subscriptions operation")
		}
	})
}

func readSubscriptionFile(path string) ([]byte, error) {
	return readLimitedRegularFile(path, sub.MaxBody)
}

func readLimitedRegularFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("input must be a regular file within the size limit")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("input file too large")
	}
	return data, nil
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

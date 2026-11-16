package manage

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sck03/sing-box-vpn/internal/application"
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
	if operation == "backup" || operation == "restore-backup" {
		return subscriptionBackupCommand(ctx, *root, *core, operation, *file, *apply, input, output)
	}
	command := application.SubscriptionCommand{Operation: operation, ID: *id, Kind: *kind, Outputs: strings.Split(*outputs, ","), Expires: *expires, MaxDownloads: *maxDownloads, Apply: *apply, ProbeURL: *probeURL}
	if *file != "" && (operation == "preview" || operation == "apply") {
		data, err := readSubscriptionFile(*file)
		if err != nil {
			return err
		}
		command.Config = data
	}
	if operation == "export" && *file != "" {
		var result bytes.Buffer
		if err := (SubscriptionService{Root: *root, Core: *core}).Execute(ctx, command, &result); err != nil {
			return err
		}
		return AtomicBytes(*file, result.Bytes(), 0600)
	}
	return (SubscriptionService{Root: *root, Core: *core}).Execute(ctx, command, output)
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

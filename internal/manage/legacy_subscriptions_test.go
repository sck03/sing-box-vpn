package manage

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sck03/sing-box-vpn/internal/application"
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
		return errors.New("subscriptions: show, clients, preview, apply, refresh, grant, revoke, export, logs, archive, restore, trash, serve, check, deliver")
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
	maxDownloads := opts.Int64("max-downloads", -1, "omit for unlimited; zero disables downloads")
	apply := opts.Bool("apply", false, "apply a validated backup")
	listen := opts.String("listen", "127.0.0.1:9099", "local administration listener")
	probeURL := opts.String("probe-url", "https://www.gstatic.com/generate_204", "HTTP(S) target for on-demand protocol checks")
	if err := parseOptions(opts, args[1:]); err != nil {
		return err
	}
	operation := args[0]
	if operation == "serve" {
		return RunPanel(ctx, []string{"--root", *root, "--core", *core, "--listen", *listen, "serve"}, input, output)
	}
	if platformOwned(*root) && !slices.Contains([]string{"show", "clients", "preview", "export", "logs", "check", "refresh", "deliver"}, operation) {
		return errors.New("subscription configuration is owned by control.db; use the unified panel")
	}
	command := application.SubscriptionCommand{Operation: operation, ID: *id, Kind: *kind, Outputs: strings.Split(*outputs, ","), Expires: *expires, Apply: *apply, ProbeURL: *probeURL}
	opts.Visit(func(f *flag.Flag) {
		if f.Name == "max-downloads" {
			command.MaxDownloads = maxDownloads
		}
	})
	if *file != "" && (operation == "preview" || operation == "apply") {
		data, err := readSubscriptionFile(*file)
		if err != nil {
			return err
		}
		command.Config = data
	}
	if operation == "export" && *file != "" {
		var result bytes.Buffer
		if err := (SubscriptionService{Root: *root, Core: *core}).legacyExecute(ctx, command, &result); err != nil {
			return err
		}
		return AtomicBytes(*file, result.Bytes(), 0600)
	}
	return (SubscriptionService{Root: *root, Core: *core}).legacyExecute(ctx, command, output)
}

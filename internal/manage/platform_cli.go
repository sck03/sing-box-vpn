package manage

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"

	strict "github.com/sck03/sing-box-vpn/internal/encoding"
)

func RunPanel(ctx context.Context, args []string, input io.Reader, output io.Writer) error {
	f := flag.NewFlagSet("panel", flag.ContinueOnError)
	f.SetOutput(output)
	root := f.String("root", "/etc/s-box", "managed state directory")
	core := f.String("core", "", "official validation core")
	runtime := f.String("runtime", "", "managed runtime binary")
	address := f.String("address", "", "published server IP or domain")
	name := f.String("name", "VPS", "server name")
	listen := f.String("listen", "", "panel listening address")
	public := f.String("public-url", "", "external origin")
	cert := f.String("tls-cert", "", "panel TLS certificate")
	key := f.String("tls-key", "", "panel TLS key")
	proxy := f.String("trusted-proxy", "", "comma-separated trusted TLS proxy CIDRs")
	noSystem := f.Bool("no-system", false, "local development without systemd operations")
	if err := f.Parse(args); err != nil {
		return err
	}
	if len(f.Args()) != 1 {
		return errors.New("panel requires init, serve, reset-admin, status, command, apply, recover or restore")
	}
	if *core == "" {
		*core = filepath.Join(*root, "sing-box")
	}
	if *runtime == "" {
		*runtime = filepath.Join(*root, "managed-core")
	}
	op := f.Args()[0]
	decode := func(v any) error {
		limit := maxJSONFile
		data, err := io.ReadAll(io.LimitReader(input, int64(limit)+1))
		if err != nil {
			return err
		}
		return strict.JSON(data, v, limit)
	}
	if op == "restore" {
		var v struct {
			BackupPath  string `json:"backup_path"`
			Password    string `json:"password"`
			Username    string `json:"username"`
			NewPassword string `json:"new_password"`
		}
		if err := decode(&v); err != nil {
			return err
		}
		file, err := os.Open(v.BackupPath)
		if err != nil {
			return err
		}
		defer file.Close()
		backup, err := io.ReadAll(io.LimitReader(file, maxPlatformBackup+1))
		if err != nil {
			return err
		}
		if len(backup) > maxPlatformBackup {
			return errors.New("backup exceeds 96 MiB")
		}
		return RestoreDisaster(ctx, *root, backup, []byte(v.Password), v.Username, v.NewPassword)
	}
	s, err := OpenPlatform(*root, *core, *runtime, *noSystem)
	if err != nil {
		return err
	}
	defer s.Close()
	switch op {
	case "init":
		var v struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err = decode(&v); err != nil {
			return err
		}
		if len(v.Password) < 12 || len(v.Password) > 72 {
			return errors.New("administrator password must contain 12–72 bytes")
		}
		if v.Username == "" {
			v.Username = "admin"
		}
		if err = s.Initialize(ctx, *address, *name, v.Username, v.Password); err != nil {
			return err
		}
		options := PanelOptions{Listen: "127.0.0.1:2096"}
		if *listen != "" {
			options.Listen = *listen
		}
		options.PublicURL, options.Certificate, options.Key = *public, *cert, *key
		if *proxy != "" {
			options.TrustedProxy = strings.Split(*proxy, ",")
		}
		return AtomicJSON(filepath.Join(*root, "panel.json"), options)
	case "reset-admin":
		var v struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err = decode(&v); err != nil {
			return err
		}
		return s.Auth.Bootstrap(ctx, v.Username, v.Password, true)
	case "serve":
		options := PanelOptions{Listen: "127.0.0.1:2096"}
		if _, err = ReadJSON(filepath.Join(*root, "panel.json"), &options); err != nil {
			return err
		}
		if *listen != "" {
			options.Listen = *listen
		}
		if *public != "" {
			options.PublicURL = *public
		}
		if *cert != "" {
			options.Certificate = *cert
			options.Key = *key
		}
		if *proxy != "" {
			options.TrustedProxy = strings.Split(*proxy, ",")
		}
		return s.Serve(ctx, options, output)
	case "status":
		state, e := s.State(ctx, false)
		if e != nil {
			return e
		}
		return json.NewEncoder(output).Encode(state)
	case "command":
		var v PlatformAction
		if err = decode(&v); err != nil {
			return err
		}
		id, e := s.Queue(ctx, v, "local-cli")
		if e != nil {
			return e
		}
		return json.NewEncoder(output).Encode(Object{"task_id": id})
	case "apply":
		var v struct {
			Revision int64          `json:"revision"`
			Config   PlatformConfig `json:"config"`
		}
		if err = decode(&v); err != nil {
			return err
		}
		id, e := s.QueueApply(ctx, v.Config, v.Revision, "local-cli")
		if e != nil {
			return e
		}
		return json.NewEncoder(output).Encode(Object{"task_id": id})
	case "recover":
		state, e := s.Store.State(ctx)
		if e != nil {
			return e
		}
		c, e := decodePlatform(state.Applied)
		if e != nil {
			return e
		}
		files, e := s.build(ctx, c)
		if e != nil {
			return e
		}
		if e = files.json("runtime-revision.json", state.AppliedRevision); e != nil {
			return e
		}
		return withStartupLock(ctx, s.Root, func() error { return publish(s.Root, files) })
	case "install-services":
		if *noSystem {
			return errors.New("system operations are disabled")
		}
		if err = s.WriteUnits(ctx); err != nil {
			return err
		}
		state, err := s.Store.State(ctx)
		if err != nil {
			return err
		}
		c, err := decodePlatform(state.Applied)
		if err != nil {
			return err
		}
		return s.writeTunnel(ctx, c)
	case "validate":
		state, err := s.Store.State(ctx)
		if err != nil {
			return err
		}
		c, err := decodePlatform(state.Applied)
		if err != nil {
			return err
		}
		files, err := s.build(ctx, c)
		if err != nil {
			return err
		}
		return s.checkRuntime(ctx, files)
	default:
		return errors.New("unknown panel command")
	}
}

func platformOwned(root string) bool {
	_, err := os.Stat(filepath.Join(root, ".platform-v4"))
	return err == nil
}

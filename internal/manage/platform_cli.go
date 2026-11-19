package manage

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sck03/sing-box-vpn/internal/adminauth"
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
	adminStdin := f.Bool("admin-stdin", false, "read NUL-delimited username and password from stdin (init/reset-admin)")
	if err := f.Parse(args); err != nil {
		return err
	}
	if len(f.Args()) != 1 && !(len(f.Args()) == 2 && f.Arg(0) == "restart-settings") {
		return errors.New("panel requires init, serve, configure, reset-admin, status, command, apply, recover or restore")
	}
	if *core == "" {
		*core = filepath.Join(*root, "sing-box")
	}
	if *runtime == "" {
		*runtime = filepath.Join(*root, "managed-core")
	}
	op := f.Args()[0]
	if *adminStdin && op != "init" && op != "reset-admin" {
		return errors.New("--admin-stdin only supports init and reset-admin")
	}
	if op == "recover-services" {
		if *noSystem {
			return errors.New("system operations are disabled")
		}
		return recoverServices(ctx, *root)
	}
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
		return restoreDisasterFrom(ctx, *root, file, []byte(v.Password), v.Username, v.NewPassword)
	}
	s, err := OpenPlatform(*root, *core, *runtime, *noSystem)
	if err != nil {
		return err
	}
	defer s.Close()
	applyFlags := func(options PanelOptions) PanelOptions {
		f.Visit(func(flag *flag.Flag) {
			switch flag.Name {
			case "listen":
				options.Listen = *listen
			case "public-url":
				options.PublicURL = *public
			case "tls-cert":
				options.Certificate = *cert
			case "tls-key":
				options.Key = *key
			case "trusted-proxy":
				options.TrustedProxy = nil
				if *proxy != "" {
					options.TrustedProxy = strings.Split(*proxy, ",")
				}
			}
		})
		return options
	}
	switch op {
	case "restart-settings":
		id, err := strconv.ParseInt(f.Arg(1), 10, 64)
		if err != nil {
			return err
		}
		return s.restartPanelSettings(ctx, id)
	case "complete-upgrade":
		return s.reconcileProgramUpgrade(ctx)
	case "wait-panel":
		return s.waitPanel(ctx)
	case "init":
		v, err := readAdministrator(input, *adminStdin)
		if err != nil {
			return err
		}
		if len(v.Password) < adminauth.MinPasswordBytes || len(v.Password) > adminauth.MaxPasswordBytes {
			return errors.New("administrator password must contain 8–72 bytes")
		}
		if v.Username == "" {
			v.Username = "admin"
		}
		return s.initialize(ctx, *address, *name, v.Username, v.Password, applyFlags(defaultPanelOptions()))
	case "reset-admin":
		v, err := readAdministrator(input, *adminStdin)
		if err != nil {
			return err
		}
		return s.Auth.Bootstrap(ctx, v.Username, v.Password, true)
	case "configure":
		var options PanelOptions
		if err := decode(&options); err != nil {
			return err
		}
		if err := options.Validate(); err != nil {
			return err
		}
		if err := s.validatePanelListener(ctx, options); err != nil {
			return err
		}
		return withStartupLock(ctx, s.Root, func() error {
			if pending, err := s.Store.PendingKind(ctx, "panel-restart"); err != nil {
				return err
			} else if pending {
				return errors.New("panel restart is still pending")
			}
			if err := s.validatePanelListener(ctx, options); err != nil {
				return err
			}
			return AtomicJSON(filepath.Join(s.Root, "panel.json"), options)
		})
	case "serve":
		options, _, err := s.panelSettings()
		if err != nil {
			return err
		}
		return s.Serve(ctx, applyFlags(options), output)
	case "status":
		state, e := s.State(ctx, false)
		if e != nil {
			return e
		}
		return json.NewEncoder(output).Encode(state)
	case "wait-runtime":
		var v struct {
			Instance string `json:"instance"`
			Started  int64  `json:"started"`
		}
		if err := decode(&v); err != nil {
			return err
		}
		if (v.Instance != "normal" && v.Instance != "vpngate") || v.Started <= 0 || v.Started > time.Now().UnixNano() {
			return errors.New("invalid runtime readiness request")
		}
		return s.awaitRuntime(ctx, v.Instance, v.Started)
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
		if e := withStartupLock(ctx, s.Root, func() error { return recoverSubscriptionRuntime(s.Root) }); e != nil {
			return e
		}
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

type administratorCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func readAdministrator(input io.Reader, nulDelimited bool) (administratorCredentials, error) {
	var credentials administratorCredentials
	const limit = 4096
	data, err := io.ReadAll(io.LimitReader(input, limit+1))
	if err != nil {
		return credentials, err
	}
	if len(data) > limit {
		return credentials, errors.New("administrator input exceeds size limit")
	}
	if !nulDelimited {
		err = strict.JSON(data, &credentials, limit)
		return credentials, err
	}
	fields := strings.Split(string(data), "\x00")
	if len(fields) != 3 || fields[2] != "" {
		return credentials, errors.New("administrator input requires two NUL-delimited fields")
	}
	credentials.Username, credentials.Password = fields[0], fields[1]
	return credentials, nil
}

func platformOwned(root string) bool {
	_, err := os.Stat(filepath.Join(root, ".platform-v4"))
	return err == nil
}

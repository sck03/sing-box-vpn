package manage

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"

	"github.com/sck03/sing-box-vpn/internal/adminauth"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

const initializationMarker = ".initializing-platform"

// Configuration and the first administrator commit together. Projection failure
// leaves an explicit recovery marker; retry publishes the committed configuration.
func (s *PlatformService) Initialize(ctx context.Context, address, name, admin, password string) error {
	install, err := adminauth.PrepareAdministrator(admin, password)
	if err != nil {
		return err
	}
	return withStartupLock(ctx, s.Root, func() error {
		marker := filepath.Join(s.Root, initializationMarker)
		_, pending := os.Stat(marker)
		state, err := s.Store.State(ctx)
		var config PlatformConfig
		if err == nil {
			if pending != nil {
				return errors.New("platform is already initialized")
			}
			config, err = decodePlatform(state.Applied)
			if err != nil {
				return err
			}
		} else {
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			for _, name := range []string{"app.json", "sb.json"} {
				if _, err = os.Stat(filepath.Join(s.Root, name)); err == nil {
					return errors.New("new platform requires a fresh directory; legacy configuration is not imported")
				} else if !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
			app, err := initialApp(ctx, s.Root, s.Core, address, name)
			if err != nil {
				return err
			}
			subscriptions := sub.DefaultConfig()
			config = PlatformConfig{Schema: 4, App: app, VPN: VPNSettings{ProxyPort: 7928, Profiles: map[string]Profile{}}, VPNSecrets: map[string]Secrets{}, Gateway: Object{}, Subscriptions: subscriptions, Certificates: []CertificateAsset{}, Argo: TunnelSettings{Mode: "off"}, Listener: subscriptionSettings{Listen: "127.0.0.1", Port: 2097}}
			// Validate/build before committing intent. No app.json is published yet.
			if _, err = s.build(ctx, config); err != nil {
				return err
			}
			data, err := encodePlatform(config)
			if err != nil {
				return err
			}
			if err = AtomicBytes(marker, []byte("retry panel init to finish projection\n"), 0600); err != nil {
				return err
			}
			if err = s.Store.InitializeWith(ctx, data, func(tx *sql.Tx) error { return install(ctx, tx) }); err != nil {
				return err
			}
		}
		files, err := s.build(ctx, config)
		if err != nil {
			return err
		}
		files[".platform-v4"] = []byte("control.db owns configuration; JSON files are generated\n")
		if err = files.json("runtime-revision.json", int64(1)); err != nil {
			return err
		}
		if err = publish(s.Root, files); err != nil {
			return err
		}
		return removeFile(marker)
	})
}

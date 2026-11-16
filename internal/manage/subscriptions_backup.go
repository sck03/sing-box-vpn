package manage

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

const backupMagic = "SBVPNB01"

type subscriptionBackup struct {
	Version int               `json:"version"`
	Files   map[string][]byte `json:"files"`
}

func backupCipher(password, salt []byte) (cipher.AEAD, error) {
	key, err := pbkdf2.Key(sha256.New, string(password), salt, 600000, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func encryptSubscriptionBackup(data, password []byte) ([]byte, error) {
	salt := randomBytes(16)
	aead, err := backupCipher(password, salt)
	if err != nil {
		return nil, err
	}
	header := append([]byte(backupMagic), salt...)
	nonce := randomBytes(aead.NonceSize())
	header = append(header, nonce...)
	return aead.Seal(header, nonce, data, header), nil
}

func decryptSubscriptionBackup(data, password []byte) ([]byte, error) {
	if len(data) < 52 || len(data) > maxJSONFile || string(data[:8]) != backupMagic {
		return nil, errors.New("invalid current-format encrypted backup")
	}
	aead, err := backupCipher(password, data[8:24])
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, data[24:36], data[36:], data[:36])
	if err != nil {
		return nil, errors.New("backup password incorrect or archive damaged")
	}
	return plain, nil
}

func mapAppCertificates(app *App, visit func(string) (string, error)) error {
	var err error
	for _, path := range []*string{&app.Profile.Certificate, &app.Profile.Key} {
		if *path != "" {
			*path, err = visit(*path)
			if err != nil {
				return err
			}
		}
	}
	for _, in := range app.Inbounds {
		tls := object(in["tls"])
		for _, key := range []string{"certificate_path", "key_path"} {
			if value := text(tls[key]); value != "" {
				tls[key], err = visit(value)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func collectSubscriptionBackup(root string) (subscriptionBackup, error) {
	b := subscriptionBackup{Version: sub.SchemaVersion, Files: map[string][]byte{}}
	var app App
	found, err := ReadJSON(filepath.Join(root, "app.json"), &app)
	if err != nil {
		return b, err
	}
	if !found {
		return b, errors.New("no managed application to back up")
	}
	collect := func(path string) (string, error) {
		data, err := readSubscriptionFile(path)
		if err != nil {
			return "", err
		}
		if len(data) > 256<<10 {
			return "", errors.New("certificate/key exceeds backup limit")
		}
		name := "certificates/" + sub.Digest(data) + ".pem"
		b.Files[name] = data
		return name, nil
	}
	if err := mapAppCertificates(&app, collect); err != nil {
		return b, err
	}
	b.Files["app.json"], err = JSONBytes(app)
	if err != nil {
		return b, err
	}
	settings, private, err := loadVPN(root)
	if err != nil {
		return b, err
	}
	for id, p := range settings.Profiles {
		p.Certificate, err = collect(p.Certificate)
		if err != nil {
			return b, err
		}
		p.Key, err = collect(p.Key)
		if err != nil {
			return b, err
		}
		settings.Profiles[id] = p
	}
	if len(settings.Profiles) > 0 {
		b.Files["vpngate/settings.json"], _ = JSONBytes(settings)
		b.Files["vpngate/secrets.json"], _ = JSONBytes(private)
	}
	c, runtime, _, err := loadSubscriptions(root)
	if err != nil {
		return b, err
	}
	c.Grants = []sub.Grant{}
	runtime.Runs = nil
	b.Files[subscriptionsRuntime], err = JSONBytes(runtime)
	if err != nil {
		return b, err
	}
	b.Files[subscriptionsConfig], err = JSONBytes(c)
	if err != nil {
		return b, err
	}
	for _, path := range []string{"remote.json", "vpngate/gateway.json", "subscription.json"} {
		var value Object
		exists, err := ReadJSON(filepath.Join(root, path), &value)
		if err != nil {
			return b, err
		}
		if !exists {
			continue
		}
		if path == "subscription.json" {
			delete(value, "token")
			value["enabled"] = false
		}
		b.Files[path], err = JSONBytes(value)
		if err != nil {
			return b, err
		}
	}
	return b, nil
}

func validateBackupFiles(b subscriptionBackup) error {
	if b.Version != sub.SchemaVersion || len(b.Files) == 0 || len(b.Files) > 256 {
		return errors.New("invalid backup schema or file count")
	}
	if _, err := bundleNames(b.Files); err != nil {
		return err
	}
	total := 0
	allowed := map[string]bool{"app.json": true, subscriptionsConfig: true, subscriptionsRuntime: true, "remote.json": true, "subscription.json": true, "vpngate/settings.json": true, "vpngate/secrets.json": true, "vpngate/gateway.json": true}
	for name, data := range b.Files {
		total += len(data)
		if total > sub.MaxBody {
			return errors.New("backup content exceeds 8 MiB")
		}
		if !allowed[name] && !(strings.HasPrefix(name, "certificates/") && strings.HasSuffix(name, ".pem") && len(data) <= 256<<10) {
			return errors.New("backup contains an unexpected file")
		}
	}
	return nil
}

func prepareSubscriptionRestore(ctx context.Context, root, core string, b subscriptionBackup) (fileBundle, error) {
	if err := validateBackupFiles(b); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(root, ".restore-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	for name, data := range b.Files {
		if err := AtomicBytes(filepath.Join(stage, name), data, 0600); err != nil {
			return nil, err
		}
	}
	var app App
	if err := json.Unmarshal(b.Files["app.json"], &app); err != nil {
		return nil, err
	}
	rebase := func(path string) (string, error) {
		if !strings.HasPrefix(path, "certificates/") || b.Files[path] == nil {
			return "", errors.New("backup certificate reference is invalid")
		}
		return filepath.ToSlash(filepath.Join(stage, path)), nil
	}
	if err := mapAppCertificates(&app, rebase); err != nil {
		return nil, err
	}
	settings, private, err := loadVPN(stage)
	if err != nil {
		return nil, err
	}
	for id, p := range settings.Profiles {
		p.Certificate, err = rebase(p.Certificate)
		if err != nil {
			return nil, err
		}
		p.Key, err = rebase(p.Key)
		if err != nil {
			return nil, err
		}
		settings.Profiles[id] = p
	}
	if err := AtomicJSON(filepath.Join(stage, "vpngate/settings.json"), settings); err != nil {
		return nil, err
	}
	var c sub.Config
	if err := json.Unmarshal(b.Files[subscriptionsConfig], &c); err != nil {
		return nil, err
	}
	c.Grants = []sub.Grant{}
	c.Revision++
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if err := AtomicJSON(filepath.Join(stage, subscriptionsConfig), c); err != nil {
		return nil, err
	}
	// Cached sources are restored subject to the same freshness and identity
	// checks as a regular build; expired required sources block the restore.
	files, _, err := exportBundle(ctx, stage, core, app, settings, private)
	if err != nil {
		return nil, err
	}
	server, err := ServerConfig(app.Inbounds, app.Routing, app.Warp, app.SocksPort)
	if err != nil {
		return nil, err
	}
	if err := CheckJSON(ctx, core, stage, server); err != nil {
		return nil, err
	}
	if err := files.json("sb.json", server); err != nil {
		return nil, err
	}
	if err := files.json("app.json", app); err != nil {
		return nil, err
	}
	if err := files.json("vpngate/settings.json", settings); err != nil {
		return nil, err
	}
	if settings.ActiveCountry != "" {
		server, err := vpnServer(settings, private)
		if err != nil {
			return nil, err
		}
		if err := CheckJSON(ctx, core, stage, server); err != nil {
			return nil, err
		}
		if err := files.json("vpngate/server.json", server); err != nil {
			return nil, err
		}
	}
	for name, data := range b.Files {
		if _, exists := files[name]; !exists {
			files[name] = data
		}
	}
	for _, name := range []string{"remote.json", "vpngate/secrets.json", "vpngate/gateway.json", "vpngate/server.json"} {
		if _, exists := files[name]; !exists {
			files[name] = nil
		}
	}
	// Only generated paths are rebased. Archive file names never control a path
	// outside the destination root, and no executable or system unit is restored.
	for name, data := range files {
		if strings.HasSuffix(name, ".json") {
			files[name] = bytes.ReplaceAll(data, []byte(filepath.ToSlash(stage)), []byte(filepath.ToSlash(root)))
		}
	}
	var listener subscriptionSettings
	if data := files["subscription.json"]; data != nil {
		if err := json.Unmarshal(data, &listener); err != nil {
			return nil, err
		}
	}
	listener.Enabled = false
	listener.Token = randomSecret()
	if err := ensureSubscriptionGrant(&c, listener.Token); err != nil {
		return nil, err
	}
	if err := files.json(subscriptionsConfig, c); err != nil {
		return nil, err
	}
	if err := files.json("subscription.json", listener); err != nil {
		return nil, err
	}
	return files, nil
}

func subscriptionBackupCommand(ctx context.Context, root, core, operation, file string, apply bool, input io.Reader, output io.Writer) error {
	if file == "" {
		return errors.New("backup requires --file")
	}
	password, err := io.ReadAll(io.LimitReader(input, 1025))
	if err != nil {
		return err
	}
	password = bytes.TrimRight(password, "\r\n")
	if len(password) < 12 || len(password) > 1024 {
		return errors.New("provide a 12-1024 byte backup password on stdin")
	}
	defer clear(password)
	if operation == "backup" {
		var b subscriptionBackup
		if err := withStartupLock(ctx, root, func() error { var err error; b, err = collectSubscriptionBackup(root); return err }); err != nil {
			return err
		}
		data, err := json.Marshal(b)
		if err != nil {
			return err
		}
		if len(data) > sub.MaxBody {
			return errors.New("backup too large")
		}
		encrypted, err := encryptSubscriptionBackup(data, password)
		clear(data)
		if err != nil {
			return err
		}
		return AtomicBytes(file, encrypted, 0600)
	}
	data, err := readLimitedRegularFile(file, sub.MaxBody+52)
	if err != nil {
		return err
	}
	plain, err := decryptSubscriptionBackup(data, password)
	if err != nil {
		return err
	}
	defer clear(plain)
	var b subscriptionBackup
	if err := strictSubscriptionJSON(plain, &b); err != nil {
		return err
	}
	return withOperationLock(root, func() error {
		var files fileBundle
		if err := withStartupLock(ctx, root, func() error { var err error; files, err = prepareSubscriptionRestore(ctx, root, core, b); return err }); err != nil {
			return err
		}
		if !apply {
			names, err := bundleNames(files)
			if err != nil {
				return err
			}
			return jsonEncoder(output).Encode(Object{"files": names, "sharing": "disabled; all previous grants remain unusable", "apply": "repeat with --apply"})
		}
		if runtime.GOOS != "linux" {
			return errors.New("applying a backup requires the Linux installation; preview is available on all platforms")
		}
		if err := applySubscriptionRestore(ctx, root, files, systemRestoreServices{root: root}); err != nil {
			return err
		}
		_, err = fmt.Fprintln(output, "备份已恢复，已应用到原有活动服务；订阅已关闭，重新启用后生成新分享。")
		return err
	})
}

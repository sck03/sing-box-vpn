package manage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/adminauth"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
	"github.com/sck03/sing-box-vpn/internal/database"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

const platformBackupMagic = "SBVPNV04"
const maxPlatformBackup = 96 << 20

type platformBackup struct {
	Version  int               `json:"version"`
	Root     string            `json:"root"`
	Database []byte            `json:"database"`
	Files    map[string][]byte `json:"files"`
	Created  time.Time         `json:"created"`
}

func (s *PlatformService) Backup(ctx context.Context, password []byte) ([]byte, error) {
	if len(password) < 12 || len(password) > 1024 {
		return nil, errors.New("backup password must contain 12–1024 bytes")
	}
	stage, err := os.MkdirTemp(s.Root, ".backup-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	snapshot := filepath.Join(stage, "snapshot.db")
	db, err := database.Open(filepath.Join(s.Root, database.Filename))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	// VACUUM INTO obtains a transactionally consistent online snapshot including
	// outstanding quota reservations; it never copies a live WAL file piecemeal.
	if _, err = db.ExecContext(ctx, `VACUUM INTO ?`, snapshot); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(snapshot)
	if err != nil {
		return nil, err
	}
	if len(data) > 48<<20 {
		return nil, errors.New("database exceeds backup budget")
	}
	backup := platformBackup{Version: 4, Root: s.Root, Database: data, Files: map[string][]byte{}, Created: time.Now().UTC()}
	copyDB, err := database.Open(snapshot)
	if err != nil {
		return nil, err
	}
	defer copyDB.Close()
	var desired, applied []byte
	if err = copyDB.QueryRowContext(ctx, `SELECT desired,applied FROM control_state WHERE id=1`).Scan(&desired, &applied); err != nil {
		return nil, err
	}
	for _, raw := range [][]byte{desired, applied} {
		c, e := decodePlatform(raw)
		if e != nil {
			return nil, e
		}
		for _, path := range certificatePaths(c) {
			rel, e := filepath.Rel(s.Root, path)
			if e != nil || !filepath.IsLocal(rel) {
				return nil, errors.New("backup requires certificate files inside the managed root")
			}
			rel = filepath.ToSlash(rel)
			if _, ok := backup.Files[rel]; ok {
				continue
			}
			value, e := os.ReadFile(path)
			if errors.Is(e, os.ErrNotExist) && pendingCertificatePath(c, path) {
				continue
			}
			if e != nil {
				return nil, e
			}
			if len(value) > 1<<20 || len(backup.Files) >= 2048 {
				return nil, errors.New("certificate backup budget exceeded")
			}
			backup.Files[rel] = value
		}
	}
	plain, err := json.Marshal(backup)
	if err != nil {
		return nil, err
	}
	if len(plain) > maxPlatformBackup-128 {
		return nil, errors.New("backup exceeds 96 MiB")
	}
	salt := randomBytes(16)
	cipher, err := backupCipher(password, salt)
	if err != nil {
		return nil, err
	}
	header := append([]byte(platformBackupMagic), salt...)
	nonce := randomBytes(cipher.NonceSize())
	header = append(header, nonce...)
	return cipher.Seal(header, nonce, plain, header), nil
}
func pendingCertificatePath(c PlatformConfig, path string) bool {
	for _, in := range c.App.Inbounds {
		tls := object(in["tls"])
		if text(tls["certificate_path"]) == path || text(tls["key_path"]) == path {
			return false
		}
	}
	for _, p := range c.VPN.Profiles {
		if p.Certificate == path || p.Key == path {
			return false
		}
	}
	for _, a := range c.Certificates {
		if a.Provider == "certbot" && (a.Certificate == path || a.Key == path) {
			return true
		}
	}
	return false
}
func certificatePaths(c PlatformConfig) []string {
	paths := []string{}
	add := func(v string) {
		if v != "" {
			paths = append(paths, v)
		}
	}
	for _, in := range c.App.Inbounds {
		t := object(in["tls"])
		add(text(t["certificate_path"]))
		add(text(t["key_path"]))
	}
	for _, p := range c.VPN.Profiles {
		add(p.Certificate)
		add(p.Key)
	}
	for _, a := range c.Certificates {
		add(a.Certificate)
		add(a.Key)
	}
	return paths
}
func decodePlatformBackup(data, password []byte) (platformBackup, error) {
	var backup platformBackup
	if len(password) < 12 || len(password) > 1024 || len(data) < 52 || len(data) > maxPlatformBackup || string(data[:8]) != platformBackupMagic {
		return backup, errors.New("invalid v4 encrypted backup")
	}
	cipher, err := backupCipher(password, data[8:24])
	if err != nil {
		return backup, err
	}
	plain, err := cipher.Open(nil, data[24:36], data[36:], data[:36])
	if err != nil {
		return backup, errors.New("backup password incorrect or archive damaged")
	}
	if err = strictSubscriptionJSON(plain, &backup); err != nil {
		return backup, err
	}
	if backup.Version != 4 || len(backup.Database) > 48<<20 || len(backup.Files) > 2048 {
		return backup, errors.New("unsupported backup format or size")
	}
	for path, value := range backup.Files {
		if !filepath.IsLocal(path) || filepath.ToSlash(filepath.Clean(path)) != path || strings.Contains(path, "\\") || len(value) > 1<<20 || (!strings.HasSuffix(path, ".pem") && !strings.HasSuffix(path, ".key")) {
			return backup, errors.New("invalid certificate backup entry")
		}
	}
	return backup, nil
}
func inspectBackupDatabase(ctx context.Context, path string, data []byte) (PlatformConfig, Object, error) {
	if err := AtomicBytes(path, data, 0600); err != nil {
		return PlatformConfig{}, nil, err
	}
	db, err := database.Open(path)
	if err != nil {
		return PlatformConfig{}, nil, err
	}
	defer db.Close()
	var check string
	if err = db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&check); err != nil || check != "ok" {
		return PlatformConfig{}, nil, errors.New("backup database integrity check failed")
	}
	// Compare to this release's schema before executing any restored SQL writes.
	// Views, triggers, virtual tables and modified constraints are not imported.
	expectedPath := filepath.Join(filepath.Dir(path), "expected-schema.db")
	control, err := controlstore.Open(expectedPath)
	if err != nil {
		return PlatformConfig{}, nil, err
	}
	control.Close()
	auth, err := adminauth.Open(expectedPath)
	if err != nil {
		return PlatformConfig{}, nil, err
	}
	auth.Close()
	accounts, err := accounting.Open(expectedPath)
	if err != nil {
		return PlatformConfig{}, nil, err
	}
	accounts.Close()
	expected, err := database.Open(expectedPath)
	if err != nil {
		return PlatformConfig{}, nil, err
	}
	defer expected.Close()
	readSchema := func(d *sql.DB) ([]string, error) {
		rows, e := d.QueryContext(ctx, `SELECT type||':'||name||':'||COALESCE(sql,'') FROM sqlite_schema ORDER BY type,name`)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		result := []string{}
		for rows.Next() {
			var value string
			if e = rows.Scan(&value); e != nil {
				return nil, e
			}
			result = append(result, value)
		}
		return result, rows.Err()
	}
	wanted, err := readSchema(expected)
	if err != nil {
		return PlatformConfig{}, nil, err
	}
	actual, err := readSchema(db)
	if err != nil || !reflect.DeepEqual(actual, wanted) {
		return PlatformConfig{}, nil, errors.New("backup database schema does not match this release")
	}
	var raw []byte
	if err = db.QueryRowContext(ctx, `SELECT applied FROM control_state WHERE id=1`).Scan(&raw); err != nil {
		return PlatformConfig{}, nil, err
	}
	c, err := decodePlatform(raw)
	if err != nil {
		return c, nil, err
	}
	summary := Object{}
	for _, table := range []string{"accounts", "credentials", "account_grants", "administrators", "usage_periods"} {
		var count int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil {
			return c, nil, err
		}
		summary[table] = count
	}
	return c, summary, nil
}
func relocateBackup(c *PlatformConfig, b platformBackup, destination string) error {
	mapPath := func(path string) (string, error) {
		if path == "" {
			return "", nil
		}
		rel, err := filepath.Rel(b.Root, path)
		if err != nil || !filepath.IsLocal(rel) {
			return "", errors.New("certificate path escapes backup root")
		}
		rel = filepath.ToSlash(rel)
		if _, ok := b.Files[rel]; !ok {
			return "", fmt.Errorf("backup is missing certificate %s", rel)
		}
		return filepath.ToSlash(filepath.Join(destination, filepath.FromSlash(rel))), nil
	}
	if err := mapAppCertificates(&c.App, mapPath); err != nil {
		return err
	}
	for cc, p := range c.VPN.Profiles {
		var err error
		p.Certificate, err = mapPath(p.Certificate)
		if err != nil {
			return err
		}
		p.Key, err = mapPath(p.Key)
		if err != nil {
			return err
		}
		c.VPN.Profiles[cc] = p
	}
	for i := range c.Certificates {
		a := &c.Certificates[i]
		if pendingCertificatePath(*c, a.Certificate) {
			rel, _ := filepath.Rel(b.Root, a.Certificate)
			if _, ok := b.Files[filepath.ToSlash(rel)]; !ok {
				a.Certificate = filepath.ToSlash(filepath.Join(destination, "certificates", a.ID, "cert.pem"))
				a.Key = filepath.ToSlash(filepath.Join(destination, "certificates", a.ID, "key.pem"))
				continue
			}
		}
		var err error
		a.Certificate, err = mapPath(a.Certificate)
		if err != nil {
			return err
		}
		a.Key, err = mapPath(a.Key)
		if err != nil {
			return err
		}
		if a.Provider == "certbot" {
			a.Provider = "import"
		}
	}
	return nil
}
func (s *PlatformService) servePlatformBackup(w http.ResponseWriter, r *http.Request, actor string) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var v struct {
		Password string `json:"password"`
		Backup   []byte `json:"backup"`
		Apply    bool   `json:"apply"`
		Revision int64  `json:"revision"`
	}
	if err := decodeRequest(w, r, &v, 128<<20); err != nil {
		platformError(w, err)
		return
	}
	if r.URL.Path == "/api/backup" {
		data, err := s.Backup(r.Context(), []byte(v.Password))
		if err != nil {
			platformError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="sing-box-vpn-v4.sbvpn"`)
		_, _ = w.Write(data)
		return
	}
	b, err := decodePlatformBackup(v.Backup, []byte(v.Password))
	if err != nil {
		platformError(w, err)
		return
	}
	stage, err := os.MkdirTemp(s.Root, ".restore-inspect-")
	if err != nil {
		platformError(w, err)
		return
	}
	defer os.RemoveAll(stage)
	c, summary, err := inspectBackupDatabase(r.Context(), filepath.Join(stage, "snapshot.db"), b.Database)
	if err != nil {
		platformError(w, err)
		return
	}
	if !v.Apply {
		previewConfig := c
		if err = relocateBackup(&previewConfig, b, stage); err == nil {
			err = previewConfig.Validate()
		}
		if err != nil {
			platformError(w, err)
			return
		}
		summary["mode"] = "same-host configuration restore; current accounts, usage, credentials, administrators and revocations are retained"
		summary["created"] = b.Created
		jsonResponse(w, summary)
		return
	}
	destination, err := os.MkdirTemp(filepath.Join(s.Root, "certificates"), "restored-")
	if err != nil {
		_ = os.MkdirAll(filepath.Join(s.Root, "certificates"), 0700)
		destination, err = os.MkdirTemp(filepath.Join(s.Root, "certificates"), "restored-")
	}
	if err != nil {
		platformError(w, err)
		return
	}
	if err = relocateBackup(&c, b, destination); err == nil {
		for i := range c.Certificates {
			a := &c.Certificates[i]
			if a.Provider == "certbot" {
				a.Certificate = filepath.ToSlash(filepath.Join(s.Root, "certificates", a.ID, "cert.pem"))
				a.Key = filepath.ToSlash(filepath.Join(s.Root, "certificates", a.ID, "key.pem"))
			}
		}
		for path, data := range b.Files {
			if err = AtomicBytes(filepath.Join(destination, filepath.FromSlash(path)), data, 0600); err != nil {
				break
			}
		}
	}
	if err == nil {
		var id int64
		current, e := s.Store.State(r.Context())
		if e != nil {
			platformError(w, e)
			return
		}
		live, e := decodePlatform(current.Desired)
		if e != nil {
			platformError(w, e)
			return
		}
		// Download revocations are security state, even for third-party outputs.
		c.Subscriptions.Grants = live.Subscriptions.Grants
		id, err = s.QueueApply(r.Context(), c, v.Revision, actor)
		if err == nil {
			jsonResponse(w, Object{"task_id": id, "ledger": "current ledger and revocations retained"})
			return
		}
	}
	_ = os.RemoveAll(destination)
	platformError(w, err)
}

// RestoreDisaster accepts an empty root only. All proxy users are disabled and
// all download/session tokens are revoked until the operator reconciles usage.
func RestoreDisaster(ctx context.Context, root string, data, password []byte, admin, newPassword string) error {
	if entries, err := os.ReadDir(root); err == nil && len(entries) > 0 {
		return errors.New("disaster recovery requires an empty destination")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	b, err := decodePlatformBackup(data, password)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(root), 0700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(root), ".disaster-restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	path := filepath.Join(stage, database.Filename)
	c, _, err := inspectBackupDatabase(ctx, path, b.Database)
	if err != nil {
		return err
	}
	if err = relocateBackup(&c, b, root); err != nil {
		return err
	}
	for i := range c.Subscriptions.Grants {
		c.Subscriptions.Grants[i].Revoked = true
	}
	for name, value := range b.Files {
		if err = AtomicBytes(filepath.Join(stage, filepath.FromSlash(name)), value, 0600); err != nil {
			return err
		}
	}
	db, err := database.Open(path)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `DELETE FROM admin_sessions; DELETE FROM administrators; UPDATE accounts SET disabled=1,revision=revision+1; UPDATE account_grants SET revoked=1; DELETE FROM control_tasks; DELETE FROM runtime_state;`)
	if err == nil {
		raw, e := encodePlatform(c)
		err = e
		if err == nil {
			_, err = db.ExecContext(ctx, `UPDATE control_state SET desired=?,applied=?,revision=revision+1,applied_revision=revision+1 WHERE id=1`, raw, raw)
		}
	}
	db.Close()
	if err != nil {
		return err
	}
	auth, err := adminauth.Open(path)
	if err != nil {
		return err
	}
	if err = auth.Bootstrap(ctx, admin, newPassword, false); err != nil {
		auth.Close()
		return err
	}
	if err = auth.Close(); err != nil {
		return err
	}
	if err = AtomicJSON(filepath.Join(stage, "panel.json"), PanelOptions{Listen: "127.0.0.1:2096"}); err != nil {
		return err
	}
	if err = AtomicBytes(filepath.Join(stage, ".platform-v4"), []byte("recovered; reconcile disabled entitlements before activation\n"), 0600); err != nil {
		return err
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		_ = os.Remove(filepath.Join(stage, "expected-schema.db"+suffix))
	}
	// Remove only an empty destination. A concurrent writer prevents publication.
	if err = os.Remove(root); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(stage, root)
}

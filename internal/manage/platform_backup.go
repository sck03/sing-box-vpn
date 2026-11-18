package manage

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/adminauth"
	"github.com/sck03/sing-box-vpn/internal/backuparchive"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
	"github.com/sck03/sing-box-vpn/internal/database"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"time"
)

const maxPlatformBackup = backuparchive.MaxArchive

// Binary multipart upload plus a bounded metadata envelope.
const maxPlatformBackupRequest = maxPlatformBackup + (16 << 10)

type platformBackup = backuparchive.Archive

func (s *PlatformService) Backup(ctx context.Context, password []byte) ([]byte, error) {
	var out bytes.Buffer
	err := s.BackupTo(ctx, &out, password)
	return out.Bytes(), err
}

func (s *PlatformService) BackupTo(ctx context.Context, out io.Writer, password []byte) error {
	if len(password) < 12 || len(password) > 1024 {
		return errors.New("backup password must contain 12–1024 bytes")
	}
	stage, err := os.MkdirTemp(s.Root, ".backup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	snapshot := filepath.Join(stage, "snapshot.db")
	files := map[string]string{}
	// Publication and snapshot capture share a lock; encryption and download do
	// not hold it. The SQL snapshot and all referenced pairs form one generation.
	err = withStartupLock(ctx, s.Root, func() error {
		return s.captureBackup(ctx, stage, snapshot, files)
	})
	if err != nil {
		return err
	}
	return backuparchive.Write(ctx, out, password, backuparchive.Manifest{Root: s.Root, Created: time.Now().UTC()}, snapshot, files)
}

func (s *PlatformService) captureBackup(ctx context.Context, stage, snapshot string, files map[string]string) error {
	db, err := database.Open(filepath.Join(s.Root, database.Filename))
	if err != nil {
		return err
	}
	defer db.Close()
	// VACUUM INTO obtains a transactionally consistent online snapshot including
	// outstanding quota reservations; it never copies a live WAL file piecemeal.
	if _, err = db.ExecContext(ctx, `VACUUM INTO ?`, snapshot); err != nil {
		return err
	}
	copyDB, err := database.Open(snapshot)
	if err != nil {
		return err
	}
	defer copyDB.Close()
	var desired, applied []byte
	if err = copyDB.QueryRowContext(ctx, `SELECT desired,applied FROM control_state WHERE id=1`).Scan(&desired, &applied); err != nil {
		return err
	}
	for _, raw := range [][]byte{desired, applied} {
		c, e := decodePlatform(raw)
		if e != nil {
			return e
		}
		for _, path := range certificatePaths(c) {
			rel, e := filepath.Rel(s.Root, path)
			if e != nil || !filepath.IsLocal(rel) {
				return errors.New("backup requires certificate files inside the managed root")
			}
			rel = filepath.ToSlash(rel)
			if _, ok := files[rel]; ok {
				continue
			}
			value, e := readLimitedRegularFile(path, 1<<20)
			if errors.Is(e, os.ErrNotExist) && pendingCertificatePath(c, path) {
				continue
			}
			if e != nil {
				return e
			}
			if len(value) > backuparchive.MaxFile || len(files) >= backuparchive.MaxFiles {
				return errors.New("certificate backup budget exceeded")
			}
			staged := filepath.Join(stage, "files", filepath.FromSlash(rel))
			if e = AtomicBytes(staged, value, 0600); e != nil {
				return e
			}
			files[rel] = staged
		}
		if err = validateBackupPairs(c, s.Root, files); err != nil {
			return err
		}
	}
	return nil
}

func validateBackupPairs(c PlatformConfig, root string, files map[string]string) error {
	return validateCertificatePairs(c, func(path string) ([]byte, error) {
		rel, err := filepath.Rel(root, path)
		if err != nil || !filepath.IsLocal(rel) {
			return nil, errors.New("invalid certificate pair path")
		}
		staged, ok := files[filepath.ToSlash(rel)]
		if !ok {
			return nil, os.ErrNotExist
		}
		return os.ReadFile(staged)
	})
}

func validateArchivedPairs(c PlatformConfig, b platformBackup) error {
	return validateCertificatePairs(c, func(path string) ([]byte, error) {
		rel, err := filepath.Rel(b.Root, path)
		if err != nil || !filepath.IsLocal(rel) {
			return nil, errors.New("invalid certificate archive path")
		}
		data, ok := b.Files[filepath.ToSlash(rel)]
		if !ok {
			return nil, os.ErrNotExist
		}
		return data, nil
	})
}

func validateCertificatePairs(c PlatformConfig, read func(string) ([]byte, error)) error {
	pairs := [][2]string{{c.App.Profile.Certificate, c.App.Profile.Key}}
	for _, in := range c.App.Inbounds {
		t := object(in["tls"])
		pairs = append(pairs, [2]string{text(t["certificate_path"]), text(t["key_path"])})
	}
	for _, p := range c.VPN.Profiles {
		pairs = append(pairs, [2]string{p.Certificate, p.Key})
	}
	for _, a := range c.Certificates {
		pairs = append(pairs, [2]string{a.Certificate, a.Key})
	}
	for _, pair := range pairs {
		if pair[0] == "" && pair[1] == "" {
			continue
		}
		cert, certErr := read(pair[0])
		key, keyErr := read(pair[1])
		if errors.Is(certErr, os.ErrNotExist) && errors.Is(keyErr, os.ErrNotExist) && pendingCertificatePath(c, pair[0]) && pendingCertificatePath(c, pair[1]) {
			continue
		}
		if certErr != nil {
			return certErr
		}
		if keyErr != nil {
			return keyErr
		}
		if _, e := tls.X509KeyPair(cert, key); e != nil {
			return fmt.Errorf("backup certificate pair: %w", e)
		}
	}
	return nil
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
		if automaticCertificate(a) && (a.Certificate == path || a.Key == path) {
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
	add(c.App.Profile.Certificate)
	add(c.App.Profile.Key)
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
	for _, table := range []string{"accounts", "credentials", "download_grants", "administrators", "usage_periods"} {
		var count int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil {
			return c, nil, err
		}
		summary[table] = count
	}
	return c, summary, nil
}
func relocateBackup(c *PlatformConfig, b platformBackup, destination string) error {
	if err := validateArchivedPairs(*c, b); err != nil {
		return err
	}
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
	}
	return nil
}
func (s *PlatformService) servePlatformBackup(w http.ResponseWriter, r *http.Request, actor string) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	if !s.backupBusy.CompareAndSwap(false, true) {
		w.Header().Set("Retry-After", "5")
		http.Error(w, "another backup or restore is running", http.StatusTooManyRequests)
		return
	}
	defer s.backupBusy.Store(false)
	var v backupRequest
	defer v.Close()
	if err := readBackupRequest(w, r, &v); err != nil {
		platformError(w, err)
		return
	}
	if r.URL.Path == "/api/backup" {
		file, err := os.CreateTemp(s.Root, ".backup-download-*")
		if err != nil {
			platformError(w, err)
			return
		}
		defer func() { file.Close(); os.Remove(file.Name()) }()
		err = s.BackupTo(r.Context(), file, []byte(v.Password))
		if err != nil {
			platformError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="sing-box-vpn-v4.sbvpn"`)
		if _, err = file.Seek(0, io.SeekStart); err != nil {
			platformError(w, err)
			return
		}
		_, _ = io.Copy(w, file)
		return
	}
	b, err := backuparchive.ReadFrom(v.Backup, []byte(v.Password))
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
		state, err := s.Store.State(r.Context())
		if err != nil {
			platformError(w, err)
			return
		}
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
		summary["revision"] = state.Revision
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
		for path, data := range b.Files {
			if err = AtomicBytes(filepath.Join(destination, filepath.FromSlash(path)), data, 0600); err != nil {
				break
			}
		}
	}
	if err == nil {
		err = AtomicBytes(filepath.Join(destination, ".managed-material"), []byte("sing-box-vpn restored certificates\n"), 0600)
	}
	if err == nil {
		var id int64
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
	return restoreDisasterFrom(ctx, root, bytes.NewReader(data), password, admin, newPassword)
}

func restoreDisasterFrom(ctx context.Context, root string, source io.Reader, password []byte, admin, newPassword string) error {
	if entries, err := os.ReadDir(root); err == nil && len(entries) > 0 {
		return errors.New("disaster recovery requires an empty destination")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	b, err := backuparchive.ReadFrom(source, password)
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
	for name, value := range b.Files {
		if err = AtomicBytes(filepath.Join(stage, filepath.FromSlash(name)), value, 0600); err != nil {
			return err
		}
	}
	db, err := database.Open(path)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `DELETE FROM admin_sessions; DELETE FROM administrators; UPDATE accounts SET disabled=1,revision=revision+1; UPDATE download_grants SET revoked=1; DELETE FROM control_tasks; DELETE FROM runtime_state; UPDATE credential_publications SET published=0,loaded=0;`)
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

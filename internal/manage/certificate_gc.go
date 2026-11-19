package manage

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Desired and applied references protect pending apply/rollback. Seven days
// also protect certificates imported into an unsaved browser draft. Only
// immutable imported/restored directories with our ownership marker qualify.
func (s *PlatformService) pruneCertificates(ctx context.Context, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return withStartupLock(ctx, s.Root, func() error {
		state, err := s.Store.State(ctx)
		if err != nil {
			return err
		}
		base := filepath.Join(s.Root, "certificates")
		info, err := os.Lstat(base)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return errors.New("certificate material root must be a real directory")
		}
		keep := map[string]bool{}
		panels, err := s.panelCertificateRefs()
		if err != nil {
			return err
		}
		var paths []string
		for _, panel := range panels {
			paths = append(paths, panel.Certificate, panel.Key)
		}
		for _, name := range paths {
			rel, e := filepath.Rel(base, name)
			if e == nil && filepath.IsLocal(rel) {
				keep[strings.Split(filepath.ToSlash(rel), "/")[0]] = true
			}
		}
		for _, raw := range [][]byte{state.Desired, state.Applied} {
			c, e := decodePlatform(raw)
			if e != nil {
				return e
			}
			for _, name := range certificatePaths(c) {
				rel, e := filepath.Rel(base, name)
				if e == nil && filepath.IsLocal(rel) {
					keep[strings.Split(filepath.ToSlash(rel), "/")[0]] = true
				}
			}
		}
		entries, err := os.ReadDir(base)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !entry.IsDir() || keep[entry.Name()] {
				continue
			}
			dir := filepath.Join(base, entry.Name())
			marker, err := os.Lstat(filepath.Join(dir, ".managed-material"))
			if err != nil || !marker.Mode().IsRegular() || now.Sub(marker.ModTime()) < 7*24*time.Hour {
				continue
			}
			if err = ctx.Err(); err != nil {
				return err
			}
			if err = os.RemoveAll(dir); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *PlatformService) panelCertificateRefs() (map[string]PanelOptions, error) {
	saved, _, err := s.panelSettings()
	if err != nil {
		return nil, err
	}
	refs := map[string]PanelOptions{"面板（已保存）": saved}
	var restart panelRestartReceipt
	if exists, err := ReadJSON(filepath.Join(s.Root, panelRestartFile), &restart); err != nil {
		return nil, err
	} else if exists && restart.Outcome == "pending" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		task, err := s.Store.Task(ctx, restart.TaskID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if err == nil && (task.State == "queued" || task.State == "running") {
			refs["面板（重启回滚）"] = restart.Previous
		}
	}
	s.panelMu.RLock()
	defer s.panelMu.RUnlock()
	if s.activePanel != nil {
		refs["面板（运行中）"] = *s.activePanel
	}
	return refs, nil
}

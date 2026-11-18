package manage

import (
	"context"
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
		panel, _, err := s.panelSettings()
		if err != nil {
			return err
		}
		paths := []string{panel.Certificate, panel.Key}
		s.panelMu.RLock()
		if s.activePanel != nil {
			paths = append(paths, s.activePanel.Certificate, s.activePanel.Key)
		}
		s.panelMu.RUnlock()
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

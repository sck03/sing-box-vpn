package deployment

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// PruneUpgrades only removes successful, owned sibling directories. Failed or
// interrupted upgrades are diagnostic material and never qualify for pruning.
func PruneUpgrades(home string, keep int) error {
	if keep < 1 {
		return errors.New("at least one rollback must be retained")
	}
	home, err := filepath.Abs(home)
	if err != nil {
		return err
	}
	parent := filepath.Dir(home)
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return err
	}
	if resolved != parent {
		return errors.New("upgrade parent must not contain symbolic links")
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return err
	}
	type backup struct {
		path string
		at   time.Time
	}
	var candidates []backup
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), filepath.Base(home)+".update.") {
			continue
		}
		path := filepath.Join(parent, entry.Name())
		marker := func(name, expected string) bool {
			p := filepath.Join(path, name)
			info, e := os.Lstat(p)
			if e != nil || !info.Mode().IsRegular() || info.Size() > 128 {
				return false
			}
			data, e := os.ReadFile(p)
			return e == nil && strings.TrimSpace(string(data)) == expected
		}
		if !marker(".upgrade-owned", "sing-box-vpn upgrade") || !marker(".upgrade-success", "sing-box-vpn upgrade succeeded") {
			continue
		}
		info, e := os.Stat(filepath.Join(path, ".upgrade-success"))
		if e != nil {
			return e
		}
		candidates = append(candidates, backup{path, info.ModTime()})
	}
	slices.SortFunc(candidates, func(a, b backup) int {
		if c := b.at.Compare(a.at); c != 0 {
			return c
		}
		return strings.Compare(b.path, a.path)
	})
	for _, candidate := range candidates[min(keep, len(candidates)):] {
		// Recheck immediately before removal; never follow a replaced directory link.
		info, e := os.Lstat(candidate.path)
		if e != nil {
			return e
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || filepath.Dir(candidate.path) != parent {
			return errors.New("upgrade directory changed")
		}
		if e = os.RemoveAll(candidate.path); e != nil {
			return e
		}
	}
	return nil
}

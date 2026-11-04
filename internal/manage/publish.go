package manage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const journalName = ".pending-config.json"

type commitJournal struct {
	Before fileBundle `json:"before"`
	Stage  string     `json:"stage"`
}

// Services may start together; recovery waits for a writer instead of failing
// one service merely because another startup is recovering the same bundle.
func recoverConfig(ctx context.Context, root string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := withLock(root, func() error { return nil })
		if !errors.Is(err, errLockBusy) {
			return err
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func bundleNames(files fileBundle) ([]string, error) {
	names := make([]string, 0, len(files))
	for name := range files {
		if !filepath.IsLocal(name) || filepath.ToSlash(filepath.Clean(name)) != name || name == "." || name == journalName || name == ".manage.lock" || strings.HasPrefix(name, ".commit-") {
			return nil, errors.New("无效配置路径")
		}
		names = append(names, name)
	}
	slices.Sort(names)
	return names, nil
}

func syncBundleDirectories(root string, names []string) error {
	root = filepath.Clean(root)
	dirs := map[string]bool{root: true}
	for _, name := range names {
		for dir := filepath.Dir(filepath.Join(root, name)); dir != root && !dirs[dir]; dir = filepath.Dir(dir) {
			dirs[dir] = true
		}
	}
	for dir := range dirs {
		if err := syncDirectory(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func removeFile(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Recovery is idempotent: keep the journal until every previous file is restored.
func recoverPublish(root string) error {
	path := filepath.Join(root, journalName)
	var journal commitJournal
	found, err := ReadJSON(path, &journal)
	if err != nil || !found {
		return err
	}
	names, err := bundleNames(journal.Before)
	if err != nil || len(names) == 0 || filepath.Base(journal.Stage) != journal.Stage || !strings.HasPrefix(journal.Stage, ".commit-") {
		return errors.New("未完成配置的恢复记录无效，已保留记录")
	}
	stage := filepath.Join(root, journal.Stage)
	if info, err := os.Lstat(stage); err == nil && !info.IsDir() {
		return errors.New("配置暂存目录类型错误，已保留恢复记录")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, name := range names {
		path := filepath.Join(root, name)
		if before := journal.Before[name]; before != nil {
			err = AtomicBytes(path, before, 0600)
		} else {
			err = removeFile(path)
		}
		if err != nil {
			return fmt.Errorf("配置恢复失败，已保留恢复记录: %w", err)
		}
	}
	if err := syncBundleDirectories(root, names); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	if err := syncDirectory(root); err != nil {
		return err
	}
	return os.RemoveAll(stage)
}

// Callers validate the whole bundle first; unchanged files avoid staging and fsync.
func publish(root string, files fileBundle) error {
	if err := recoverPublish(root); err != nil {
		return err
	}
	names, err := bundleNames(files)
	if err != nil {
		return err
	}
	before := fileBundle{}
	changed := make([]string, 0, len(names))
	for _, name := range names {
		path := filepath.Join(root, name)
		info, err := os.Lstat(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		var data []byte
		if err == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("配置文件不是普通文件: %s", name)
			}
			if data, err = os.ReadFile(path); err != nil {
				return err
			}
			if files[name] != nil && bytes.Equal(data, files[name]) {
				if info.Mode().Perm() != 0600 {
					if err := os.Chmod(path, 0600); err != nil {
						return err
					}
				}
				continue
			}
		} else if files[name] == nil {
			continue
		}
		before[name] = data
		changed = append(changed, name)
	}
	if len(changed) == 0 {
		return nil
	}
	stage, err := os.MkdirTemp(root, ".commit-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, name := range changed {
		if data := files[name]; data != nil {
			if err := AtomicBytes(filepath.Join(stage, name), data, 0600); err != nil {
				return err
			}
		}
	}
	journal := filepath.Join(root, journalName)
	if err := AtomicJSON(journal, commitJournal{Before: before, Stage: filepath.Base(stage)}); err != nil {
		return err
	}
	if err := syncDirectory(root); err != nil {
		return err
	}
	for _, name := range changed {
		path := filepath.Join(root, name)
		if files[name] == nil {
			err = removeFile(path)
		} else if err = os.MkdirAll(filepath.Dir(path), 0700); err == nil {
			err = os.Rename(filepath.Join(stage, name), path)
		}
		if err != nil {
			return errors.Join(err, recoverPublish(root))
		}
	}
	if err := syncBundleDirectories(root, changed); err != nil {
		return errors.Join(err, recoverPublish(root))
	}
	if err := os.Remove(journal); err != nil {
		return errors.Join(err, recoverPublish(root))
	}
	if err := syncDirectory(root); err != nil {
		restoreErr := AtomicJSON(journal, commitJournal{Before: before, Stage: filepath.Base(stage)})
		if restoreErr == nil {
			restoreErr = recoverPublish(root)
		}
		return errors.Join(err, restoreErr)
	}
	return nil
}

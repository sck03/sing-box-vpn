package manage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const journalName = ".pending-config.json"
const snapshotStage = ".commit-current"
const snapshotOwner = "sck03/sing-box-vpn configuration snapshot\n"

type commitJournal struct {
	// Empty fingerprints denote files that did not exist before this commit.
	Files map[string]string `json:"files"`
	Stage string            `json:"stage"`
}

func recoverConfig(ctx context.Context, root string) error {
	return withStartupLock(ctx, root, func() error { return nil })
}

func withStartupLock(ctx context.Context, root string, operation func() error) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := withConfigLock(root, operation)
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

func bundleNames[T any](files map[string]T) ([]string, error) {
	names := make([]string, 0, len(files))
	for name := range files {
		if !filepath.IsLocal(name) || filepath.ToSlash(filepath.Clean(name)) != name || name == "." || name == journalName || name == ".manage.lock" || name == ".operation.lock" || strings.HasPrefix(name, ".commit-") {
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

func (j commitJournal) names() ([]string, error) {
	names, err := bundleNames(j.Files)
	if err != nil || len(names) == 0 || j.Stage != snapshotStage {
		return nil, errors.New("未完成配置的恢复记录无效，已保留记录")
	}
	for _, digest := range j.Files {
		if digest != "" {
			bytes, err := hex.DecodeString(digest)
			if err != nil || len(bytes) != sha256.Size {
				return nil, errors.New("恢复备份校验值无效，已保留记录")
			}
		}
	}
	return names, nil
}

// Keep the single snapshot until both the record removal and directory sync
// complete. Failed recovery never discards its backups.
func finishJournal(root string, j commitJournal) error {
	if err := os.Remove(filepath.Join(root, journalName)); err != nil {
		return err
	}
	if err := syncDirectory(root); err != nil {
		return errors.Join(err, AtomicJSON(filepath.Join(root, journalName), j))
	}
	// The commit/recovery is complete; leftover staging data is no longer active.
	_ = os.RemoveAll(filepath.Join(root, j.Stage))
	return nil
}

func recoverPublish(root string) error {
	var j commitJournal
	found, err := ReadJSON(filepath.Join(root, journalName), &j)
	if err != nil || !found {
		return err
	}
	names, err := j.names()
	if err != nil {
		return err
	}
	stage := filepath.Join(root, j.Stage)
	info, err := os.Lstat(stage)
	if err != nil || !info.IsDir() {
		return errors.New("配置恢复暂存目录不可用，已保留记录")
	}
	owner, err := os.ReadFile(filepath.Join(stage, ".owner"))
	if err != nil || string(owner) != snapshotOwner {
		return errors.New("配置恢复暂存目录归属无效，已保留记录")
	}
	// Validate every backup before changing any current file.
	for _, name := range names {
		expected := j.Files[name]
		if expected == "" {
			continue
		}
		digest, err := fingerprintFile(filepath.Join(stage, "before", name))
		if err != nil || digest != expected {
			return fmt.Errorf("恢复备份无效，已保留记录: %s: %w", name, errors.Join(err, errors.New("SHA256 不匹配或备份不可读")))
		}
	}
	for _, name := range names {
		expected := j.Files[name]
		path := filepath.Join(root, name)
		if expected == "" {
			err = removeFile(path)
		} else {
			err = atomicWrite(path, 0600, func(output io.Writer) error {
				file, err := os.Open(filepath.Join(stage, "before", name))
				if err != nil {
					return err
				}
				defer file.Close()
				digest := sha256.New()
				if _, err := copyFileContent(io.MultiWriter(output, digest), file); err != nil {
					return err
				}
				if hex.EncodeToString(digest.Sum(nil)) != expected {
					return errors.New("恢复期间备份发生变化")
				}
				return nil
			})
		}
		if err != nil {
			return fmt.Errorf("配置恢复失败，已保留记录: %w", err)
		}
	}
	if err := syncBundleDirectories(root, names); err != nil {
		return err
	}
	return finishJournal(root, j)
}

// Prepare one snapshot, streaming old files instead of embedding their contents
// in JSON. This phase leaves every current configuration untouched.
func preparePublish(root string, files fileBundle) (j commitJournal, err error) {
	names, err := bundleNames(files)
	if err != nil {
		return j, err
	}
	j.Files = make(map[string]string)
	defer func() {
		if err != nil && j.Stage != "" {
			_ = os.RemoveAll(filepath.Join(root, j.Stage))
		}
	}()
	for _, name := range names {
		if err = j.stageFile(root, name, files[name]); err != nil {
			return j, err
		}
	}
	if j.Stage != "" {
		staged := make([]string, 0, 2*len(j.Files))
		for name, digest := range j.Files {
			if digest != "" {
				staged = append(staged, "before/"+name)
			}
			if files[name] != nil {
				staged = append(staged, "next/"+name)
			}
		}
		if err = syncBundleDirectories(filepath.Join(root, j.Stage), staged); err != nil {
			return j, err
		}
	}
	return j, nil
}

func createSnapshot(root string) error {
	if _, err := os.Lstat(filepath.Join(root, journalName)); err == nil {
		return errors.New("当前配置快照尚未恢复")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	stage := filepath.Join(root, snapshotStage)
	info, err := os.Lstat(stage)
	if err == nil {
		if !info.IsDir() {
			return errors.New("配置快照路径类型错误")
		}
		owner, err := os.ReadFile(filepath.Join(stage, ".owner"))
		if err != nil || string(owner) != snapshotOwner {
			return errors.New("未完成暂存目录的归属无法确认")
		}
		// Recovery already handled the active record. This is a draft left by
		// interruption before the record was saved; there is no earlier history.
		if err := os.RemoveAll(stage); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Mkdir(stage, 0700); err != nil {
		return err
	}
	if err := AtomicBytes(filepath.Join(stage, ".owner"), []byte(snapshotOwner), 0600); err != nil {
		_ = os.RemoveAll(stage)
		return err
	}
	return nil
}

func (j *commitJournal) stageFile(root, name string, data []byte) error {
	path := filepath.Join(root, name)
	info, err := os.Lstat(path)
	var file *os.File
	if err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("配置文件不是普通文件: %s", name)
		}
		file, err = os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		if data != nil {
			equal, err := fileEquals(file, info, data)
			if err != nil {
				return err
			}
			if equal {
				if info.Mode().Perm() != 0600 {
					return file.Chmod(0600)
				}
				return nil
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else if data == nil {
		return nil
	}
	if j.Stage == "" {
		if err := createSnapshot(root); err != nil {
			return err
		}
		j.Stage = snapshotStage
	}
	stage := filepath.Join(root, j.Stage)
	digest := ""
	if file != nil {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return err
		}
		hash := sha256.New()
		if err := writeStaged(filepath.Join(stage, "before", name), func(output io.Writer) error {
			_, err := copyFileContent(io.MultiWriter(output, hash), file)
			return err
		}); err != nil {
			return err
		}
		digest = hex.EncodeToString(hash.Sum(nil))
	}
	if data != nil {
		if err := writeStaged(filepath.Join(stage, "next", name), func(output io.Writer) error {
			n, err := output.Write(data)
			if err == nil && n != len(data) {
				return io.ErrShortWrite
			}
			return err
		}); err != nil {
			return err
		}
	}
	j.Files[name] = digest
	return nil
}

func publish(root string, files fileBundle) error {
	if err := recoverPublish(root); err != nil {
		return err
	}
	j, err := preparePublish(root, files)
	if err != nil || len(j.Files) == 0 {
		return err
	}
	stage := filepath.Join(root, j.Stage)
	record := filepath.Join(root, journalName)
	if err := AtomicJSON(record, j); err != nil {
		if _, exists := os.Lstat(record); errors.Is(exists, os.ErrNotExist) {
			_ = os.RemoveAll(stage)
		}
		return err
	}
	// Once the record exists, only completed recovery/commit may remove backups.
	names, err := j.names()
	if err != nil {
		return err
	}
	for _, name := range names {
		path := filepath.Join(root, name)
		if files[name] == nil {
			err = removeFile(path)
		} else if err = os.MkdirAll(filepath.Dir(path), 0700); err == nil {
			err = os.Rename(filepath.Join(stage, "next", name), path)
		}
		if err != nil {
			return errors.Join(err, recoverPublish(root))
		}
	}
	if err := syncBundleDirectories(root, names); err != nil {
		return errors.Join(err, recoverPublish(root))
	}
	if err := finishJournal(root, j); err != nil {
		return errors.Join(err, recoverPublish(root))
	}
	return nil
}

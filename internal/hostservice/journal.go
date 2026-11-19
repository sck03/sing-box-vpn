package hostservice

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/sck03/sing-box-vpn/internal/servicecatalog"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const JournalLimitsPath = "/etc/systemd/journald@sb-vpn.conf.d/limits.conf"

func CheckJournalOwnership(path string) error {
	if info, err := os.Lstat(filepath.Dir(path)); err == nil && !info.IsDir() {
		return errors.New("journal configuration directory is not an owned regular directory")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data, err := ReadUnit(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !bytes.HasPrefix(data, []byte("# Managed by sing-box-vpn\n")) {
		return errors.New("journal limits path is owned by another program")
	}
	return nil
}

type JournalQuery struct {
	Service string
	Since   string
	Lines   int
}

func (q JournalQuery) Args() ([]string, error) {
	if q.Service != "" && q.Service != servicecatalog.Panel && !slices.Contains(servicecatalog.Names(), q.Service) {
		return nil, errors.New("unknown service")
	}
	if !slices.Contains([]string{"", "1 hour ago", "24 hours ago", "7 days ago"}, q.Since) {
		return nil, errors.New("invalid log time filter")
	}
	if q.Lines == 0 {
		q.Lines = 120
	}
	if q.Lines < 1 || q.Lines > 1000 {
		return nil, errors.New("log limit must be 1–1000")
	}
	args := []string{"--namespace=" + servicecatalog.Journal, "--no-pager", "-o", "short-iso", "-n", fmt.Sprint(q.Lines)}
	if q.Service != "" {
		args = append(args, "-u", q.Service)
	}
	if q.Since != "" {
		args = append(args, "--since", q.Since)
	}
	return args, nil
}
func Journal(ctx context.Context, run Command, q JournalQuery) (string, error) {
	args, err := q.Args()
	if err != nil {
		return "", err
	}
	raw, err := run(ctx, "journalctl", args...)
	if len(raw) > 256<<10 {
		raw = raw[len(raw)-(256<<10):]
	}
	return string(raw), err
}
func JournalUsage(ctx context.Context, run Command) (string, error) {
	raw, err := run(ctx, "journalctl", "--namespace="+servicecatalog.Journal, "--disk-usage")
	return strings.TrimSpace(string(raw)), err
}
func ClearJournal(ctx context.Context, run Command) error {
	// Rotation and vacuum are restricted to this project's namespace.
	if _, err := run(ctx, "journalctl", "--namespace="+servicecatalog.Journal, "--rotate"); err != nil {
		return err
	}
	_, err := run(ctx, "journalctl", "--namespace="+servicecatalog.Journal, "--vacuum-size=1K")
	return err
}

const JournalLimits = "# Managed by sing-box-vpn\n[Journal]\nStorage=persistent\nSystemMaxUse=64M\nRuntimeMaxUse=16M\nSystemKeepFree=128M\nMaxRetentionSec=7day\nMaxFileSec=1day\n"

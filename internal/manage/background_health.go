package manage

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sck03/sing-box-vpn/internal/application"
)

// Background health is bounded process state, not a competing durable ledger.
// Fixed categories deliberately exclude raw command output, URLs and secrets.
type BackgroundIssue struct {
	Operation   string    `json:"operation"`
	Code        string    `json:"code"`
	Message     string    `json:"message"`
	Since       time.Time `json:"since"`
	LastFailure time.Time `json:"last_failure"`
	Failures    int       `json:"failures"`
}

type backgroundHealth struct {
	mu     sync.Mutex
	issues map[string]BackgroundIssue
	logged map[string]time.Time
}

func backgroundCause(err error) (string, string) {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout", "操作超时；检查磁盘、服务状态和凭据加载状态，后台会自动重试。"
	case errors.Is(err, application.ErrConflict):
		return "conflict", "配置版本发生变化，后台会使用当前版本重试。"
	case errors.Is(err, os.ErrPermission):
		return "permission", "文件访问被拒绝；检查数据目录和服务运行权限。"
	case errors.Is(err, os.ErrNotExist):
		return "missing_resource", "所需文件或程序不存在；检查运行组件与发布文件。"
	case strings.Contains(strings.ToLower(err.Error()), "database is locked"), strings.Contains(err.Error(), "SQLITE_BUSY"):
		return "storage_busy", "数据库写入繁忙；检查磁盘延迟及持续占用写锁的任务。"
	default:
		return "operation_failed", "后台操作失败，将自动重试；请检查对应任务详情和主机服务日志。"
	}
}

func (h *backgroundHealth) record(operation string, err error, now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err == nil {
		_, existed := h.issues[operation]
		delete(h.issues, operation)
		delete(h.logged, operation)
		return existed
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if h.issues == nil {
		h.issues = map[string]BackgroundIssue{}
		h.logged = map[string]time.Time{}
	}
	previous, exists := h.issues[operation]
	if !exists && len(h.issues) >= 16 {
		return false
	}
	code, message := backgroundCause(err)
	issue := previous
	if !exists {
		issue.Since = now
	}
	issue.Operation, issue.Code, issue.Message = operation, code, message
	issue.LastFailure, issue.Failures = now, min(issue.Failures+1, 1000000)
	h.issues[operation] = issue
	if previous.Code != code || now.Sub(h.logged[operation]) >= 5*time.Minute {
		slog.Warn("background operation failed", "operation", operation, "code", code, "message", message)
		h.logged[operation] = now
	}
	return true
}

func (h *backgroundHealth) snapshot() []BackgroundIssue {
	h.mu.Lock()
	defer h.mu.Unlock()
	issues := make([]BackgroundIssue, 0, len(h.issues))
	for _, issue := range h.issues {
		issues = append(issues, issue)
	}
	slices.SortFunc(issues, func(a, b BackgroundIssue) int { return strings.Compare(a.Operation, b.Operation) })
	return issues
}

func (s *PlatformService) backgroundResult(operation string, err error) {
	if s.health.record(operation, err, time.Now()) {
		s.invalidateOverview()
	}
}

func (s *PlatformService) schedulerResult(ctx context.Context, operation string, err error) {
	s.backgroundResult(operation, err)
	if err != nil && !errors.Is(err, context.Canceled) {
		// Maintenance can report after its own deadline. Give diagnostics a small,
		// independent budget instead of losing both the operation and its audit.
		audit, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		s.backgroundResult("audit-write", s.Auth.Audit(audit, "scheduler", operation, "", "failed"))
	}
}

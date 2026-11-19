package manage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sck03/sing-box-vpn/internal/command"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
	"net/http"
	"path/filepath"
	"strconv"
	"time"
)

const panelRestartFile = "panel-restart.json"

type panelRestartReceipt struct {
	TaskID   int64        `json:"task_id"`
	Revision string       `json:"revision"`
	Previous PanelOptions `json:"previous"`
	Phase    string       `json:"phase"`
	Outcome  string       `json:"outcome"`
	Error    string       `json:"error,omitempty"`
}

func (s *PlatformService) servePanelRestart(w http.ResponseWriter, r *http.Request, a panelAccess, active PanelOptions) bool {
	if r.URL.Path != "/api/panel/restart" {
		return false
	}
	if !a.require(a.admin) {
		return true
	}
	if r.Method != "POST" {
		http.NotFound(w, r)
		return true
	}
	if s.NoSystem {
		http.Error(w, "面板重启需要 Linux systemd", 400)
		return true
	}
	var v struct {
		Revision string `json:"revision"`
	}
	if err := decodeRequest(w, r, &v, 1024); err != nil {
		platformError(w, err)
		return true
	}
	if !a.audit("panel-restart", "sb-panel") {
		return true
	}
	var id int64
	err := withStartupLock(r.Context(), s.Root, func() error {
		if pending, err := s.Store.PendingKind(r.Context(), "panel-restart"); err != nil {
			return err
		} else if pending {
			return errors.New("面板访问重启任务尚未结束")
		}
		saved, revision, err := s.panelSettings()
		if err != nil {
			return err
		}
		if revision != v.Revision {
			return controlstore.ErrConflict
		}
		if err = saved.Validate(); err != nil {
			return err
		}
		receipt := panelRestartReceipt{Previous: active, Revision: revision}
		data, err := json.Marshal(receipt)
		if err != nil {
			return err
		}
		id, err = s.Store.Queue(r.Context(), "panel-restart", a.actor, data, 0, nil)
		return err
	})
	if err != nil {
		platformError(w, err)
	} else {
		s.signal()
		jsonResponse(w, Object{"task_id": id})
	}
	return true
}
func (s *PlatformService) startPanelRestart(ctx context.Context, id int64, input []byte) error {
	var v panelRestartReceipt
	if err := json.Unmarshal(input, &v); err != nil {
		return err
	}
	_, revision, err := s.panelSettings()
	if err != nil {
		return err
	}
	if revision != v.Revision {
		return controlstore.ErrConflict
	}
	v.TaskID = id
	v.Phase = "awaiting-panel-health"
	v.Outcome = "pending"
	if err = AtomicJSON(filepath.Join(s.Root, panelRestartFile), v); err != nil {
		return err
	}
	raw, _ := json.Marshal(Object{"phase": v.Phase, "outcome": v.Outcome})
	if err = s.Store.Result(ctx, id, raw); err != nil {
		return err
	}
	if err = s.Store.Phase(ctx, id, v.Phase); err != nil {
		return err
	}
	_, err = command.Output(ctx, nil, "systemd-run", "--unit=sb-panel-restart", "--collect", "--property=Type=exec", "--property=LogNamespace=sb-vpn", "/usr/local/bin/yg-vpngate", "panel", "--root", s.Root, "restart-settings", fmt.Sprint(id))
	if err != nil {
		return err
	}
	return errTaskDeferred
}
func (s *PlatformService) restartPanelSettings(ctx context.Context, id int64) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var v panelRestartReceipt
	ok, err := ReadJSON(filepath.Join(s.Root, panelRestartFile), &v)
	if err != nil {
		return err
	}
	if !ok || id <= 0 || v.TaskID != id || v.Outcome != "pending" {
		return errors.New("invalid panel restart receipt")
	}
	return withServiceLock(ctx, s.Root, func() error {
		// Let the API response leave the old listener before systemd stops it.
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
		_, revision, err := s.panelSettings()
		if err != nil {
			return err
		}
		if revision != v.Revision {
			return controlstore.ErrConflict
		}
		_, err = command.Output(ctx, nil, "systemctl", "restart", "sb-panel")
		if err == nil {
			err = s.waitPanel(ctx)
		}
		v.Phase = "succeeded"
		v.Outcome = "passed"
		if err != nil {
			v.Error = "新面板未通过健康检查，已恢复原访问配置"
			v.Outcome = "failed"
			v.Phase = "rolled-back"
			rollback, stop := context.WithTimeout(context.Background(), time.Minute)
			defer stop()
			recovery := withStartupLock(rollback, s.Root, func() error {
				_, current, e := s.panelSettings()
				if e != nil {
					return e
				}
				if current != v.Revision {
					return controlstore.ErrConflict
				}
				return AtomicJSON(filepath.Join(s.Root, "panel.json"), v.Previous)
			})
			if recovery == nil {
				_, recovery = command.Output(rollback, nil, "systemctl", "restart", "sb-panel")
			}
			if recovery == nil {
				recovery = s.waitPanel(rollback)
			}
			if recovery != nil {
				v.Phase = "rollback-failed"
				v.Error = "面板重启与回滚失败，请检查 sb-panel 日志"
			}
		}
		if e := AtomicJSON(filepath.Join(s.Root, panelRestartFile), v); e != nil {
			return e
		}
		return s.reconcilePanelRestart(context.WithoutCancel(ctx))
	})
}
func (s *PlatformService) reconcilePanelRestart(ctx context.Context) error {
	var v panelRestartReceipt
	ok, err := ReadJSON(filepath.Join(s.Root, panelRestartFile), &v)
	if err != nil || !ok || v.Outcome == "pending" {
		return err
	}
	if v.TaskID < 1 || len(v.Revision) != 64 || (v.Phase != "succeeded" && v.Phase != "rolled-back" && v.Phase != "rollback-failed") {
		return errors.New("invalid panel restart result")
	}
	// Do not retain listener key paths and previous settings in public task detail.
	raw, _ := json.Marshal(Object{"phase": v.Phase, "outcome": v.Outcome})
	changed, err := s.Store.CompletePanelRestart(ctx, v.TaskID, raw, v.Phase, v.Error)
	if err == nil && changed {
		s.invalidateOverview()
		_ = s.Auth.Audit(ctx, "restart-helper", "panel-restart", strconv.FormatInt(v.TaskID, 10), v.Outcome)
	}
	return err
}

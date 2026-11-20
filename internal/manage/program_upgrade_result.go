package manage

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/sck03/sing-box-vpn/internal/certificates"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"time"
)

const upgradeReceiptFile = "program-upgrade.json"

var errTaskDeferred = errors.New("task completion is owned by the restart helper")

type programUpgradeReceipt struct {
	TaskID        int64  `json:"task_id"`
	TargetVersion string `json:"target_version"`
	ActualVersion string `json:"actual_version"`
	Phase         string `json:"phase"`
	Outcome       string `json:"outcome"`
}

// The helper writes atomically before invoking the CLI. A later panel startup
// can replay that receipt if the CLI or the restarted service was interrupted.
func (s *PlatformService) reconcileProgramUpgrade(ctx context.Context) error {
	var receipt programUpgradeReceipt
	exists, err := ReadJSON(filepath.Join(s.Root, upgradeReceiptFile), &receipt)
	if err != nil || !exists || receipt.Outcome == "pending" {
		return err
	}
	if receipt.TaskID <= 0 || !programVersion.MatchString(receipt.TargetVersion) || (receipt.Phase != "rollback-failed" && !programVersion.MatchString(receipt.ActualVersion)) {
		return errors.New("invalid program upgrade receipt")
	}
	detail := ""
	switch receipt.Phase {
	case "succeeded":
		if receipt.Outcome != "passed" || receipt.ActualVersion != receipt.TargetVersion {
			return errors.New("program upgrade version was not confirmed")
		}
	case "rolled-back":
		detail = "新面板健康检查失败，已恢复上一发行包"
	case "rollback-failed":
		detail = "升级或回滚未通过健康检查，请检查服务与系统日志"
	default:
		return errors.New("invalid program upgrade phase")
	}
	if detail != "" && receipt.Outcome != "failed" {
		return errors.New("invalid program upgrade outcome")
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	task, err := s.Store.Task(ctx, receipt.TaskID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	changed, err := s.Store.CompleteExternal(ctx, receipt.TaskID, receipt.Phase, receipt.TargetVersion, data, detail)
	if err == nil && changed {
		result := "success"
		if detail != "" {
			result = "failed"
		}
		_ = s.Auth.Audit(ctx, task.Actor, task.Kind, "program-upgrade", result)
		s.invalidateOverview()
	}
	return err
}

func (s *PlatformService) awaitProgramUpgrade(ctx context.Context, id int64, target string) error {
	receipt := programUpgradeReceipt{TaskID: id, TargetVersion: target, Phase: "awaiting-panel-health", Outcome: "pending"}
	if err := AtomicJSON(filepath.Join(s.Root, upgradeReceiptFile), receipt); err != nil {
		return err
	}
	data, err := json.Marshal(receipt)
	if err == nil {
		err = s.Store.Result(ctx, id, data)
	}
	if err == nil {
		err = s.Store.Phase(ctx, id, "awaiting-panel-health")
	}
	return err
}

const upgradeWaitBudget = 20 * time.Minute

func localPanelPeer(r *http.Request, peer string) bool {
	ip := net.ParseIP(peer)
	if ip.IsLoopback() {
		return true
	}
	// A listener bound to a specific interface receives its own health probe
	// from that interface IP. Use the socket address, never forwarded headers.
	local, ok := r.Context().Value(http.LocalAddrContextKey).(*net.TCPAddr)
	return ok && ip != nil && ip.Equal(local.IP)
}

// Probe the actual local listener with the configured Host and TLS identity;
// no public DNS, reverse proxy or administrator credentials are needed.
func (s *PlatformService) waitPanel(ctx context.Context) error {
	options, _, err := s.panelSettings()
	if err != nil {
		return err
	}
	host, port, err := net.SplitHostPort(options.Listen)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		if ip.To4() != nil {
			host = "127.0.0.1"
		} else {
			host = "::1"
		}
	}
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	endpoint := "http://" + net.JoinHostPort(host, port)
	if options.Certificate != "" {
		origin, err := url.Parse(options.origin())
		if err != nil {
			return err
		}
		material, err := certificates.Read(CertificateAsset{Domain: origin.Hostname(), Certificate: options.Certificate, Key: options.Key})
		if err != nil {
			return err
		}
		roots := x509.NewCertPool()
		roots.AppendCertsFromPEM(material.Certificate)
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: origin.Hostname(), RootCAs: roots}
		endpoint = "https://" + net.JoinHostPort(host, port)
	}
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	ctx, stop := context.WithTimeout(ctx, 30*time.Second)
	defer stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		request, err := http.NewRequestWithContext(ctx, "GET", endpoint+"/healthz", nil)
		if err != nil {
			return err
		}
		origin, err := url.Parse(options.origin())
		if err != nil {
			return err
		}
		request.Host = origin.Host
		response, err := client.Do(request)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return errors.New("面板监听未通过健康检查")
		case <-ticker.C:
		}
	}
}

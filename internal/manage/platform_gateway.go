package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"slices"
	"time"
)

func (s *PlatformService) gateway(ctx context.Context, resource string, payload []byte) (json.RawMessage, error) {
	if resource != "status" && resource != "nodes" && resource != "actions" {
		return nil, errors.New("unknown gateway resource")
	}
	if resource == "actions" {
		var v struct {
			Op string `json:"op"`
		}
		if err := json.Unmarshal(payload, &v); err != nil {
			return nil, err
		}
		if !slices.Contains([]string{"start", "stop", "swap", "refresh"}, v.Op) {
			return nil, errors.New("edit persistent gateway policy in configuration")
		}
	}
	c, _, err := s.appliedConfiguration(ctx)
	if err != nil {
		return nil, err
	}
	run := text(c.Gateway["run_dir"])
	if run == "" {
		run = "/run/yg-vpngate"
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", filepath.Join(run, "control.sock"))
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 90 * time.Second}
	method := "GET"
	if resource == "actions" {
		method = "POST"
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://gateway/v1/"+resource, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return nil, errors.New("VPNGate control unavailable; inspect service status")
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("gateway operation failed (HTTP %d)", res.StatusCode)
	}
	return data, nil
}

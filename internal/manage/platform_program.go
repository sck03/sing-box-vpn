package manage

import (
	"context"
	"strings"
	"time"

	project "github.com/sck03/sing-box-vpn"
	"github.com/sck03/sing-box-vpn/internal/command"
	"github.com/sck03/sing-box-vpn/internal/deployment"
)

type ProgramComponent struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Version    string `json:"version"`
	Expected   string `json:"expected"`
	Compatible bool   `json:"compatible"`
	Error      string `json:"error,omitempty"`
}
type ProgramStatus struct {
	Current         string             `json:"current"`
	Components      []ProgramComponent `json:"components"`
	Latest          string             `json:"latest,omitempty"`
	ReleaseURL      string             `json:"release_url,omitempty"`
	UpdateAvailable bool               `json:"update_available"`
	CheckError      string             `json:"check_error,omitempty"`
}

func (s *PlatformService) ProgramStatus(ctx context.Context, check bool) (ProgramStatus, error) {
	v := project.Versions
	result := ProgramStatus{Current: v.Release, Components: []ProgramComponent{
		{ID: "manager", Name: "面板与管理程序", Version: v.Release, Expected: v.Release, Compatible: true},
		{ID: "certificate", Name: "证书签发组件（lego）", Version: v.CertificateManager, Expected: v.CertificateManager, Compatible: true},
	}}
	probe, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	for _, target := range []struct{ id, name, path, expected string }{
		{"runtime", "受管 sing-box 核心", s.Runtime, v.ManagedRuntime},
		{"validator", "官方配置校验器", s.Core, v.SingBox.Tested},
	} {
		component := ProgramComponent{ID: target.id, Name: target.name, Expected: target.expected}
		var err error
		if target.id == "validator" {
			component.Version, err = CheckCore(probe, target.path)
		} else {
			var output []byte
			output, err = command.Output(probe, nil, target.path, "--version")
			component.Version = strings.TrimSpace(string(output))
		}
		component.Compatible = err == nil && component.Version == component.Expected
		if err != nil {
			component.Error = "无法读取组件版本；请检查安装文件"
			component.Version = ""
		}
		result.Components = append(result.Components, component)
	}
	if !check {
		return result, nil
	}
	endpoint, _ := deployment.Endpoint("")
	client := newHTTPClient(20 * time.Second)
	defer client.CloseIdleConnections()
	var release deployment.Release
	if err := getJSON(ctx, client, endpoint, &release); err != nil {
		result.CheckError = err.Error()
		return result, nil
	}
	if _, err := deployment.Bundle(release); err != nil {
		result.CheckError = err.Error()
		return result, nil
	}
	result.Latest, result.ReleaseURL = release.Tag, deployment.ReleaseURL(release.Tag)
	result.UpdateAvailable = deployment.Newer(release.Tag, v.Release)
	return result, nil
}

package manage

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// The platform owns configuration in control.db. This helper creates the initial
// model; publication is a separate, recoverable operation.
func initialApp(ctx context.Context, root, core, address, name string) (App, error) {
	if _, err := os.Lstat(filepath.Join(root, "sb.json")); err == nil {
		return App{}, errors.New("配置目录已有 sb.json；请恢复当前配置或使用空目录安装，不支持旧版迁移")
	} else if !errors.Is(err, os.ErrNotExist) {
		return App{}, err
	}
	if address == "" {
		var err error
		address, err = publicIP(ctx)
		if err != nil {
			return App{}, err
		}
	}
	if net.ParseIP(address) == nil {
		if err := validateDomain(address); err != nil {
			return App{}, fmt.Errorf("服务器地址无效: %w", err)
		}
	}
	version, err := CheckCore(ctx, core)
	if err != nil {
		return App{}, err
	}
	settings, _, err := loadVPN(root)
	if err != nil {
		return App{}, err
	}
	app := App{Name: name, Address: address, Version: version}
	used, ranges, err := allocationPorts(root, app, settings)
	if err != nil {
		return App{}, err
	}
	profile, err := NewProfile(root, used, "", ranges...)
	if err != nil {
		return App{}, err
	}
	app.Nodes = MakeNodes(profile, "")
	return app, nil
}

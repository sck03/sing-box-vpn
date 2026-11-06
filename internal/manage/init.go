package manage

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// app.json is the sole configuration source. Do not infer managed state from
// generated server files or silently replace an unrelated installation.
func initializeApp(ctx context.Context, root, core, address, name string) error {
	if _, err := os.Lstat(filepath.Join(root, "sb.json")); err == nil {
		return errors.New("配置目录已有 sb.json，但缺少 app.json；请恢复当前配置或使用空目录安装，不支持旧版迁移")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if address == "" {
		var err error
		address, err = publicIP(ctx)
		if err != nil {
			return err
		}
	}
	if net.ParseIP(address) == nil {
		if err := validateDomain(address); err != nil {
			return fmt.Errorf("服务器地址无效: %w", err)
		}
	}
	if _, err := CheckCore(ctx, core); err != nil {
		return err
	}
	settings, _, err := loadVPN(root)
	if err != nil {
		return err
	}
	app := App{Name: name, Address: address, SocksPort: 40000, Routing: map[string][]string{}}
	used, ranges, err := allocationPorts(root, app, settings)
	if err != nil {
		return err
	}
	app.Profile, err = NewProfile(root, used, "", ranges...)
	if err != nil {
		return err
	}
	app.Inbounds = MakeInbounds(app.Profile, "")
	return CommitApp(ctx, root, core, &app)
}

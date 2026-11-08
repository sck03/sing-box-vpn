package manage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxRemoteExport = 16 << 20
const maxTelegramMessage = 3500 // Conservative byte budget, including separators.

type remoteExport struct{ path, content string }

// Take a coherent snapshot under the publication lock, then release it before
// making any remote request. Startup recovery also completes interrupted writes.
func readRemoteExports(ctx context.Context, root, scope string, names ...string) ([]remoteExport, error) {
	directory, err := exportDirectory(root, scope)
	if err != nil {
		return nil, err
	}
	prefix := ""
	if scope == vpngateScope {
		prefix = "vpngate/"
	}
	files := make([]remoteExport, 0, len(names))
	err = withStartupLock(ctx, root, func() error {
		for _, name := range names {
			if err := ctx.Err(); err != nil {
				return err
			}
			content, err := readRemoteExport(filepath.Join(directory, name))
			if err != nil {
				return err
			}
			files = append(files, remoteExport{prefix + name, content})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func readRemoteExport(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > maxRemoteExport {
		return "", errors.New("订阅必须是最多 16 MiB 的普通文件")
	}
	var content strings.Builder
	content.Grow(int(info.Size()))
	size, err := copyFileContent(&content, io.LimitReader(file, maxRemoteExport+1))
	if err != nil {
		return "", err
	}
	if size > maxRemoteExport {
		return "", errors.New("订阅文件超过 16 MiB")
	}
	return content.String(), nil
}

// Validate the entire list before sending anything. Keep links intact and omit
// blank lines and trailing separators so an exact-limit link is one message.
func telegramMessages(content string) ([]string, error) {
	var messages []string
	var message strings.Builder
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > maxTelegramMessage {
			return nil, errors.New("单个节点链接超过 Telegram 消息长度，请使用本地订阅")
		}
		if message.Len() > 0 {
			if message.Len()+1+len(line) > maxTelegramMessage {
				messages = append(messages, message.String())
				message.Reset()
			} else {
				message.WriteByte('\n')
			}
		}
		message.WriteString(line)
	}
	if message.Len() > 0 {
		messages = append(messages, message.String())
	}
	if len(messages) == 0 {
		return nil, errors.New("节点订阅为空，请先生成订阅")
	}
	return messages, nil
}

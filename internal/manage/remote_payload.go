package manage

import (
	"errors"
	"strings"
)

const maxTelegramMessage = 3500

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

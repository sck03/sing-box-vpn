package manage

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

type remoteSettings struct {
	Telegram *struct {
		Token  string `json:"token"`
		ChatID string `json:"chat_id"`
	} `json:"telegram,omitempty"`
	GitLab *struct {
		Project string `json:"project"`
		Branch  string `json:"branch"`
		Token   string `json:"token"`
	} `json:"gitlab,omitempty"`
}

func pushTelegram(ctx context.Context, client *http.Client, baseURL, root, scope string, settings remoteSettings) error {
	if settings.Telegram == nil {
		return errors.New("先配置 Telegram")
	}
	files, err := readRemoteExports(ctx, root, scope, "jhsub.txt")
	if err != nil {
		return err
	}
	chunks, err := telegramMessages(files[0].content)
	if err != nil {
		return err
	}
	for _, value := range chunks {
		var response struct {
			OK bool `json:"ok"`
		}
		if err := remoteRequest(ctx, client, baseURL+"/bot"+settings.Telegram.Token+"/sendMessage", "POST", Object{"chat_id": settings.Telegram.ChatID, "text": value, "disable_web_page_preview": true}, nil, &response); err != nil {
			return err
		}
		if !response.OK {
			return errors.New("Telegram 拒绝了推送请求")
		}
	}
	return nil
}

func pushGitLab(ctx context.Context, client *http.Client, baseURL, root, scope string, settings remoteSettings) error {
	if settings.GitLab == nil {
		return errors.New("先配置 GitLab")
	}
	files, err := readRemoteExports(ctx, root, scope, "sbox.json", "clmi.yaml", "jhsub.txt")
	if err != nil {
		return err
	}
	cfg := settings.GitLab
	base := baseURL + "/api/v4/projects/" + url.PathEscape(cfg.Project)
	headers := map[string]string{"PRIVATE-TOKEN": cfg.Token}
	var meta struct {
		Visibility string `json:"visibility"`
	}
	if err := remoteRequest(ctx, client, base, "GET", nil, headers, &meta); err != nil {
		return err
	}
	if meta.Visibility != "private" {
		return errors.New("节点订阅只允许写入私有 GitLab 项目")
	}
	actions := make([]Object, 0, len(files))
	for _, file := range files {
		action := "update"
		err = remoteRequest(ctx, client, base+"/repository/files/"+url.PathEscape(file.path)+"?ref="+url.QueryEscape(cfg.Branch), http.MethodHead, nil, headers, nil)
		if err != nil {
			var status httpStatusError
			if !errors.As(err, &status) || status != 404 {
				return err
			}
			action = "create"
		}
		actions = append(actions, Object{"action": action, "file_path": file.path, "content": file.content})
	}
	return remoteRequest(ctx, client, base+"/repository/commits", "POST", Object{"branch": cfg.Branch, "commit_message": "更新 sing-box-vpn 订阅", "actions": actions}, headers, nil)
}

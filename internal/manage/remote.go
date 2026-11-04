package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
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
type httpStatusError int

func (e httpStatusError) Error() string { return fmt.Sprintf("HTTP response %d", int(e)) }

func remoteRequest(ctx context.Context, client *http.Client, endpoint, method string, body any, headers map[string]string, target any) error {
	var input io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		input = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, input)
	if err != nil {
		return errors.New("远端请求地址无效")
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response, err := client.Do(req)
	if err != nil {
		return errors.New("远端请求失败，检查配置与网络；凭据未输出")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return httpStatusError(response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil {
		return errors.New("远端响应读取失败")
	}
	if len(data) > 4<<20 {
		return errors.New("远端响应过大")
	}
	if target == nil {
		return nil
	}
	return json.Unmarshal(data, target)
}

func pushTelegram(ctx context.Context, client *http.Client, baseURL, root string, settings remoteSettings) error {
	if settings.Telegram == nil {
		return errors.New("先配置 Telegram")
	}
	data, err := os.ReadFile(filepath.Join(root, "jhsub.txt"))
	if err != nil {
		return err
	}
	var chunks []string
	chunk := ""
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if len(line) > 3500 {
			return errors.New("单个节点链接超过 Telegram 消息长度，请使用本地订阅")
		}
		if len(chunk)+len(line)+1 > 3500 {
			chunks = append(chunks, chunk)
			chunk = ""
		}
		chunk += line + "\n"
	}
	if chunk != "" {
		chunks = append(chunks, chunk)
	}
	for _, value := range chunks {
		var response Object
		if err := remoteRequest(ctx, client, baseURL+"/bot"+settings.Telegram.Token+"/sendMessage", "POST", Object{"chat_id": settings.Telegram.ChatID, "text": value, "disable_web_page_preview": true}, nil, &response); err != nil {
			return err
		}
		if response["ok"] != true {
			return errors.New("Telegram 拒绝了推送请求")
		}
	}
	return nil
}

func pushGitLab(ctx context.Context, client *http.Client, baseURL, root, scope string, settings remoteSettings) error {
	if settings.GitLab == nil {
		return errors.New("先配置 GitLab")
	}
	directory, err := exportDirectory(root, scope)
	if err != nil {
		return err
	}
	prefix := ""
	if scope == vpngateScope {
		prefix = "vpngate/"
	}
	cfg := settings.GitLab
	base := baseURL + "/api/v4/projects/" + url.PathEscape(cfg.Project)
	headers := map[string]string{"PRIVATE-TOKEN": cfg.Token}
	var meta Object
	if err := remoteRequest(ctx, client, base, "GET", nil, headers, &meta); err != nil {
		return err
	}
	if meta["visibility"] != "private" {
		return errors.New("节点订阅只允许写入私有 GitLab 项目")
	}
	actions := []Object{}
	for _, name := range []string{"sbox.json", "clmi.yaml", "jhsub.txt"} {
		content, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			return err
		}
		name = prefix + name
		action := "update"
		var existing Object
		err = remoteRequest(ctx, client, base+"/repository/files/"+url.PathEscape(name)+"?ref="+url.QueryEscape(cfg.Branch), "GET", nil, headers, &existing)
		if err != nil {
			var status httpStatusError
			if !errors.As(err, &status) || status != 404 {
				return err
			}
			action = "create"
		}
		actions = append(actions, Object{"action": action, "file_path": name, "content": string(content)})
	}
	return remoteRequest(ctx, client, base+"/repository/commits", "POST", Object{"branch": cfg.Branch, "commit_message": "更新 sing-box-vpn 订阅", "actions": actions}, headers, nil)
}

func RunRemote(ctx context.Context, args []string, input io.Reader, output io.Writer) error {
	f := flag.NewFlagSet("remote", flag.ContinueOnError)
	f.SetOutput(output)
	root := f.String("root", "/etc/s-box", "configuration directory")
	if err := f.Parse(args); err != nil {
		return err
	}
	args = f.Args()
	if len(args) == 0 {
		return errors.New("缺少远端操作")
	}
	operation := args[0]
	options := flag.NewFlagSet(operation, flag.ContinueOnError)
	options.SetOutput(output)
	chatID := options.String("chat-id", "", "Telegram chat")
	project := options.String("project", "", "private GitLab project")
	branch := options.String("branch", "main", "GitLab branch")
	scope := options.String("scope", normalScope, "normal or vpngate nodes")
	if err := parseOptions(options, args[1:]); err != nil {
		return err
	}
	directory, err := exportDirectory(*root, *scope)
	if err != nil {
		return err
	}
	if operation == "telegram-config" || operation == "gitlab-config" {
		data, err := io.ReadAll(io.LimitReader(input, 4097))
		if err != nil {
			return err
		}
		token := strings.TrimSpace(string(data))
		if len(token) == 0 || len(data) > 4096 {
			return errors.New("访问令牌为空或过长")
		}
		return withLock(*root, func() error {
			var settings remoteSettings
			path := filepath.Join(*root, "remote.json")
			if _, err := ReadJSON(path, &settings); err != nil {
				return err
			}
			if operation == "telegram-config" {
				if *chatID == "" {
					return errors.New("缺少 chat_id")
				}
				settings.Telegram = &struct {
					Token  string `json:"token"`
					ChatID string `json:"chat_id"`
				}{token, *chatID}
			} else {
				if *project == "" || *branch == "" {
					return errors.New("缺少项目或分支")
				}
				settings.GitLab = &struct {
					Project string `json:"project"`
					Branch  string `json:"branch"`
					Token   string `json:"token"`
				}{*project, *branch, token}
			}
			return AtomicJSON(path, settings)
		})
	}
	var settings remoteSettings
	if _, err := ReadJSON(filepath.Join(*root, "remote.json"), &settings); err != nil {
		return err
	}
	client := newHTTPClient(20 * time.Second)
	defer client.CloseIdleConnections()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	switch operation {
	case "telegram-push":
		if err := pushTelegram(ctx, client, "https://api.telegram.org", directory, settings); err != nil {
			return err
		}
		_, err := fmt.Fprintln(output, "节点已发送到配置的 Telegram 会话")
		return err
	case "gitlab-push":
		if err := pushGitLab(ctx, client, "https://gitlab.com", *root, *scope, settings); err != nil {
			return err
		}
		_, err := fmt.Fprintln(output, "私有订阅已更新")
		return err
	}
	return errors.New("未知远端操作")
}

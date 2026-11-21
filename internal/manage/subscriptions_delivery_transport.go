package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
	"io"
	"net/http"
	"net/url"
)

var errDeliveryUncertain = errors.New("remote acceptance is unknown")

func sendSubscriptionArtifact(ctx context.Context, client *http.Client, d sub.Delivery, artifact sub.Artifact) error {
	if d.Kind == "telegram" {
		chunks, err := telegramMessages(string(artifact.Data))
		if err != nil {
			return err
		}
		for i, chunk := range chunks {
			var response struct {
				OK bool `json:"ok"`
			}
			if err = remoteRequest(ctx, client, d.URL+"/bot"+d.Token+"/sendMessage", "POST", Object{"chat_id": d.Username, "text": chunk, "disable_web_page_preview": true}, nil, &response); err != nil {
				return fmt.Errorf("Telegram delivery failed: %w", errDeliveryUncertain)
			}
			if !response.OK {
				if i > 0 {
					return fmt.Errorf("Telegram partially delivered: %w", errDeliveryUncertain)
				}
				return errors.New("Telegram rejected delivery")
			}
		}
		return nil
	}
	if d.Kind == "gitlab" {
		headers := map[string]string{"PRIVATE-TOKEN": d.Token}
		var project struct {
			Visibility string `json:"visibility"`
		}
		if err := remoteRequest(ctx, client, d.URL, "GET", nil, headers, &project); err != nil {
			return errors.New("GitLab project check failed")
		}
		if project.Visibility != "private" {
			return errors.New("GitLab delivery requires a private project")
		}
		branch := d.Username
		if branch == "" {
			branch = "main"
		}
		action := "update"
		err := remoteRequest(ctx, client, d.URL+"/repository/files/"+url.PathEscape(d.Filename)+"?ref="+url.QueryEscape(branch), "HEAD", nil, headers, nil)
		if err != nil {
			var status httpStatusError
			if !errors.As(err, &status) || status != 404 {
				return errors.New("GitLab file check failed")
			}
			action = "create"
		}
		if err = remoteRequest(ctx, client, d.URL+"/repository/commits", "POST", Object{"branch": branch, "commit_message": "Update subscription", "actions": []Object{{"action": action, "file_path": d.Filename, "content": string(artifact.Data)}}}, headers, nil); err != nil {
			return fmt.Errorf("GitLab delivery failed: %w", errDeliveryUncertain)
		}
		return nil
	}
	method, data := http.MethodPut, artifact.Data
	if d.Kind == "github-gist" {
		// Subscription credentials must never be uploaded to a public Gist.
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.URL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+d.Token)
		response, err := client.Do(req)
		if err != nil {
			return errors.New("Gist visibility check failed")
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, sub.MaxBody+1))
		response.Body.Close()
		var gist struct {
			Public *bool `json:"public"`
		}
		if readErr != nil || response.StatusCode != 200 || len(body) > sub.MaxBody || json.Unmarshal(body, &gist) != nil || gist.Public == nil || *gist.Public {
			return errors.New("delivery requires an existing secret Gist")
		}
		method = http.MethodPatch
		data, err = json.Marshal(Object{"files": Object{d.Filename: Object{"content": string(artifact.Data)}}})
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, d.URL, bytes.NewReader(data))
	if err != nil {
		return errors.New("invalid delivery request")
	}
	req.Header.Set("Content-Type", artifact.MIME)
	if d.Kind == "github-gist" {
		req.Header.Set("Content-Type", "application/json")
	}
	if d.Username != "" {
		req.SetBasicAuth(d.Username, d.Password)
	}
	if d.Token != "" {
		req.Header.Set("Authorization", "Bearer "+d.Token)
	}
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("delivery failed or timed out: %w", errDeliveryUncertain)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("delivery HTTP %d", response.StatusCode)
	}
	n, err := io.Copy(io.Discard, io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || n > 4<<20 {
		return fmt.Errorf("invalid delivery acknowledgement: %w", errDeliveryUncertain)
	}
	return nil
}

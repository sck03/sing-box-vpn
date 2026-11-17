package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

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
	input = io.LimitReader(response.Body, (4<<20)+1)
	var data []byte
	var size int64
	if target == nil {
		size, err = copyFileContent(io.Discard, input)
	} else {
		data, err = io.ReadAll(input)
		size = int64(len(data))
	}
	if err != nil {
		return errors.New("远端响应读取失败")
	}
	if size > 4<<20 {
		return errors.New("远端响应过大")
	}
	if target == nil {
		return nil
	}
	return json.Unmarshal(data, target)
}

package manage

import (
	"errors"
	"io"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// One line contains the UUID/password, with a second line for TUIC or all.
// Menus use stdin so credentials do not appear in child-process arguments.
func readCredentials(input io.Reader) ([]string, error) {
	data, err := io.ReadAll(io.LimitReader(input, 2050))
	if err != nil {
		return nil, err
	}
	if len(data) == 2050 {
		return nil, errors.New("凭据输入过长")
	}
	values := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(values) > 2 {
		return nil, errors.New("凭据最多两行：UUID / 密码，以及可选的 TUIC 或统一密码")
	}
	for i := range values {
		values[i] = strings.TrimSuffix(values[i], "\r")
	}
	return values, nil
}

func changeCredentials(app *App, operation string, values []string) error {
	if len(values) == 0 || values[0] != "all" && !slices.Contains(Protocols, values[0]) {
		return errors.New("请选择有效协议或 all")
	}
	custom := operation == "credentials"
	kind := values[0]
	if !custom && len(values) != 1 || custom && (len(values) < 2 || len(values) > 3 || len(values) == 3 && kind != "tuic" && kind != "all") {
		return errors.New("credentials 需要协议和 UUID / 密码，TUIC 或 all 可再指定密码；reset 只需要协议")
	}
	value, password := "", ""
	if custom {
		value = values[1]
		// Empty TUIC fields keep their current values; randomization is explicit.
		if (kind != "tuic" || value != "") && slices.Contains([]string{"all", "vless", "vmess", "tuic"}, kind) {
			if !uuidPattern.MatchString(value) {
				return errors.New("UUID 格式错误，请使用 xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx")
			}
			value = strings.ToLower(value)
		}
		password = value
		if kind == "tuic" {
			password = ""
		}
		if len(values) == 3 {
			password = values[2]
		}
		if value == "" && password == "" {
			return errors.New("请填写要修改的 UUID 或密码；随机生成请使用 reset")
		}
		if password == "" && kind != "tuic" || len(password) > 1024 || !utf8.ValidString(password) || strings.ContainsAny(password, "\x00\r\n") {
			return errors.New("密码须为 1-1024 字节的有效文本，不能包含空字符或换行")
		}
	}
	matched := false
	for i := range app.Nodes {
		n := &app.Nodes[i]
		if kind != "all" && n.Protocol != kind {
			continue
		}
		if n.UUID != "" && (!custom || value != "") {
			n.UUID = value
			if !custom {
				n.UUID = randomUUID()
			}
		}
		if n.Password != "" && (!custom || password != "") {
			n.Password = password
			if !custom {
				n.Password = randomSecret()
			}
		}
		matched = true
	}
	if !matched {
		return errors.New("未找到所选协议入口")
	}
	return nil
}

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

// One line contains the UUID/password, with an optional second line for TUIC.
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
		return nil, errors.New("凭据最多两行：UUID / 密码，以及可选的 TUIC 密码")
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
	if !custom && len(values) != 1 || custom && (len(values) < 2 || len(values) > 3 || len(values) == 3 && values[0] != "tuic") {
		return errors.New("credentials 需要协议和 UUID / 密码，TUIC 可再指定独立密码；reset 只需要协议")
	}
	value, password := "", ""
	if custom {
		value = values[1]
		if slices.Contains([]string{"all", "vless", "vmess", "tuic"}, values[0]) {
			if !uuidPattern.MatchString(value) {
				return errors.New("UUID 格式错误，请使用 xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx")
			}
			value = strings.ToLower(value)
		}
		password = value
		if len(values) == 3 {
			password = values[2]
		}
		if len(password) == 0 || len(password) > 1024 || !utf8.ValidString(password) || strings.ContainsAny(password, "\x00\r\n") {
			return errors.New("密码须为 1-1024 字节的有效文本，不能包含空字符或换行")
		}
	}
	matched := false
	for _, in := range app.Inbounds {
		if values[0] != "all" && in["type"] != values[0] {
			continue
		}
		users := objects(in["users"])
		if len(users) == 0 {
			return errors.New("入口缺少用户凭据")
		}
		// Imported additional users keep their own identities when customizing
		// the exported user; random reset retains the existing all-user behavior.
		if custom {
			users = users[:1]
		}
		for _, user := range users {
			if _, ok := user["uuid"]; ok {
				user["uuid"] = value
				if !custom {
					user["uuid"] = randomUUID()
				}
			}
			if _, ok := user["password"]; ok {
				user["password"] = password
				if !custom {
					user["password"] = randomSecret()
				}
			}
		}
		matched = true
	}
	if !matched {
		return errors.New("未找到所选协议入口")
	}
	syncProfileSecrets(&app.Profile, app.Inbounds)
	return nil
}

func syncProfileSecrets(profile *Profile, inbounds []Object) {
	if profile.Secrets.UUID == nil {
		profile.Secrets.UUID = map[string]string{}
	}
	if profile.Secrets.Passwords == nil {
		profile.Secrets.Passwords = map[string]string{}
	}
	for _, in := range inbounds {
		users := objects(in["users"])
		if len(users) == 0 {
			continue
		}
		kind := text(in["type"])
		if value, ok := users[0]["uuid"]; ok {
			profile.Secrets.UUID[kind] = text(value)
		}
		if value, ok := users[0]["password"]; ok {
			profile.Secrets.Passwords[kind] = text(value)
		}
	}
}

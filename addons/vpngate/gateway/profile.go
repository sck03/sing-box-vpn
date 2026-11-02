// Profile validation adapts ConduitVPN (GPL-3.0); country filtering adapts
// fanout (MIT). See the repository's NOTICE.md and LICENSE.
package main

import (
	"bufio"
	"encoding/base64"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Node struct {
	Host    string `json:"host"`
	IP      string `json:"ip"`
	Country string `json:"country"`
	Port    int    `json:"port"`
	Network string `json:"network"`
	Speed   int64  `json:"speed"`
	Ping    int    `json:"ping"`
	Profile string `json:"profile"`
}

func publicIPv4(s string) bool {
	ip := net.ParseIP(s)
	if ip == nil || ip.To4() == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	v := ip.To4()
	return !(v[0] == 100 && v[1] >= 64 && v[1] <= 127)
}

func volunteer(n Node) bool {
	if !publicIPv4(n.IP) || strings.HasPrefix(strings.ToLower(n.Host), "public-vpn-") {
		return false
	}
	_, own, _ := net.ParseCIDR("219.100.37.0/24")
	return !own.Contains(net.ParseIP(n.IP))
}

var countryPattern = regexp.MustCompile(`^[A-Z]{2}$`)
var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9_.:/@+=,-]+$`)
var hostPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

func validateProfile(profile, expectedIP string) (string, int, string, error) {
	if len(profile) == 0 || len(profile) > 1<<20 || !publicIPv4(expectedIP) {
		return "", 0, "", errors.New("invalid profile or node IPv4")
	}
	scanner := bufio.NewScanner(strings.NewReader(strings.ReplaceAll(profile, "\r\n", "\n")))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var lines []string
	block, proto, remoteProto := "", "udp", ""
	port, remotes := 0, 0
	hasCA := false
	noarg := map[string]bool{"client": true, "nobind": true, "persist-key": true, "persist-tun": true, "auth-nocache": true, "pull": true, "fast-io": true, "auth-user-pass": true}
	for scanner.Scan() {
		raw := scanner.Text()
		line := strings.TrimSpace(raw)
		if block != "" {
			lines = append(lines, raw)
			if line == "</"+block+">" {
				block = ""
			}
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "<") {
			name := strings.TrimSuffix(strings.TrimPrefix(line, "<"), ">")
			if !map[string]bool{"ca": true, "cert": true, "key": true, "tls-auth": true, "tls-crypt": true}[name] || line != "<"+name+">" {
				return "", 0, "", errors.New("unsupported inline block")
			}
			if name == "ca" {
				hasCA = true
			}
			block = name
			lines = append(lines, line)
			continue
		}
		f := strings.Fields(line)
		name, args := f[0], f[1:]
		valid := false
		if noarg[name] {
			valid = len(args) == 0
		} else {
			switch name {
			case "dev", "dev-type":
				valid = len(args) == 1 && args[0] == "tun"
			case "proto":
				valid = len(args) == 1 && validProto(args[0])
				if valid {
					proto = normalizeProto(args[0])
				}
			case "remote":
				remotes++
				valid = len(args) >= 2 && len(args) <= 3 && args[0] == expectedIP && publicIPv4(args[0]) && remotes == 1
				if valid {
					port, _ = strconv.Atoi(args[1])
					valid = port > 0 && port <= 65535
				}
				if valid && len(args) == 3 {
					valid = validProto(args[2])
					remoteProto = normalizeProto(args[2])
				}
			case "resolv-retry":
				valid = len(args) == 1 && (args[0] == "infinite" || positiveInt(args[0]))
			case "remote-cert-tls":
				valid = len(args) == 1 && args[0] == "server"
			case "cipher", "data-ciphers", "data-ciphers-fallback", "auth", "tls-cipher", "tls-ciphersuites":
				valid = len(args) == 1 && tokenPattern.MatchString(args[0])
			case "comp-lzo":
				valid = len(args) == 0 || (len(args) == 1 && map[string]bool{"yes": true, "no": true, "adaptive": true}[args[0]])
			case "compress":
				valid = len(args) == 1 && map[string]bool{"lz4": true, "lz4-v2": true, "stub": true, "stub-v2": true, "no": true}[args[0]]
			case "key-direction":
				valid = len(args) == 1 && (args[0] == "0" || args[0] == "1")
			case "tls-version-min":
				valid = len(args) == 1 && (args[0] == "1.2" || args[0] == "1.3")
			case "verb":
				if len(args) == 1 {
					v, e := strconv.Atoi(args[0])
					valid = e == nil && v >= 0 && v <= 6
				}
			case "reneg-sec", "connect-timeout", "connect-retry", "sndbuf", "rcvbuf", "explicit-exit-notify":
				valid = len(args) == 1 && positiveInt(args[0])
			}
		}
		if !valid {
			return "", 0, "", fmt.Errorf("unsupported or invalid OpenVPN directive %q", name)
		}
		lines = append(lines, strings.Join(f, " "))
	}
	if err := scanner.Err(); err != nil {
		return "", 0, "", err
	}
	if block != "" || remotes != 1 || !hasCA {
		return "", 0, "", errors.New("profile needs a complete CA and one permitted remote")
	}
	if remoteProto != "" {
		proto = remoteProto
	}
	return strings.Join(lines, "\n") + "\n", port, proto, nil
}

func validProto(s string) bool {
	return map[string]bool{"udp": true, "udp4": true, "tcp": true, "tcp4": true, "tcp-client": true}[s]
}
func normalizeProto(s string) string {
	if strings.HasPrefix(s, "tcp") {
		return "tcp"
	}
	return "udp"
}
func positiveInt(s string) bool { v, e := strconv.Atoi(s); return e == nil && v > 0 && v < 1<<31 }
func canonicalColumn(s string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "", " ", "", "\ufeff", "").Replace(strings.TrimPrefix(s, "#")))
}

func parseNodes(data []byte) ([]Node, error) {
	if len(data) > 16<<20 {
		return nil, errors.New("VPNGate response exceeds 16 MiB")
	}
	text := strings.TrimPrefix(string(data), "\ufeff")
	lines := strings.Split(text, "\n")
	start := -1
	for i, line := range lines {
		lc := strings.ToLower(line)
		if strings.Contains(lc, "openvpn_configdata_base64") && strings.Contains(lc, "country") {
			start = i
			break
		}
	}
	if start < 0 {
		return nil, errors.New("VPNGate CSV header missing")
	}
	r := csv.NewReader(strings.NewReader(strings.Join(lines[start:], "\n")))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true
	head, err := r.Read()
	if err != nil {
		return nil, err
	}
	columns := map[string]int{}
	for i, k := range head {
		columns[canonicalColumn(k)] = i
	}
	var out []Node
	seen := map[string]bool{}
	for len(out) < 1500 {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		get := func(keys ...string) string {
			for _, k := range keys {
				if i, ok := columns[canonicalColumn(k)]; ok && i < len(row) {
					return strings.TrimSpace(row[i])
				}
			}
			return ""
		}
		ip, cc := get("IP"), strings.ToUpper(get("CountryShort", "CountryCode"))
		if !publicIPv4(ip) || !countryPattern.MatchString(cc) {
			continue
		}
		host := get("HostName", "Hostname", "DDNS_HostName")
		if host == "" {
			host = strings.ReplaceAll(ip, ".", "-")
		}
		if !hostPattern.MatchString(host) || seen[host] {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(get("OpenVPN_ConfigData_Base64"))
		if err != nil {
			continue
		}
		profile, port, proto, err := validateProfile(string(raw), ip)
		if err != nil {
			continue
		}
		speed, _ := strconv.ParseInt(get("Speed"), 10, 64)
		ping, _ := strconv.Atoi(get("Ping"))
		out = append(out, Node{Host: host, IP: ip, Country: cc, Port: port, Network: proto, Speed: speed, Ping: ping, Profile: profile})
		seen[host] = true
	}
	if len(out) == 0 {
		return nil, errors.New("no valid VPNGate profiles")
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Speed == out[j].Speed {
			return out[i].Ping < out[j].Ping
		}
		return out[i].Speed > out[j].Speed
	})
	return out, nil
}

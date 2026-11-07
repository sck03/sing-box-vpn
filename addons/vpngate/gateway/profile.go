// Profile validation adapts ConduitVPN (GPL-3.0); country filtering adapts
// fanout (MIT). See the repository's NOTICE.md and LICENSE.
package main

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

const maxProfile = 1 << 20

func publicIPv4(s string) bool {
	ip, err := netip.ParseAddr(s)
	return err == nil && publicIPv4Addr(ip.Unmap())
}

func publicIPv4Addr(ip netip.Addr) bool {
	if !ip.Is4() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	v := ip.As4()
	return !(v[0] == 100 && v[1] >= 64 && v[1] <= 127)
}

func volunteer(n Node) bool {
	if !publicIPv4(n.IP) || strings.HasPrefix(strings.ToLower(n.Host), "public-vpn-") {
		return false
	}
	return !vpngateSubnet.Contains(net.ParseIP(n.IP))
}

var _, vpngateSubnet, _ = net.ParseCIDR("219.100.37.0/24")
var countryPattern = regexp.MustCompile(`^[A-Z]{2}$`)
var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9_.:/@+=,-]+$`)
var hostPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

func validateProfile(profile, expectedIP string) (string, int, string, error) {
	if len(profile) == 0 || len(profile) > maxProfile || !publicIPv4(expectedIP) {
		return "", 0, "", errors.New("invalid profile or node IPv4")
	}
	var output strings.Builder
	// Normalization only removes bytes, apart from a possible final newline.
	output.Grow(len(profile) + 1)
	fields := make([]string, 0, 4)
	block, proto, remoteProto := "", "udp", ""
	port, remotes := 0, 0
	hasCA := false
	for raw := range strings.SplitSeq(profile, "\n") {
		raw = strings.TrimSuffix(raw, "\r")
		line := strings.TrimSpace(raw)
		if block != "" {
			output.WriteString(raw)
			output.WriteByte('\n')
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
			if line != "<"+name+">" {
				return "", 0, "", errors.New("unsupported inline block")
			}
			switch name {
			case "ca":
				hasCA = true
			case "cert", "key", "tls-auth", "tls-crypt":
			default:
				return "", 0, "", errors.New("unsupported inline block")
			}
			block = name
			output.WriteString(line)
			output.WriteByte('\n')
			continue
		}
		fields = fields[:0]
		for field := range strings.FieldsSeq(line) {
			fields = append(fields, field)
		}
		name, args := fields[0], fields[1:]
		valid := false
		switch name {
		case "client", "nobind", "persist-key", "persist-tun", "auth-nocache", "pull", "fast-io", "auth-user-pass":
			valid = len(args) == 0
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
			valid = len(args) == 0 || (len(args) == 1 && (args[0] == "yes" || args[0] == "no" || args[0] == "adaptive"))
		case "compress":
			valid = len(args) == 1 && (args[0] == "lz4" || args[0] == "lz4-v2" || args[0] == "stub" || args[0] == "stub-v2" || args[0] == "no")
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
		if !valid {
			return "", 0, "", fmt.Errorf("unsupported or invalid OpenVPN directive %q", name)
		}
		output.WriteString(name)
		for _, arg := range args {
			output.WriteByte(' ')
			output.WriteString(arg)
		}
		output.WriteByte('\n')
	}
	if block != "" || remotes != 1 || !hasCA {
		return "", 0, "", errors.New("profile needs a complete CA and one permitted remote")
	}
	if remoteProto != "" {
		proto = remoteProto
	}
	return output.String(), port, proto, nil
}

func validProto(s string) bool {
	switch s {
	case "udp", "udp4", "tcp", "tcp4", "tcp-client":
		return true
	}
	return false
}
func normalizeProto(s string) string {
	if strings.HasPrefix(s, "tcp") {
		return "tcp"
	}
	return "udp"
}
func positiveInt(s string) bool { v, e := strconv.Atoi(s); return e == nil && v > 0 && v < 1<<31 }
func validPort(s string) bool   { v, e := strconv.Atoi(s); return e == nil && v > 0 && v <= 65535 }

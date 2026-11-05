package manage

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

type portRange struct{ first, last int }

var hoppingPattern = regexp.MustCompile(`^\d+(?::\d+)?$`)

func parsePortRanges(values []string) ([]portRange, error) {
	ranges := make([]portRange, 0, len(values))
	for _, value := range values {
		if !hoppingPattern.MatchString(value) {
			return nil, errors.New("多端口使用 20000:20100 或单端口格式")
		}
		first, last, interval := strings.Cut(value, ":")
		if !interval {
			last = first
		}
		a, errA := strconv.Atoi(first)
		b, errB := strconv.Atoi(last)
		if errA != nil || errB != nil || a < 1 || b < a || b > 65535 {
			return nil, errors.New("端口范围须为 1-65535，起始端口不能大于结束端口")
		}
		ranges = append(ranges, portRange{a, b})
	}
	slices.SortFunc(ranges, func(a, b portRange) int { return a.first - b.first })
	merged := ranges[:0]
	for _, r := range ranges {
		if n := len(merged); n > 0 && r.first <= merged[n-1].last+1 {
			merged[n-1].last = max(merged[n-1].last, r.last)
		} else {
			merged = append(merged, r)
		}
	}
	return merged, nil
}

func (r portRange) String() string {
	if r.first == r.last {
		return strconv.Itoa(r.first)
	}
	return strconv.Itoa(r.first) + ":" + strconv.Itoa(r.last)
}

func portInRanges(port int, ranges []portRange) bool {
	for _, r := range ranges {
		if port >= r.first && port <= r.last {
			return true
		}
	}
	return false
}

func forwardPorts(profile Profile, protocol string) []string {
	if protocol == "hysteria2" {
		return profile.Hopping
	}
	if protocol == "tuic" {
		return profile.TUICPorts
	}
	return nil
}

func profileOwner(country string) string {
	if country == "" {
		return normalScope
	}
	return vpngateScope + ":" + country
}

type portReservation struct {
	portRange
	owner, protocol string
}

// Keep ranges compact: reserving 1:65535 must not allocate 65,535 map entries.
func portReservations(app App, settings VPNSettings) ([]portReservation, error) {
	var out []portReservation
	add := func(profile Profile, owner string) error {
		for _, protocol := range []string{"hysteria2", "tuic"} {
			ranges, err := parsePortRanges(forwardPorts(profile, protocol))
			if err != nil {
				return fmt.Errorf("%s %s 多端口配置错误: %w", owner, protocol, err)
			}
			for _, r := range ranges {
				out = append(out, portReservation{r, owner, protocol})
			}
		}
		return nil
	}
	owner := normalScope
	for _, in := range app.Inbounds {
		port := number(in["listen_port"])
		out = append(out, portReservation{portRange{port, port}, owner, text(in["type"])})
	}
	if err := add(app.Profile, owner); err != nil {
		return nil, err
	}
	for country, profile := range settings.Profiles {
		owner := profileOwner(country)
		for protocol, port := range profile.Ports {
			out = append(out, portReservation{portRange{port, port}, owner, protocol})
		}
		if err := add(profile, owner); err != nil {
			return nil, err
		}
	}
	for _, port := range []int{app.SocksPort, settings.ProxyPort} {
		if port > 0 {
			out = append(out, portReservation{portRange{port, port}, "", ""})
		}
	}
	return out, nil
}

func usedPortRanges(app App, settings VPNSettings) ([]portRange, error) {
	reservations, err := portReservations(app, settings)
	if err != nil {
		return nil, err
	}
	ranges := make([]portRange, len(reservations))
	for i, reservation := range reservations {
		ranges[i] = reservation.portRange
	}
	return ranges, nil
}

func allocationPorts(root string, app App, settings VPNSettings) (map[int]bool, []portRange, error) {
	used := usedPorts(app, settings)
	ranges, err := usedPortRanges(app, settings)
	if err != nil {
		return nil, nil, err
	}
	var subscription subscriptionSettings
	if _, err := ReadJSON(filepath.Join(root, "subscription.json"), &subscription); err != nil {
		return nil, nil, err
	}
	if subscription.Enabled {
		used[subscription.Port] = true
	}
	return used, ranges, nil
}

func portMutation(operation string, values []string) (string, []portRange, error) {
	protocol, ports := "hysteria2", values
	if operation != "hopping" {
		if len(values) == 0 {
			return "", nil, errors.New("需要协议")
		}
		protocol, ports = values[0], values[1:]
	}
	if operation == "port" {
		if !slices.Contains(Protocols, protocol) || len(ports) != 1 || strings.Contains(ports[0], ":") {
			return "", nil, errors.New("port 需要有效协议和一个主端口")
		}
	} else if protocol != "hysteria2" && protocol != "tuic" {
		return "", nil, errors.New("多端口仅支持 hysteria2 或 tuic")
	}
	ranges, err := parsePortRanges(ports)
	return protocol, ranges, err
}

func validatePortMutation(root string, app App, settings VPNSettings, scope, operation string, values []string) error {
	protocol, ranges, err := portMutation(operation, values)
	if err != nil {
		return err
	}
	profile := app.Profile
	owner := normalScope
	if scope == vpngateScope {
		var exists bool
		profile, exists = settings.Profiles[settings.ActiveCountry]
		if !exists {
			return errors.New("请先配置 VPNGate 国家")
		}
		owner = profileOwner(settings.ActiveCountry)
	}
	if profile.Ports[protocol] < 1 {
		return errors.New("未找到所选协议的主端口")
	}
	reservations, err := portReservations(app, settings)
	if err != nil {
		return err
	}
	if root != "" {
		var subscription subscriptionSettings
		if _, err := ReadJSON(filepath.Join(root, "subscription.json"), &subscription); err != nil {
			return err
		}
		if subscription.Enabled {
			reservations = append(reservations, portReservation{portRange{subscription.Port, subscription.Port}, "", "subscription"})
		}
	}
	if err := validateReservedPorts(reservations, owner, protocol, ranges); err != nil {
		return err
	}
	if root != "" && len(ranges) > 0 {
		return checkSystemPorts(ranges, profile.Ports[protocol])
	}
	return nil
}

func validateReservedPorts(reservations []portReservation, owner, protocol string, ranges []portRange) error {
	for _, r := range ranges {
		for _, occupied := range reservations {
			if occupied.owner == owner && occupied.protocol == protocol {
				continue
			}
			if r.first <= occupied.last && occupied.first <= r.last {
				return fmt.Errorf("端口 %s 与其他节点、代理或订阅端口冲突", r.String())
			}
		}
	}
	return nil
}

func checkSystemPorts(ranges []portRange, ownPort int) error {
	if runtime.GOOS != "linux" {
		for _, r := range ranges {
			for port := r.first; port <= r.last; port++ {
				if port != ownPort {
					if err := probePort(port); err != nil {
						return fmt.Errorf("端口 %d 已被其他程序使用: %w", port, err)
					}
				}
			}
		}
		return nil
	}
	// Read the kernel's socket inventory once instead of binding every port in
	// a range. Include IPv6 and established UDP sockets, but only TCP listeners.
	for _, table := range []string{"tcp", "tcp6", "udp", "udp6"} {
		file, err := os.Open("/proc/net/" + table)
		if errors.Is(err, os.ErrNotExist) && strings.HasSuffix(table, "6") {
			continue
		}
		if err != nil {
			return err
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) < 4 || strings.HasPrefix(table, "tcp") && fields[3] != "0A" {
				continue
			}
			_, rawPort, valid := strings.Cut(fields[1], ":")
			port, err := strconv.ParseInt(rawPort, 16, 32)
			if valid && err == nil && int(port) != ownPort && portInRanges(int(port), ranges) {
				file.Close()
				return fmt.Errorf("端口 %d 已被其他程序使用", port)
			}
		}
		err = scanner.Err()
		file.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func probePort(port int) error {
	for _, probe := range []struct{ family, host string }{{"4", "0.0.0.0"}, {"6", "::"}, {"4", "127.0.0.1"}, {"6", "::1"}} {
		// Unix wildcard binds cover loopback; Windows can accept both bindings.
		if runtime.GOOS != "windows" && (probe.host == "127.0.0.1" || probe.host == "::1") {
			continue
		}
		address := net.JoinHostPort(probe.host, strconv.Itoa(port))
		tcp, err := net.Listen("tcp"+probe.family, address)
		if err == nil {
			var udp net.PacketConn
			udp, err = net.ListenPacket("udp"+probe.family, address)
			if udp != nil {
				udp.Close()
			}
			tcp.Close()
		}
		if probe.family == "6" && (errors.Is(err, syscall.EAFNOSUPPORT) || errors.Is(err, syscall.EADDRNOTAVAIL) || errors.Is(err, syscall.EPROTONOSUPPORT)) {
			continue
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func changePorts(app *App, settings VPNSettings, operation string, values []string) error {
	if err := validatePortMutation("", *app, settings, normalScope, operation, values); err != nil {
		return err
	}
	protocol, ranges, _ := portMutation(operation, values)
	return applyPorts(app, operation, protocol, ranges)
}

// Scope validation belongs to the caller; both node types share this mutation.
func applyPorts(app *App, operation, protocol string, ranges []portRange) error {
	if operation == "port" {
		port := ranges[0].first
		if port != app.Profile.Ports[protocol] {
			if err := probePort(port); err != nil {
				return err
			}
		}
		for _, in := range app.Inbounds {
			if in["type"] == protocol {
				in["listen_port"] = port
			}
		}
		app.Profile.Ports[protocol] = port
		return nil
	}
	ports := make([]string, len(ranges))
	for i, r := range ranges {
		ports[i] = r.String()
	}
	if protocol == "hysteria2" {
		app.Profile.Hopping = ports
	} else {
		app.Profile.TUICPorts = ports
	}
	return nil
}

func writeTUICLink(output io.Writer, app App, settings VPNSettings, private map[string]Secrets, scope string, port int) error {
	profile, inbounds := app.Profile, app.Inbounds
	name := Labels["tuic"] + "-普通-" + app.Name
	if scope == vpngateScope {
		country := settings.ActiveCountry
		var exists bool
		profile, exists = settings.Profiles[country]
		secret, authenticated := private[country]
		if !exists || !authenticated {
			return errors.New("当前 VPNGate 国家缺少入口或凭据")
		}
		profile.Secrets = secret
		inbounds = MakeInbounds(profile, "vg-"+country+"-")
		label := Countries[country]
		if label == "" {
			label = country
		}
		name = "VPNGate-" + country + "-" + label + "-" + Labels["tuic"] + "-家宽候选"
	}
	ranges, err := parsePortRanges(profile.TUICPorts)
	if err != nil {
		return err
	}
	if port != profile.Ports["tuic"] && !portInRanges(port, ranges) {
		return errors.New("此端口未配置为 TUIC 主端口或复用端口")
	}
	for _, inbound := range inbounds {
		if inbound["type"] != "tuic" {
			continue
		}
		cache := certificateCache{}
		profile, err = outboundProfile(inbound, profile, cache)
		if err != nil {
			return err
		}
		node, err := outboundWithCache(inbound, profile, app.Address, name+"-端口-"+strconv.Itoa(port), cache)
		if err != nil {
			return err
		}
		node["server_port"] = port
		link, err := uriWithCache(node, profile, cache)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(output, link)
		return err
	}
	return errors.New("缺少 TUIC 入口")
}

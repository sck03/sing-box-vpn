package manage

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

type portRange struct{ first, last int }

// Visit each candidate at most once from a random starting point. Reserved
// ranges skip directly to their end and never consume the OS probe budget.
func AvailablePort(used map[int]bool, excluded ...portRange) (int, error) {
	const first, count, maxProbes = 20000, 40000, 200
	start, err := rand.Int(rand.Reader, big.NewInt(count))
	if err != nil {
		return 0, err
	}
	probes := 0
	startOffset := int(start.Int64())
	for offset := 0; offset < count; {
		port := first + (startOffset+offset)%count
		skip := 0
		for _, r := range excluded {
			if port >= r.first && port <= r.last {
				skip = max(skip, min(r.last, first+count-1)-port+1)
			}
		}
		offset += max(1, skip)
		if skip > 0 || used[port] {
			continue
		}
		if err = probePort(port); err == nil {
			used[port] = true
			return port, nil
		}
		probes++
		if probes == maxProbes {
			return 0, fmt.Errorf("探测 %d 个未预留端口均不可用: %w", probes, err)
		}
	}
	return 0, errors.New("自动分配范围 20000-59999 内没有可用入口端口")
}

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
	panel := PanelOptions{Listen: "127.0.0.1:2096"}
	if _, err := ReadJSON(filepath.Join(root, "panel.json"), &panel); err != nil {
		return nil, nil, err
	}
	_, panelPort, err := net.SplitHostPort(panel.Listen)
	if err != nil {
		return nil, nil, err
	}
	port, err := strconv.Atoi(panelPort)
	if err != nil {
		return nil, nil, err
	}
	used[port] = true
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

// Scope validation belongs to the caller; both node types share this mutation.

// TUIC forwarding changes only firewall metadata. Its single server listener,
// credentials and exported primary node stay identical, so no core process or
// subscription regeneration is needed. The caller holds the configuration lock
// and validates reservations and system sockets before entering this path.

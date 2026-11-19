package manage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

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
	profile := app.fixtureProfile()
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
		err = checkPortInventory(file, strings.HasPrefix(table, "tcp"), ranges, ownPort)
		file.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", table, err)
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

func applyPorts(app *App, operation, protocol string, ranges []portRange) error {
	if operation == "port" {
		port := ranges[0].first
		if port != app.fixtureProfile().Ports[protocol] {
			if err := probePort(port); err != nil {
				return err
			}
		}
		for i := range app.Nodes {
			if app.Nodes[i].Protocol == protocol {
				app.Nodes[i].Port = port
			}
		}
		return nil
	}
	ports := make([]string, len(ranges))
	for i, r := range ranges {
		ports[i] = r.String()
	}
	if protocol == "hysteria2" {
		app.Hopping = ports
	} else {
		app.TUICPorts = ports
	}
	return nil
}

func commitTUICPorts(root string, app App, settings VPNSettings, scope string, values []string) error {
	ranges, err := parsePortRanges(values)
	if err != nil {
		return err
	}
	current := &app
	name := "app.json"
	if scope == vpngateScope {
		current = &App{Country: settings.Profiles[settings.ActiveCountry].Country, Hopping: settings.Profiles[settings.ActiveCountry].Hopping, TUICPorts: settings.Profiles[settings.ActiveCountry].TUICPorts}
		name = "vpngate/settings.json"
	}
	if err := applyPorts(current, "multiport", "tuic", ranges); err != nil {
		return err
	}
	var value any = app
	if scope == vpngateScope {
		p := settings.Profiles[settings.ActiveCountry]
		p.TUICPorts = current.TUICPorts
		settings.Profiles[settings.ActiveCountry] = p
		value = settings
	}
	files := fileBundle{}
	if err := files.json(name, value); err != nil {
		return err
	}
	return publish(root, files)
}

func writeTUICLink(output io.Writer, app App, settings VPNSettings, private map[string]Secrets, scope string, port int) error {
	profile, inbounds := app.fixtureProfile(), app.Inbounds()
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
		managed, err := managedNodes(App{Nodes: fixtureNodes([]Object{inbound}), Country: profile.Country, Hopping: profile.Hopping, TUICPorts: profile.TUICPorts})
		if err != nil {
			return err
		}
		node, err := managed[0].subscriptionNode(app.Address, name+"-端口-"+strconv.Itoa(port), scope, nil, cache)
		if err != nil {
			return err
		}
		node.Port = port
		_, err = fmt.Fprintln(output, node.URI())
		return err
	}
	return errors.New("缺少 TUIC 入口")
}

package hostservice

import (
	"context"
	"regexp"
	"strings"
)

type Tunnel struct {
	Invocation string `json:"invocation"`
	Domain     string `json:"domain"`
}

var invocationPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var tunnelDomainPattern = regexp.MustCompile(`https://([a-z0-9-]+\.trycloudflare\.com)`)

// ObserveTunnel retains a confirmed hostname for exactly one running instance.
// Once captured, journal rotation cannot invalidate it and no log scan is needed.
func ObserveTunnel(ctx context.Context, run Command, previous Tunnel) (Tunnel, error) {
	data, err := run(ctx, "systemctl", "show", "sb-argo", "--property=InvocationID,ActiveState")
	if err != nil {
		return previous, err
	}
	id, active := "", false
	for _, line := range strings.Split(string(data), "\n") {
		key, value, _ := strings.Cut(line, "=")
		if key == "InvocationID" {
			id = value
		}
		if key == "ActiveState" {
			active = value == "active"
		}
	}
	if !active || !invocationPattern.MatchString(id) {
		return Tunnel{}, nil
	}
	if id == previous.Invocation && previous.Domain != "" {
		return previous, nil
	}
	next := Tunnel{Invocation: id}
	data, err = run(ctx, "journalctl", "_SYSTEMD_INVOCATION_ID="+id, "-n", "200", "--no-pager", "-o", "cat")
	if err != nil {
		return previous, err
	}
	if len(data) > 64<<10 {
		data = data[len(data)-(64<<10):]
	}
	matches := tunnelDomainPattern.FindAllStringSubmatch(string(data), -1)
	if len(matches) > 0 {
		next.Domain = matches[len(matches)-1][1]
	}
	return next, nil
}

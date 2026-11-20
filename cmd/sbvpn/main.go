// Command sbvpn is the composition root for the management and VPNGate commands.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	project "github.com/sck03/sing-box-vpn"
	"github.com/sck03/sing-box-vpn/internal/manage"
	"github.com/sck03/sing-box-vpn/internal/vpngate"
)

var version string

func main() {
	if version != "" {
		project.Versions.Release = "v" + strings.TrimPrefix(version, "v")
	}
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "sbvpn:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "--help" {
		fmt.Println("sbvpn panel|core|firewall|subscription|daemon|ctl|worker|cleanup|--version")
		return nil
	}
	switch args[0] {
	case "--version":
		fmt.Println(strings.TrimPrefix(project.Versions.Release, "v"))
		return nil
	case "--runtime-version":
		fmt.Println(project.Versions.ManagedRuntime)
		return nil
	case "panel", "core", "firewall", "subscription":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		switch args[0] {
		case "panel":
			return manage.RunPanel(ctx, args[1:], os.Stdin, os.Stdout)
		case "core":
			return manage.RunCore(ctx, args[1:], os.Stdout)
		case "firewall":
			return manage.RunFirewall(ctx, args[1:], os.Stdout)
		case "subscription":
			return manage.RunSubscription(ctx, args[1:], os.Stdout)
		}
	}
	return vpngate.Run(args, manage.ConfigTransaction)
}

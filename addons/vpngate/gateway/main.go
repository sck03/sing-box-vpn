package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	project "github.com/sck03/sing-box-vpn"
	"github.com/sck03/sing-box-vpn/internal/manage"
)

var version string

func init() {
	if version == "" {
		version = strings.TrimPrefix(project.Versions.Release, "v")
	}
}

const defaultConfig = "/etc/s-box/vpngate/gateway.json"

func main() {
	if e := entry(os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, "yg-vpngate:", e)
		os.Exit(1)
	}
}
func entry(args []string) error {
	if len(args) == 0 || args[0] == "--help" {
		fmt.Println("yg-vpngate daemon|ctl|worker|cleanup|config|core|firewall|asset|remote|subscription|--version\nctl: status, nodes, start, stop, swap, reload, refresh, mode, policy")
		return nil
	}
	if args[0] == "--version" {
		fmt.Println(version)
		return nil
	}
	switch args[0] {
	case "config", "core", "firewall", "asset", "remote", "subscription":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		switch args[0] {
		case "config":
			return manage.RunConfig(ctx, args[1:], os.Stdout)
		case "core":
			return manage.RunCore(ctx, args[1:], os.Stdout)
		case "firewall":
			return manage.RunFirewall(ctx, args[1:], os.Stdout)
		case "asset":
			return manage.RunAsset(ctx, args[1:], os.Stdout)
		case "remote":
			return manage.RunRemote(ctx, args[1:], os.Stdin, os.Stdout)
		case "subscription":
			return manage.RunSubscription(ctx, args[1:], os.Stdout)
		}
		return errors.New("unknown management command")
	case "daemon":
		f := flag.NewFlagSet("daemon", flag.ContinueOnError)
		path := f.String("config", defaultConfig, "configuration file")
		if e := f.Parse(args[1:]); e != nil {
			return e
		}
		return runDaemon(*path)
	case "worker":
		f := flag.NewFlagSet("worker", flag.ContinueOnError)
		socket := f.String("socket", "", "private Unix socket")
		dns := f.String("dns", "1.1.1.1:53", "tunnel DNS")
		if e := f.Parse(args[1:]); e != nil {
			return e
		}
		if runtime.GOOS != "linux" {
			return errors.New("worker is a Linux network-namespace process")
		}
		if *socket == "" {
			return errors.New("worker socket missing")
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return (&worker{socket: *socket, dns: *dns}).serve(ctx)
	case "ctl":
		return runCTL(args[1:])
	case "cleanup":
		f := flag.NewFlagSet("cleanup", flag.ContinueOnError)
		path := f.String("config", defaultConfig, "configuration file")
		if e := f.Parse(args[1:]); e != nil {
			return e
		}
		c, e := readConfig(*path)
		if e != nil {
			return e
		}
		if e = preflight(); e != nil {
			if runtime.GOOS != "linux" || os.Geteuid() != 0 {
				return e
			}
		}
		if daemonAlive(c.DataDir) {
			return errors.New("stop the gateway daemon before cleanup")
		}
		unlock, lockErr := lockDaemon(c.DataDir)
		if os.IsNotExist(lockErr) {
			return nil
		}
		if lockErr != nil {
			return lockErr
		}
		defer unlock()
		b := &backend{cfg: c}
		data, e := os.ReadFile(filepath.Join(c.DataDir, "namespace-owner.json"))
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		if json.Unmarshal(data, &b.owner) != nil || b.owner.Owner != ownerID {
			return errors.New("resource ownership cannot be verified")
		}
		b.claimed = true
		b.stop()
		return b.cleanupErr
	default:
		return errors.New("unknown command")
	}
}

func daemonAlive(dir string) bool {
	f, e := os.OpenFile(filepath.Join(dir, "daemon.lock"), os.O_RDWR, 0600)
	if os.IsNotExist(e) {
		return false
	}
	if e != nil {
		return true
	}
	defer f.Close()
	return manage.TryLockFile(f) != nil
}
func lockDaemon(dir string) (func(), error) {
	path := filepath.Join(dir, "daemon.lock")
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = manage.TryLockFile(f); e != nil {
		f.Close()
		return nil, errors.New("another gateway daemon owns this directory")
	}
	if e = f.Truncate(0); e == nil {
		_, e = fmt.Fprintf(f, "%d\n", os.Getpid())
	}
	if e != nil {
		f.Close()
		return nil, e
	}
	return func() { f.Close() }, nil
}

func runDaemon(path string) error {
	c, e := readConfig(path)
	if e != nil {
		return e
	}
	if e = preflight(); e != nil {
		return e
	}
	for _, d := range []string{c.DataDir, c.RunDir} {
		if e = os.MkdirAll(d, 0700); e != nil {
			return e
		}
		if e = os.Chmod(d, 0700); e != nil {
			return e
		}
	}
	unlock, e := lockDaemon(c.DataDir)
	if e != nil {
		return e
	}
	defer unlock()
	socket := filepath.Join(c.RunDir, "control.sock")
	_ = os.Remove(socket)
	control, e := net.Listen("unix", socket)
	if e != nil {
		return e
	}
	defer control.Close()
	defer os.Remove(socket)
	if e = os.Chmod(socket, 0600); e != nil {
		return e
	}
	proxy, e := net.Listen("tcp4", c.Listen)
	if e != nil {
		return e
	}
	defer proxy.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	m := newManager(c, path)
	api := http.Server{Handler: m.handler(ctx), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan struct{})
	serveErrors := make(chan error, 2)
	var listeners sync.WaitGroup
	listeners.Add(2)
	go func() { defer close(done); m.run(ctx) }()
	go func() {
		defer listeners.Done()
		if e := api.Serve(control); e != nil && !errors.Is(e, http.ErrServerClosed) {
			serveErrors <- fmt.Errorf("control listener failed: %w", e)
			cancel()
		}
	}()
	go func() {
		defer listeners.Done()
		if e := serveProxy(ctx, proxy, m); e != nil {
			serveErrors <- fmt.Errorf("proxy listener failed: %w", e)
			cancel()
		}
	}()
	<-ctx.Done()
	m.revoke()
	_ = api.Close()
	<-done
	listeners.Wait()
	select {
	case e := <-serveErrors:
		return e
	default:
		return nil
	}
}

// managed-core isolates the pinned proxy and quota enforcement from the panel.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
	runtime "github.com/sck03/sing-box-vpn/internal/runtime"
)

func run() error {
	f := flag.NewFlagSet("managed-core", flag.ContinueOnError)
	config := f.String("config", "", "generated core configuration")
	database := f.String("database", "", "account database")
	instance := f.String("instance", "normal", "normal or vpngate")
	check := f.Bool("check", false, "construct and validate without listening")
	version := f.Bool("version", false, "runtime version")
	if err := f.Parse(os.Args[1:]); err != nil {
		return err
	}
	if *version {
		fmt.Println(runtime.Version)
		return nil
	}
	if *config == "" || *database == "" || (*instance != "normal" && *instance != "vpngate") {
		return errors.New("config, database and a valid instance are required")
	}
	info, err := os.Stat(*config)
	if err != nil {
		return err
	}
	if info.Size() > 16<<20 {
		return errors.New("configuration exceeds 16 MiB")
	}
	data, err := os.ReadFile(*config)
	if err != nil {
		return err
	}
	store, err := accounting.Open(*database)
	if err != nil {
		return err
	}
	defer store.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	meter := accounting.NewMeter(store, *instance)
	core, err := runtime.New(ctx, data, store, *instance, meter)
	if err != nil {
		return err
	}
	if *check {
		return core.Close()
	}
	if err = core.Start(); err != nil {
		core.Close()
		return err
	}
	control, err := controlstore.Open(*database)
	if err != nil {
		core.Close()
		return err
	}
	defer control.Close()
	var revision int64
	if data, e := os.ReadFile(filepath.Join(filepath.Dir(*database), "runtime-revision.json")); e == nil {
		if e = json.Unmarshal(data, &revision); e != nil {
			core.Close()
			return e
		}
	}
	started := time.Now().UnixNano()
	if err = control.Heartbeat(ctx, *instance, runtime.Version, revision, started); err != nil {
		core.Close()
		return err
	}
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	result := make(chan error, 1)
	go func() { result <- meter.Run(ctx) }()
	running := true
	for running {
		select {
		case <-ctx.Done():
			running = false
		case err = <-result:
			stop()
			running = false
		case <-ticker.C:
			if err = control.Heartbeat(ctx, *instance, runtime.Version, revision, started); err != nil {
				stop()
				running = false
			}
		}
	}
	meter.Shutdown()
	err = errors.Join(err, core.Close())
	settle, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return errors.Join(err, meter.Close(settle))
}
func main() {
	if err := run(); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

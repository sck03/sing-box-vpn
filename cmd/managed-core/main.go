// managed-core isolates the pinned proxy and quota enforcement from the panel.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
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
	result := make(chan error, 1)
	go func() { result <- meter.Run(ctx) }()
	select {
	case <-ctx.Done():
	case err = <-result:
		stop()
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

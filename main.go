package main

import (
	"context"
	"fmt"
	"github.com/elankath/go-perfmon/api"
	"github.com/elankath/go-perfmon/cli"
	"github.com/elankath/go-perfmon/core"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	var mainOpts cli.MainOpts
	var err error
	var exitCode int

	mainFlags := cli.SetupMainFlagsToOpts(&mainOpts)
	err = mainFlags.Parse(os.Args[1:])
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error parsing flags: %v\n", err)
		mainFlags.Usage()
		os.Exit(cli.ExitOptsParseErr)
	}

	exitCode, err = cli.ValidateMainOpts(&mainOpts)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Err: %v\n", err.Error())
		mainFlags.Usage()
		os.Exit(exitCode)
	}
	mainOpts.ProcessNames = mainFlags.Args()
	if len(mainOpts.ProcessNames) == 0 {
		_, _ = fmt.Fprintf(os.Stderr, "process name args are mandatory")
		mainFlags.Usage()
		os.Exit(cli.ExitMissingArgs)
	}

	if mainOpts.Interval == time.Duration(0) {
		mainOpts.Interval = 10 * time.Second
	}

	opts := api.PerfMonOpts{
		ProcessNames: mainOpts.ProcessNames,
		Interval:     mainOpts.Interval,
		WaitForProc:  mainOpts.WaitForProc,
		ErrThreshold: mainOpts.ErrThreshold,
	}
	perfmon, err := core.NewBasicPerfMon(opts)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Err: %v\n", err.Error())
		os.Exit(cli.ExitCreatePerfMon)
	}
	fmt.Printf("perfmon Opts: %v\n", opts)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	err = perfmon.Start(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Err: %v\n", err.Error())
		os.Exit(cli.ExitStartPerfMon)
	}
	sig := <-sigCh
	slog.Info("Received shutdown signal, initiating graceful shutdown...", "signal", sig)

}

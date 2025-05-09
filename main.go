package main

import (
	"context"
	"fmt"
	"github.com/elankath/procmon/api"
	"github.com/elankath/procmon/cli"
	"github.com/elankath/procmon/core"
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

	mainOpts.ProcessNames = mainFlags.Args()
	if mainOpts.Interval == time.Duration(0) {
		mainOpts.Interval = 10 * time.Second
	}

	exitCode, err = cli.ValidateMainOpts(&mainOpts)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Err: %v\n", err.Error())
		mainFlags.Usage()
		os.Exit(exitCode)
	}

	config := api.ProcessMonitorConfig{
		ProcessNames:     mainOpts.ProcessNames,
		Interval:         mainOpts.Interval,
		WaitForProc:      mainOpts.WaitForProc,
		ErrThreshold:     mainOpts.ErrThreshold,
		ReportDir:        mainOpts.ReportDir,
		ReportNamePrefix: mainOpts.ReportNamePrefix,
	}
	procmon, err := core.NewBasicProcessMonitor(config)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Err: %v\n", err.Error())
		os.Exit(cli.ExitCreateprocmon)
	}
	slog.Info("procmon config", "config", config)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	err = procmon.Start(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Err: %v\n", err.Error())
		os.Exit(cli.ExitStartprocmon)
	}
	sig := <-sigCh
	slog.Info("Received shutdown signal, initiating graceful shutdown...", "signal", sig)

}

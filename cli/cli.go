package cli

import (
	"flag"
	"fmt"
	"os"
	"time"
)

const (
	ExitSuccess int = iota
	ExitOptsParseErr
	ExitMissingArgs
	ExitCreatePerfMon
	ExitStartPerfMon
	ExitGeneral = 255
)

type MainOpts struct {
	Interval     time.Duration
	WaitForProc  bool
	ErrThreshold int
	ProcessNames []string
}

func ValidateMainOpts(mainOpts *MainOpts) (exitCode int, err error) {
	//TODO: fill me up
	return
}
func SetupMainFlagsToOpts(mainOpts *MainOpts) *flag.FlagSet {
	mainFlags := flag.NewFlagSet("main", flag.ContinueOnError)
	mainFlags.BoolVar(&mainOpts.WaitForProc, "wait", true, "Whether to wait until processes are available for monitoring or not")
	mainFlags.IntVar(&mainOpts.ErrThreshold, "errt", 3, "Probe error threshold beyond which proc monitoring will stop")
	standardUsage := mainFlags.PrintDefaults
	mainFlags.Usage = func() {
		_, _ = fmt.Fprintln(os.Stderr, "Usage: perform <flags> <args>")
		_, _ = fmt.Fprintln(os.Stderr, "<flags>")
		standardUsage()
		_, _ = fmt.Fprintln(os.Stderr, "<args>: List of process names to monitor from start to end")
	}
	mainFlags.Func("interval", "monitoring interval", func(durStr string) error {
		interval, err := time.ParseDuration(durStr)
		if err != nil {
			return fmt.Errorf("invalid duration: %w", err)
		}
		mainOpts.Interval = interval
		return nil
	})
	return mainFlags
}

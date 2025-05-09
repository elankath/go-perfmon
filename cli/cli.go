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
	ExitMandatoryOpt
	ExitMissingArgs
	ExitCreateprocmon
	ExitStartprocmon
	ExitGeneral = 255
)

type MainOpts struct {
	Interval         time.Duration
	WaitForProc      bool
	ErrThreshold     int
	ProcessNames     []string
	ReportDir        string
	ReportNamePrefix string
}

func ValidateMainOpts(mainOpts *MainOpts) (exitCode int, err error) {
	if len(mainOpts.ProcessNames) == 0 {
		exitCode = ExitMissingArgs
		err = fmt.Errorf("args (process names) is mandatory")
		return
	}

	if mainOpts.ReportNamePrefix == "" {
		exitCode = ExitMandatoryOpt
		err = fmt.Errorf("report name prefix is mandatory")
		return
	}

	return
}
func SetupMainFlagsToOpts(mainOpts *MainOpts) *flag.FlagSet {
	mainFlags := flag.NewFlagSet("main", flag.ContinueOnError)
	mainFlags.BoolVar(&mainOpts.WaitForProc, "wait", true, "Whether to wait until processes are available for monitoring or not")
	mainFlags.IntVar(&mainOpts.ErrThreshold, "errt", 3, "Probe error threshold beyond which proc monitoring will stop")
	mainFlags.StringVar(&mainOpts.ReportDir, "d", "/tmp", "Directory for reports and charts")
	mainFlags.StringVar(&mainOpts.ReportNamePrefix, "n", "", "report name prefix")
	standardUsage := mainFlags.PrintDefaults
	mainFlags.Usage = func() {
		_, _ = fmt.Fprintln(os.Stderr, "Usage: procmon <flags> <args>")
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

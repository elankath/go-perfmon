package api

import "errors"

var (
	ErrCreateReportDir   = errors.New("cannot create report directory")
	ErrCannotFindProcess = errors.New("cannot find process")

	ErrGetMetrics    = errors.New("cannot get metrics")
	ErrStoppedByUser = errors.New("stopped by user")

	ErrThresholdExceeded = errors.New("error threshold exceeded")
)

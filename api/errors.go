package api

import "errors"

var (
	ErrMissingOpt        = errors.New("missing option")
	ErrCannotFindProcess = errors.New("cannot find process")

	ErrGetMetrics = errors.New("cannot get metrics")
)

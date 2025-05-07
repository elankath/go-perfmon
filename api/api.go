package api

import (
	"context"
	"time"
)

type PerfMon interface {
	Start(ctx context.Context) error
	Stop() error
}

type PerfMonOpts struct {
	ProcessNames []string
	Interval     time.Duration
	WaitForProc  bool
	ErrThreshold int
}

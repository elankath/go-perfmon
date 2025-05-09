package api

import (
	"context"
	"time"
)

type ProcessMonitor interface {
	Start(ctx context.Context) error
	Stop()
}

type ProcessMonitorConfig struct {
	ProcessNames     []string
	Interval         time.Duration
	WaitForProc      bool
	ErrThreshold     int
	ReportDir        string
	ReportNamePrefix string
}

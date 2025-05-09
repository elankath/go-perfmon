package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"strings"
	"time"

	"github.com/elankath/procmon/api"
	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/components"
	"github.com/go-echarts/go-echarts/v2/opts"
	"github.com/shirou/gopsutil/process"
)

type basicProcessMonitor struct {
	cfg           api.ProcessMonitorConfig
	procByName    map[string]*process.Process
	errCounter    int
	metricsByName map[string][]Metric
	cancel        context.CancelCauseFunc
}

type Metric struct {
	ProbeTime time.Time
	MemRSS    uint64
	MemVMS    uint64
	CPU       float64
}

func NewBasicProcessMonitor(cfg api.ProcessMonitorConfig) (api.ProcessMonitor, error) {
	pm := &basicProcessMonitor{
		cfg:           cfg,
		procByName:    make(map[string]*process.Process),
		metricsByName: make(map[string][]Metric),
	}
	for _, n := range cfg.ProcessNames {
		pm.procByName[n] = nil
	}
	return pm, nil
}

func (b *basicProcessMonitor) Start(ctx context.Context) error {
	ctx, b.cancel = context.WithCancelCause(ctx)
	var err error
	var ok bool
	if !b.cfg.WaitForProc {
		ok, err = b.registerProcesses()
		if err != nil {
			return nil
		}
		if !ok {
			return errors.New("failed to register processes")
		}
	} else {
		go func() {
			for {
				select {
				case <-ctx.Done():
					slog.Info("Stopping procmon due to context cancellation", "reason", ctx.Err())
					return
				case <-time.After(b.cfg.Interval):
					slog.Info("Attempting to register processes")
					ok, err := b.registerProcesses()
					if err != nil {
						slog.Info("non-recoverable error, stopping", "error", err)
						b.Stop()
						b.cancel(err)
					}
					if ok {
						slog.Info("All processes are registered", "procNames", b.cfg.ProcessNames)
						return
					}
				}
			}
		}()
	}
	go b.monitorProcsEveryInterval(ctx)
	slog.Info("procmon started", "procNames", b.cfg.ProcessNames)
	//<-b.stopChan
	return nil
}

func (b *basicProcessMonitor) monitorProcsEveryInterval(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			slog.Info("Stopping procmon due to context cancellation", "reason", ctx.Err())
			return
		case <-time.After(b.cfg.Interval):
			b.monitorProcs()
		}
	}
}

func (b *basicProcessMonitor) monitorProcs() {
	for n, proc := range b.procByName {
		if proc == nil {
			continue
		}
		running, err := proc.IsRunning()
		if err != nil {
			if !b.handleMetricErr(err) {
				return
			}
			slog.Warn("Failed to check if process is running", "name", n, "pid", proc.Pid, "err", err)
			continue
		}
		if !running {
			slog.Warn("Process is no longer running", "proc", n, "pid", proc.Pid)
			delete(b.procByName, n)
			continue
		}
		cpu, err := proc.CPUPercent()
		if err != nil {
			if !b.handleMetricErr(err) {
				return
			}
			slog.Warn("Failed to get CPU percent", "procName", n, "errCounter", b.errCounter, "err", err)
			continue
		}
		mem, err := proc.MemoryInfo()
		if err != nil {
			if !b.handleMetricErr(err) {
				return
			}
			slog.Warn("Failed to get Memory percent", "procName", n, "errCounter", b.errCounter, "err", err)
			continue
		}
		m := Metric{
			ProbeTime: time.Now(),
			CPU:       cpu,
			MemRSS:    mem.RSS,
			MemVMS:    mem.VMS,
		}
		metrics := b.metricsByName[n]
		if metrics == nil {
			metrics = make([]Metric, 0, 200)
		}
		metrics = append(metrics, m)
		b.metricsByName[n] = metrics
		slog.Info("Metric captured", "procName", n, "CPU", m.CPU, "MemRSS", m.MemRSS, "MemVMS", m.MemVMS)
	}
	err := b.GenerateCharts()
	if err != nil {
		slog.Error("failed to generate charts", "error:", err)
	}
}

func (b *basicProcessMonitor) GenerateCharts() error {
	var pageCharts []components.Charter
	for procName, metrics := range b.metricsByName {
		procCharts, err := createLineCharts(procName, metrics)
		if err != nil {
			return err
		}
		pageCharts = append(pageCharts, procCharts...)
	}
	if len(pageCharts) == 0 {
		return nil
	}

	page := components.NewPage()
	page.SetPageTitle(fmt.Sprintf("%s Charts", b.cfg.ReportNamePrefix))
	page.AddCharts(pageCharts...)
	chartsPath := path.Join(b.cfg.ReportDir, fmt.Sprintf("%s-charts.html", b.cfg.ReportNamePrefix))

	err := writePage(chartsPath, page)
	if err != nil {
		return err
	}
	return nil
}

func createLineCharts(procName string, metrics []Metric) (procCharts []components.Charter, err error) {
	if len(metrics) < 2 {
		slog.Warn("skipping charts since insufficient metric data points present", "procName", procName)
		return
	}
	var cht *charts.Line
	cht, err = generateCPUChart(procName, metrics)
	if err != nil {
		return
	}
	procCharts = append(procCharts, cht)

	cht, err = generateRssMemoryChart(procName, metrics)
	if err != nil {
		return
	}
	procCharts = append(procCharts, cht)

	cht, err = generateVmsMemoryChart(procName, metrics)
	if err != nil {
		return
	}
	procCharts = append(procCharts, cht)
	return
}

func writePage(chartPath string, page *components.Page) error {
	var buf bytes.Buffer
	err := page.Render(&buf)
	if err != nil {
		slog.Error("error writing bytes", "error", err)
		return err
	}
	err = os.WriteFile(chartPath, buf.Bytes(), 0644)
	if err != nil {
		slog.Error("error writing bytes to file", "error", err)
		return err
	}
	slog.Info("Generated chart", "chartPath", chartPath)
	return nil
}

func generateCPUChart(procName string, metrics []Metric) (*charts.Line, error) {
	var xVals []string
	var yVals []opts.LineData
	for _, m := range metrics {
		xVals = append(xVals, m.ProbeTime.Format("15:04:05"))
		yVals = append(yVals, opts.LineData{Value: m.CPU})
	}

	line := charts.NewLine()

	line.SetGlobalOptions(
		charts.WithXAxisOpts(opts.XAxis{Name: "Time"}),
		charts.WithYAxisOpts(opts.YAxis{Name: "CPU %"}),
		charts.WithTitleOpts(opts.Title{
			Title: fmt.Sprintf("%s CPU usage", procName),
			//Subtitle: fmt.Sprintf("Time based over %s interval"),
		}))
	line.SetXAxis(xVals).
		AddSeries("CPU", yVals).
		SetSeriesOptions(charts.WithLineChartOpts(opts.LineChart{Smooth: opts.Bool(true)}))

	return line, nil
}

func generateRssMemoryChart(procName string, metrics []Metric) (*charts.Line, error) {
	var xVals []string
	var yValsRss []opts.LineData
	for _, m := range metrics {
		xVals = append(xVals, m.ProbeTime.Format("15:04:05"))
		yValsRss = append(yValsRss, opts.LineData{Value: m.MemRSS / (1024 * 1024)})
	}

	line := charts.NewLine()

	line.SetGlobalOptions(
		charts.WithXAxisOpts(opts.XAxis{Name: "Time"}),
		charts.WithYAxisOpts(opts.YAxis{Name: "RSS Memory (MB)"}),
		charts.WithTitleOpts(opts.Title{
			Title: fmt.Sprintf("%s RSS Memory", procName),
			//Subtitle: fmt.Sprintf("Time based over %s interval"),
		}))
	line.SetXAxis(xVals).
		AddSeries("Memory RSS", yValsRss).
		SetSeriesOptions(charts.WithLineChartOpts(opts.LineChart{Smooth: opts.Bool(true)}))

	return line, nil
}

func generateVmsMemoryChart(procName string, metrics []Metric) (*charts.Line, error) {
	var xVals []string
	var yValsVms []opts.LineData
	for _, m := range metrics {
		xVals = append(xVals, m.ProbeTime.Format("15:04:05"))
		yValsVms = append(yValsVms, opts.LineData{Value: m.MemVMS / (1024 * 1024 * 1024)})
	}

	line := charts.NewLine()

	line.SetGlobalOptions(
		charts.WithXAxisOpts(opts.XAxis{Name: "Time"}),
		charts.WithYAxisOpts(opts.YAxis{Name: "VMS Memory (GB)"}),
		charts.WithTitleOpts(opts.Title{
			Title: fmt.Sprintf("%s VMS Memory", procName),
			//Subtitle: fmt.Sprintf("Time based over %s interval"),
		}))
	line.SetXAxis(xVals).
		AddSeries("Memory VMS", yValsVms).
		SetSeriesOptions(charts.WithLineChartOpts(opts.LineChart{Smooth: opts.Bool(true)}))

	return line, nil
}

func (b *basicProcessMonitor) handleMetricErr(err error) (ok bool) {
	if b.errCounter > b.cfg.ErrThreshold {
		err = fmt.Errorf("%w: error count exceeded threshold %d: %w", api.ErrGetMetrics, b.cfg.ErrThreshold, err)
		ok = false
		b.cancel(err)
		return
	}
	b.errCounter++
	ok = true
	return
}

func (b *basicProcessMonitor) registerProcesses() (ok bool, err error) {
	var proc *process.Process
	ok = true
	for _, n := range b.cfg.ProcessNames {
		proc, err = findProcessByName(n)
		if err != nil {
			if errors.Is(err, api.ErrCannotFindProcess) {
				slog.Warn("process does not yet exist", "procName", n)
				err = nil
				ok = false
				continue
			}
			return
		}
		slog.Info("Registering process for monitoring", "procName", n, "pid", proc.Pid)
		b.procByName[n] = proc
	}
	return
}

func findProcessByName(procName string) (proc *process.Process, err error) {
	pids, err := findPIDsByName(procName)
	if err != nil {
		err = fmt.Errorf("%w: failed to find proc for  %q: %w", api.ErrCannotFindProcess, procName, err)
		return
	}
	pid := pids[0]
	proc, err = process.NewProcess(pid)
	if err != nil {
		err = fmt.Errorf("%w: failed to create proc access for %d belonging to %q: %w", api.ErrCannotFindProcess, pid, procName, err)
		return
	} //TODO: give something better later.
	return
}

func (b *basicProcessMonitor) stop(err error) {
	if b.cancel != nil {
		b.cancel(err)
	}
}
func (b *basicProcessMonitor) Stop() {
	slog.Info("Stopping procmon")
	b.stop(api.ErrStoppedByUser)
}

var _ api.ProcessMonitor = (*basicProcessMonitor)(nil)

func findPIDsByName(processName string) ([]int32, error) {
	var pids []int32

	// Get list of all processes
	processes, err := process.Processes()
	if err != nil {
		return nil, fmt.Errorf("error listing processes: %v", err)
	}

	// Iterate through processes and check names
	for _, p := range processes {
		name, err := p.Name()
		if err != nil {
			continue
		}
		if strings.EqualFold(name, processName) || strings.Contains(strings.ToLower(name), strings.ToLower(processName)) {
			pids = append(pids, p.Pid)
		}
	}

	if len(pids) == 0 {
		return nil, fmt.Errorf("%w: no process found with name: %s", api.ErrCannotFindProcess, processName)
	}

	return pids, nil
}

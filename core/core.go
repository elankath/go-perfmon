package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/elankath/go-perfmon/api"
	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/components"
	"github.com/go-echarts/go-echarts/v2/opts"
	"github.com/shirou/gopsutil/process"
	"log/slog"
	"os"
	"path"
	"strings"
	"time"
)

type basicPerfMon struct {
	opts          api.PerfMonOpts
	procByName    map[string]*process.Process
	errChan       chan error
	errCounter    int
	metricsByName map[string][]Metric
	cancel        context.CancelFunc
}

type Metric struct {
	ProbeTime time.Time
	MemRSS    uint64
	MemVMS    uint64
	CPU       float64
}

func NewBasicPerfMon(opts api.PerfMonOpts) (api.PerfMon, error) {
	pm := &basicPerfMon{
		opts:          opts,
		procByName:    make(map[string]*process.Process),
		errChan:       make(chan error, 1),
		metricsByName: make(map[string][]Metric),
	}
	for _, n := range opts.ProcessNames {
		pm.procByName[n] = nil
	}
	return pm, nil
}

func (b *basicPerfMon) ErrCh() <-chan error {
	return b.errChan
}

func (b *basicPerfMon) Start(ctx context.Context) error {
	ctx, b.cancel = context.WithCancel(ctx)
	var err error
	var ok bool
	if !b.opts.WaitForProc {
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
					slog.Info("Stopping perfmon due to context cancellation", "reason", ctx.Err())
					return
				case <-time.After(b.opts.Interval):
					slog.Info("Attempting to register processes")
					ok, err := b.registerProcesses()
					if err != nil {
						slog.Info("non-recoverable error, stopping", "error", err)
						b.errChan <- err
						b.cancel()
					}
					if ok {
						slog.Info("All processes are registered", "procNames", b.opts.ProcessNames)
						return
					}
				}
			}
		}()
	}
	go b.monitorProcsEveryInterval(ctx)
	slog.Info("Perfmon started", "procNames", b.opts.ProcessNames)
	//<-b.stopChan
	return nil
}

func (b *basicPerfMon) monitorProcsEveryInterval(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			slog.Info("Stopping perfmon due to context cancellation", "reason", ctx.Err())
			return
		case <-time.After(b.opts.Interval):
			b.monitorProcs()
		}
	}
}

func (b *basicPerfMon) monitorProcs() {
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

func (b *basicPerfMon) GenerateCharts() error {
	for procName, metrics := range b.metricsByName {
		err := generateChart(procName, b.opts.ReportDir, metrics)
		if err != nil {
			return err
		}
	}
	return nil
}

func generateChart(procName string, reportDir string, metrics []Metric) error {
	if len(metrics) < 2 {
		slog.Warn("skipping charts since insufficient metric data points present", "procName", procName)
		return nil
	}

	cpuChart, err := generateCPUChart(procName, reportDir, metrics)
	if err != nil {
		return err
	}
	rssMemChart, err := generateRssMemoryChart(procName, reportDir, metrics)
	if err != nil {
		return err
	}
	vmsMemChart, err := generateVmsMemoryChart(procName, reportDir, metrics)
	if err != nil {
		return err
	}
	chartsPath := path.Join(reportDir, fmt.Sprintf("%s-charts.html", procName))

	page := components.NewPage()
	page.AddCharts(cpuChart, rssMemChart, vmsMemChart)

	err = writePage(chartsPath, page)
	if err != nil {
		return err
	}

	return nil
}

func generateRssMemoryChart(procName string, reportDir string, metrics []Metric) (*charts.Line, error) {
	var xVals []string
	var yValsRss []opts.LineData
	for _, m := range metrics {
		xVals = append(xVals, m.ProbeTime.Format("15:04:05"))
		yValsRss = append(yValsRss, opts.LineData{Value: m.MemRSS})
	}

	line := charts.NewLine()

	line.SetGlobalOptions(
		charts.WithXAxisOpts(opts.XAxis{Name: "Time"}),
		charts.WithYAxisOpts(opts.YAxis{Name: "RSS Memory"}),
		charts.WithTitleOpts(opts.Title{
			Title: "Memory usage over time",
			//Subtitle: fmt.Sprintf("Time based over %s interval"),
		}))
	line.SetXAxis(xVals).
		AddSeries("Memory RSS", yValsRss).
		SetSeriesOptions(charts.WithLineChartOpts(opts.LineChart{Smooth: opts.Bool(true)}))

	return line, nil
}

func generateVmsMemoryChart(procName string, reportDir string, metrics []Metric) (*charts.Line, error) {
	var xVals []string
	var yValsVms []opts.LineData
	for _, m := range metrics {
		xVals = append(xVals, m.ProbeTime.Format("15:04:05"))
		yValsVms = append(yValsVms, opts.LineData{Value: m.MemVMS})
	}

	line := charts.NewLine()

	line.SetGlobalOptions(
		charts.WithXAxisOpts(opts.XAxis{Name: "Time"}),
		charts.WithYAxisOpts(opts.YAxis{Name: "VMS Memory"}),
		charts.WithTitleOpts(opts.Title{
			Title: "Memory usage over time",
			//Subtitle: fmt.Sprintf("Time based over %s interval"),
		}))
	line.SetXAxis(xVals).
		AddSeries("Memory VMS", yValsVms).
		SetSeriesOptions(charts.WithLineChartOpts(opts.LineChart{Smooth: opts.Bool(true)}))

	return line, nil
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

func generateCPUChart(procName string, reportDir string, metrics []Metric) (*charts.Line, error) {
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
			Title: "CPU usage over time",
			//Subtitle: fmt.Sprintf("Time based over %s interval"),
		}))
	line.SetXAxis(xVals).
		AddSeries("CPU", yVals).
		SetSeriesOptions(charts.WithLineChartOpts(opts.LineChart{Smooth: opts.Bool(true)}))

	return line, nil
}

func (b *basicPerfMon) handleMetricErr(err error) (ok bool) {
	if b.errCounter > b.opts.ErrThreshold {
		err = fmt.Errorf("%w: eror count exceeded threshold %d: %w", api.ErrGetMetrics, b.opts.ErrThreshold, err)
		b.errChan <- err
		ok = false
		b.cancel()
		return
	}
	b.errCounter++
	ok = true
	return
}

func (b *basicPerfMon) registerProcesses() (ok bool, err error) {
	var proc *process.Process
	ok = true
	for _, n := range b.opts.ProcessNames {
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

func (b *basicPerfMon) Stop() error {
	slog.Info("Stopping perfmon")
	if b.cancel != nil {
		b.cancel()
		return nil
	}
	return fmt.Errorf("procmon does not appear to have been started")
}

var _ api.PerfMon = (*basicPerfMon)(nil)

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

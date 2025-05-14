package core

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
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
	cfg               api.ProcessMonitorConfig
	procByName        map[string]*process.Process
	errCounter        int
	procMetricsByName map[string][]Metric
	cancel            context.CancelCauseFunc
	ioMetrics         []IOMetric
}

type Metric struct {
	ProbeTime time.Time
	MemRSS    uint64
	MemVMS    uint64
	CPU       float64
}

type IOMetric struct {
	ProbeTime time.Time
	DiskName  string
	KBt       float32
	TPS       int32
	MBs       float32
}

func (I IOMetric) String() string {
	return fmt.Sprintf("(time: %s, diskName: %s, KBt:%.2f, TPS:%d, MBs:%.2f", I.ProbeTime, I.DiskName, I.KBt, I.TPS, I.MBs)
}

func NewBasicProcessMonitor(cfg api.ProcessMonitorConfig) (api.ProcessMonitor, error) {
	pm := &basicProcessMonitor{
		cfg:               cfg,
		procByName:        make(map[string]*process.Process),
		procMetricsByName: make(map[string][]Metric),
	}
	for _, n := range cfg.ProcessNames {
		pm.procByName[n] = nil
	}
	err := os.MkdirAll(cfg.ReportDir, 0755)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", api.ErrCreateReportDir, err)
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
	reader, err := launchIostat(ctx)
	if err != nil {
		return err
	}
	go func() {
		err = parseIostatOutput(reader, b.addIOMetric)
		if err != nil {
			slog.Error("can not parse iostat output", "error", err)
			b.cancel(err)
			return
		}
	}()
	slog.Info("procmon started", "procNames", b.cfg.ProcessNames)
	//<-b.stopChan
	return nil
}

func (b *basicProcessMonitor) addIOMetric(m IOMetric) {
	b.ioMetrics = append(b.ioMetrics, m)
}

func launchIostat(ctx context.Context) (reader io.ReadCloser, err error) {
	//var buf bytes.Buffer

	command := exec.CommandContext(ctx, "iostat", "-d", "-w", "5", "-n", "1")
	//command.Stdout = &buf
	reader, err = command.StdoutPipe()
	if err != nil {
		return
	}

	slog.Info("Launching iostat")
	err = command.Start()
	if err != nil {
		err = fmt.Errorf("failed to start iostat: %w", err)
		return
	}
	//slog.Info("Waiting for 5s")
	//<-time.After(5 * time.Second)

	return
}

func parseIostatOutput(reader io.ReadCloser, addIOMetric func(metric IOMetric)) (err error) {
	scanner := bufio.NewScanner(reader)

	var m IOMetric
	for scanner.Scan() {
		text := scanner.Text()
		line := strings.TrimSpace(text)
		if strings.HasPrefix(line, "disk") {
			m.DiskName = line
			continue
		}
		if strings.HasPrefix(line, "KB/t") {
			continue
		}
		m.KBt, m.TPS, m.MBs, err = parseIostatLine(line)
		if err != nil {
			slog.Error("can not parse Iostat line", "line", line, "error", err)
			return
		}
		m.ProbeTime = time.Now()
		//slog.Info("Metric value.", "metric", m)
		addIOMetric(m)
		m = IOMetric{DiskName: m.DiskName}
	}
	return
}

func parseIostatLine(line string) (kbt float32, tps int32, mbs float32, err error) {
	//TODO: figure out how to round decimal places
	n, err := fmt.Sscanf(line, "%f %d %f", &kbt, &tps, &mbs)
	if err != nil {
		return
	}
	if n < 3 {
		err = fmt.Errorf("expected 3 items to be parsed for line: %q", line)
	}
	//kbt = RoundToTwoDecimals(kbt)
	//mbs = RoundToTwoDecimals(mbs)
	return
}

//func RoundToTwoDecimals(f float32) float32 {
//	return float32(math.Round(float64(f)*100) / 100)
//}

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
		metrics := b.procMetricsByName[n]
		if metrics == nil {
			metrics = make([]Metric, 0, 200)
		}
		metrics = append(metrics, m)
		b.procMetricsByName[n] = metrics
		slog.Debug("Metric captured", "procName", n, "CPU", m.CPU, "MemRSS", m.MemRSS, "MemVMS", m.MemVMS)
	}
	err := b.GenerateCharts()
	if err != nil {
		slog.Error("failed to generate charts", "error:", err)
	}
}

func (b *basicProcessMonitor) GenerateCharts() error {
	var pageCharts []components.Charter
	for procName, metrics := range b.procMetricsByName {
		procCharts, err := createProcessLineCharts(procName, metrics)
		if err != nil {
			return err
		}
		pageCharts = append(pageCharts, procCharts...)
	}
	if len(pageCharts) == 0 {
		return nil
	}
	cht, err := generateIOChart(b.ioMetrics)
	if err != nil {
		return err
	}
	pageCharts = append(pageCharts, cht)

	page := components.NewPage()
	page.SetPageTitle(fmt.Sprintf("%s Charts", b.cfg.ReportNamePrefix))
	page.AddCharts(pageCharts...)
	chartsPath := path.Join(b.cfg.ReportDir, fmt.Sprintf("%s-charts.html", b.cfg.ReportNamePrefix))

	err = writePage(chartsPath, page)
	if err != nil {
		return err
	}
	return nil
}

func createProcessLineCharts(procName string, metrics []Metric) (procCharts []components.Charter, err error) {
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
	slog.Debug("Generated chart", "chartPath", chartPath)
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

func generateIOChart(ioMetrics []IOMetric) (*charts.Line, error) {
	var xVals []string
	var yValsKBt []opts.LineData
	var yValsTPS []opts.LineData
	var yValsMBs []opts.LineData

	for _, m := range ioMetrics {
		xVals = append(xVals, m.ProbeTime.Format("15:04:05"))
		yValsKBt = append(yValsKBt, opts.LineData{Value: m.KBt})
		yValsTPS = append(yValsTPS, opts.LineData{Value: m.TPS})
		yValsMBs = append(yValsMBs, opts.LineData{Value: m.MBs})
	}

	line := charts.NewLine()

	line.SetGlobalOptions(
		charts.WithXAxisOpts(opts.XAxis{Name: "Time"}),
		charts.WithYAxisOpts(opts.YAxis{Name: "I/O rates"}),
		charts.WithTitleOpts(opts.Title{
			Title: "System I/O stats",
			//Subtitle: fmt.Sprintf("Time based over %s interval"),
		}))
	line.SetXAxis(xVals).
		AddSeries("KB/t", yValsKBt).
		AddSeries("tps", yValsTPS).
		AddSeries("MB/s", yValsMBs).
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

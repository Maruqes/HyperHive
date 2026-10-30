package info

import (
	"512SvMan/nots"
	"512SvMan/protocol"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	infoGrpc "github.com/Maruqes/512SvMan/api/proto/info"
	"github.com/Maruqes/512SvMan/logger"
	"google.golang.org/grpc"
)

func GetCPUInfo(conn *grpc.ClientConn, empty *infoGrpc.Empty) (*infoGrpc.CPUCoreInfo, error) {
	client := infoGrpc.NewInfoClient(conn)
	return client.GetCPUInfo(context.Background(), empty)
}

func GetMemSummary(conn *grpc.ClientConn, empty *infoGrpc.Empty) (*infoGrpc.MemSummary, error) {
	client := infoGrpc.NewInfoClient(conn)
	return client.GetMemSummary(context.Background(), empty)
}

func GetDiskSummary(conn *grpc.ClientConn, empty *infoGrpc.Empty) (*infoGrpc.DiskSummary, error) {
	client := infoGrpc.NewInfoClient(conn)
	return client.GetDiskSummary(context.Background(), empty)
}

func GetNetworkSummary(conn *grpc.ClientConn, empty *infoGrpc.Empty) (*infoGrpc.NetworkSummary, error) {
	client := infoGrpc.NewInfoClient(conn)
	return client.GetNetworkSummary(context.Background(), empty)
}

func GetProcesses(conn *grpc.ClientConn, empty *infoGrpc.Empty) (*infoGrpc.ProcessList, error) {
	client := infoGrpc.NewInfoClient(conn)
	return client.GetProcesses(context.Background(), empty)
}

func GetProcessByPID(conn *grpc.ClientConn, req *infoGrpc.ProcessPIDRequest) (*infoGrpc.ProcessStruct, error) {
	client := infoGrpc.NewInfoClient(conn)
	return client.GetProcessByPID(context.Background(), req)
}

func KillProcess(conn *grpc.ClientConn, req *infoGrpc.ProcessPIDRequest) (*infoGrpc.Ok, error) {
	client := infoGrpc.NewInfoClient(conn)
	return client.KillProcess(context.Background(), req)
}

func TerminateProcess(conn *grpc.ClientConn, req *infoGrpc.ProcessPIDRequest) (*infoGrpc.Ok, error) {
	client := infoGrpc.NewInfoClient(conn)
	return client.TerminateProcess(context.Background(), req)
}

func StressCPU(ctx context.Context, conn *grpc.ClientConn, params *infoGrpc.StressCPUParams) (*infoGrpc.Empty, error) {
	client := infoGrpc.NewInfoClient(conn)
	return client.StressCPU(ctx, params)
}

func TestRamMEM(ctx context.Context, conn *grpc.ClientConn, params *infoGrpc.TestRamMEMParams) (*infoGrpc.Ok, error) {
	client := infoGrpc.NewInfoClient(conn)
	return client.TestRamMEM(ctx, params)
}

// Metric alert thresholds. Errors push; warnings are kept in history and shown as toasts.
const (
	cpuTempWarnC   = 78
	cpuTempErrorC  = 85
	memWarnPct     = 90
	memErrorPct    = 95
	diskWarnPct    = 80
	diskErrorPct   = 90
	diskTempWarnC  = 50
	diskTempErrorC = 55

	metricsCheckInterval = 5 * time.Minute
	metricsInfoTimeout   = 30 * time.Second
)

// Two consecutive samples (about 10 minutes) make a change sustained; active alerts remind every 6 h.
var metricAlerts = nots.NewAlertEngine(nots.Notify, 2, 6*time.Hour)

func classify(value, warn, errorLimit float64) nots.AlertLevel {
	switch {
	case value >= errorLimit:
		return nots.AlertError
	case value >= warn:
		return nots.AlertWarning
	default:
		return nots.AlertOK
	}
}

func isPhysicalDisk(name string) bool {
	n := strings.TrimSpace(name)
	if n == "" || strings.HasPrefix(n, "/dev/mapper/") {
		return false
	}
	if strings.HasPrefix(n, "/dev/") {
		n = filepath.Base(n)
	}
	if n == "tmpfs" || n == "devtmpfs" {
		return false
	}
	for _, p := range []string{"dm-", "ram", "loop", "zram"} {
		if strings.HasPrefix(n, p) {
			return false
		}
	}
	return true
}

// metricSamples turns one host snapshot into alert samples: one per host for CPU and
// memory, one per physical disk for usage and temperature.
func metricSamples(machine string, cpu *infoGrpc.CPUCoreInfo, mem *infoGrpc.MemSummary, disk *infoGrpc.DiskSummary) []nots.AlertSample {
	var samples []nots.AlertSample
	add := func(event, key string, level nots.AlertLevel, title, body string) {
		samples = append(samples, nots.AlertSample{Key: event + ":" + machine + key, Event: event, Machine: machine, Level: level, Title: title, Body: body, URL: "/"})
	}

	if cpu != nil && len(cpu.Cores) > 0 {
		var maxTemp float64
		for _, core := range cpu.Cores {
			if core.Temp > maxTemp {
				maxTemp = core.Temp
			}
		}
		add("metrics.cpu_temperature", "", classify(maxTemp, cpuTempWarnC, cpuTempErrorC),
			fmt.Sprintf("%s CPU %.0f°C (max core)", machine, maxTemp),
			fmt.Sprintf("Hottest CPU core on %s is at %.0f°C (warning %d°C, error %d°C).", machine, maxTemp, cpuTempWarnC, cpuTempErrorC))
	}

	if mem != nil {
		add("metrics.memory", "", classify(mem.UsedPercent, memWarnPct, memErrorPct),
			fmt.Sprintf("%s memory %.0f%% used", machine, mem.UsedPercent),
			fmt.Sprintf("%s uses %d MB of %d MB memory.", machine, mem.UsedMb, mem.TotalMb))
	}

	if disk != nil {
		for name, used := range disk.Usage {
			if !isPhysicalDisk(name) {
				continue
			}
			add("metrics.disk_usage", ":"+name, classify(used, diskWarnPct, diskErrorPct),
				fmt.Sprintf("%s disk %s %.0f%% full", machine, name, used),
				fmt.Sprintf("Disk %s on %s is %.0f%% full (warning %d%%, error %d%%).", name, machine, used, diskWarnPct, diskErrorPct))
		}
		for _, d := range disk.Disks {
			if d == nil {
				continue
			}
			name := d.MountPoint
			if name == "" {
				name = d.Device
			}
			add("metrics.disk_temperature", ":"+name, classify(float64(d.TemperatureC), diskTempWarnC, diskTempErrorC),
				fmt.Sprintf("%s disk %s %d°C", machine, name, d.TemperatureC),
				fmt.Sprintf("Disk %s on %s is at %d°C (warning %d°C, error %d°C).", name, machine, d.TemperatureC, diskTempWarnC, diskTempErrorC))
		}
	}
	return samples
}

// LoopNots checks every connected host's metrics for the process lifetime.
func LoopNots() {
	check := func(con protocol.ConnectionsStruct) {
		if con.Connection == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), metricsInfoTimeout)
		defer cancel()
		client := infoGrpc.NewInfoClient(con.Connection)
		cpu, err := client.GetCPUInfo(ctx, &infoGrpc.Empty{})
		if err != nil {
			logger.Errorf("metrics alerts: cpu info %s: %v", con.MachineName, err)
			return
		}
		disk, err := client.GetDiskSummary(ctx, &infoGrpc.Empty{})
		if err != nil {
			logger.Errorf("metrics alerts: disk info %s: %v", con.MachineName, err)
			return
		}
		mem, err := client.GetMemSummary(ctx, &infoGrpc.Empty{})
		if err != nil {
			logger.Errorf("metrics alerts: memory info %s: %v", con.MachineName, err)
			return
		}
		for _, sample := range metricSamples(con.MachineName, cpu, mem, disk) {
			if err := metricAlerts.Observe(ctx, sample); err != nil {
				logger.Errorf("metrics alert %s: %v", sample.Key, err)
			}
		}
	}

	go func() {
		for {
			for _, con := range protocol.GetConnectionsSnapshot() {
				go check(con)
			}
			time.Sleep(metricsCheckInterval)
		}
	}()
}

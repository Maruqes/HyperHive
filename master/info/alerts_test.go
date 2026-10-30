package info

import (
	"testing"

	"512SvMan/nots"

	infoGrpc "github.com/Maruqes/512SvMan/api/proto/info"
)

func TestMetricSamplesOneCPUAlertPerHost(t *testing.T) {
	cores := []*infoGrpc.Core{}
	for i := 0; i < 16; i++ {
		cores = append(cores, &infoGrpc.Core{Temp: 80})
	}
	cores[3].Temp = 88
	samples := metricSamples("node1", &infoGrpc.CPUCoreInfo{Cores: cores}, &infoGrpc.MemSummary{UsedPercent: 50},
		&infoGrpc.DiskSummary{Usage: map[string]float64{"/dev/sda1": 91, "/dev/loop0": 100, "tmpfs": 99}})
	byEvent := map[string][]nots.AlertSample{}
	for _, s := range samples {
		byEvent[s.Event] = append(byEvent[s.Event], s)
	}
	cpu := byEvent["metrics.cpu_temperature"]
	if len(cpu) != 1 || cpu[0].Level != nots.AlertError || cpu[0].Title != "node1 CPU 88°C (max core)" || cpu[0].Key != "metrics.cpu_temperature:node1" {
		t.Fatalf("cpu: %+v", cpu)
	}
	if mem := byEvent["metrics.memory"]; len(mem) != 1 || mem[0].Level != nots.AlertOK {
		t.Fatalf("mem: %+v", mem)
	}
	if disk := byEvent["metrics.disk_usage"]; len(disk) != 1 || disk[0].Level != nots.AlertError || disk[0].Key != "metrics.disk_usage:node1:/dev/sda1" {
		t.Fatalf("disk: %+v", disk)
	}
}

func TestClassifyThresholds(t *testing.T) {
	if classify(77, cpuTempWarnC, cpuTempErrorC) != nots.AlertOK || classify(78, cpuTempWarnC, cpuTempErrorC) != nots.AlertWarning || classify(85, cpuTempWarnC, cpuTempErrorC) != nots.AlertError {
		t.Fatal("cpu thresholds")
	}
}

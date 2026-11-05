package services

import (
	"bytes"
	"encoding/csv"
	"math"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestEnergyEstimateCPUTDP(t *testing.T) {
	xeonMedian := testMedianWPerCore(t, "Xeon")
	cases := []struct {
		name    string
		sockets []EnergySocket
		want    float64
		generic bool
	}{
		{
			name:    "xeon e5-2673 v4",
			sockets: []EnergySocket{{Model: "Intel(R) Xeon(R) CPU E5-2673 v4 @ 2.30GHz", Cores: 20}},
			want:    135,
		},
		{
			name:    "xeon e5-2680 v4",
			sockets: []EnergySocket{{Model: "Intel(R) Xeon(R) CPU E5-2680 v4 @ 2.40GHz", Cores: 14}},
			want:    120,
		},
		{
			name:    "plain e5-2680 does not take v4",
			sockets: []EnergySocket{{Model: "Intel(R) Xeon(R) CPU E5-2680 0 @ 2.70GHz", Cores: 8}},
			want:    130,
		},
		{
			name:    "epyc 7763",
			sockets: []EnergySocket{{Model: "AMD EPYC 7763 64-Core Processor", Cores: 64}},
			want:    280,
		},
		{
			name:    "core i7-9700k",
			sockets: []EnergySocket{{Model: "Intel(R) Core(TM) i7-9700K CPU @ 3.60GHz", Cores: 8}},
			want:    95,
		},
		{
			name:    "unknown xeon uses brand median",
			sockets: []EnergySocket{{Model: "Intel(R) Xeon(R) CPU E5-9999 v9 @ 2.00GHz", Cores: 4}},
			want:    4 * xeonMedian,
			generic: true,
		},
		{
			name:    "unknown brand uses 8 W per core",
			sockets: []EnergySocket{{Model: "Acme Quantum CPU 9000", Cores: 4}},
			want:    32,
			generic: true,
		},
		{
			name:    "generic core suffix is not Intel Core",
			sockets: []EnergySocket{{Model: "Acme Quantum 8-Core CPU", Cores: 4}},
			want:    32,
			generic: true,
		},
		{
			name: "two sockets sum",
			sockets: []EnergySocket{
				{Model: "Intel(R) Xeon(R) CPU E5-2673 v4 @ 2.30GHz", Cores: 20},
				{Model: "Intel(R) Xeon(R) CPU E5-2673 v4 @ 2.30GHz", Cores: 20},
			},
			want: 270,
		},
		{
			name:    "short key does not match inside a longer token",
			sockets: []EnergySocket{{Model: "Intel Xeon Gold 9460X-like", Cores: 4}},
			want:    4 * xeonMedian,
			generic: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, generic := EstimateCPUTDP(tc.sockets)
			if !almostEqual(got, tc.want) {
				t.Fatalf("watts = %v, want %v", got, tc.want)
			}
			if generic != tc.generic {
				t.Fatalf("generic = %v, want %v", generic, tc.generic)
			}
		})
	}
}

func TestEnergyLookupRespectsExplicitVersions(t *testing.T) {
	ensureCPUTDPTable()
	cases := []struct {
		model     string
		wantCores int
		wantTDP   float64
	}{
		{model: "Intel(R) Xeon(R) CPU E5-2673 v2 @ 2.50GHz", wantCores: 8, wantTDP: 110},
		{model: "Intel(R) Xeon(R) CPU E5-2673 v3 @ 2.50GHz", wantCores: 12, wantTDP: 105},
		{model: "Intel(R) Xeon(R) CPU E5-2673 v4 @ 2.30GHz", wantCores: 20, wantTDP: 135},
		{model: "Intel(R) Xeon(R) CPU E5-2680 0 @ 2.70GHz", wantCores: 8, wantTDP: 130},
		{model: "Intel(R) Xeon(R) CPU E5-2680 v3 @ 2.50GHz", wantCores: 12, wantTDP: 120},
		{model: "Intel(R) Xeon(R) CPU E5-2680 v4 @ 2.40GHz", wantCores: 14, wantTDP: 120},
	}
	for _, tc := range cases {
		row, ok := lookupCPURow(tc.model, brandForModel(tc.model))
		if !ok {
			t.Fatalf("%s: no row", tc.model)
		}
		if row.cores != tc.wantCores || !almostEqual(row.tdpW, tc.wantTDP) {
			t.Fatalf("%s: got cores=%d tdp=%v, want cores=%d tdp=%v", tc.model, row.cores, row.tdpW, tc.wantCores, tc.wantTDP)
		}
	}
}

func TestEnergyEstimateHostWatts(t *testing.T) {
	socket := EnergySocket{Model: "Intel(R) Xeon(R) CPU E5-2673 v4 @ 2.30GHz", Cores: 20}

	idle, load, generic := EstimateHostWatts(EnergyHardware{
		Sockets: []EnergySocket{socket},
		RAMMB:   65536,
		HDDs:    2,
		SSDs:    3,
		NVMes:   1,
	}, 0.5)
	if !almostEqual(idle, 103.85) {
		t.Fatalf("idle = %v, want 103.85", idle)
	}
	if !almostEqual(load, 57.375) {
		t.Fatalf("load = %v, want 57.375", load)
	}
	if generic {
		t.Fatal("known CPU reported as generic")
	}

	idle, load, generic = EstimateHostWatts(EnergyHardware{
		Sockets: []EnergySocket{socket},
	}, 0)
	if !almostEqual(idle, 50.25) {
		t.Fatalf("zero-disk idle = %v, want 50.25", idle)
	}
	if load != 0 {
		t.Fatalf("zero load = %v", load)
	}
	if generic {
		t.Fatal("known CPU reported as generic")
	}
}

func TestEnergyMeanCPUUsageFraction(t *testing.T) {
	cases := []struct {
		name    string
		percent []float64
		want    float64
	}{
		{name: "empty", percent: nil, want: 0},
		{name: "half", percent: []float64{50, 50}, want: 0.5},
		{name: "mixed", percent: []float64{0, 100}, want: 0.5},
		{name: "clamped high", percent: []float64{-10, 250}, want: 1},
		{name: "clamped low", percent: []float64{-20, -40}, want: 0},
		{name: "above full", percent: []float64{150, 150}, want: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MeanCPUUsageFraction(tc.percent); !almostEqual(got, tc.want) {
				t.Fatalf("fraction = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEnergySplitVMWatts(t *testing.T) {
	vms := []EnergyVMLoad{
		{Name: "a", CPUUsage: 50, CPUCount: 2, Running: true},
		{Name: "b", CPUUsage: 25, CPUCount: 4, Running: true},
		{Name: "stopped", CPUUsage: 100, CPUCount: 8, Running: false},
		{Name: "idle", CPUUsage: 0, CPUCount: 4, Running: true},
	}
	got := SplitVMWatts(100, vms)
	want := map[string]float64{"a": 50, "b": 50, "stopped": 0, "idle": 0}
	for name, watts := range want {
		if value, ok := got[name]; !ok || !almostEqual(value, watts) {
			t.Fatalf("%s = %v (present %v), want %v", name, value, ok, watts)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("result = %v", got)
	}
	var total float64
	for _, watts := range got {
		total += watts
	}
	if !almostEqual(total, 100) {
		t.Fatalf("total = %v, want 100", total)
	}
}

func TestEnergySplitVMWattsZeroLoad(t *testing.T) {
	vms := []EnergyVMLoad{
		{Name: "running-zero", CPUUsage: 0, CPUCount: 4, Running: true},
		{Name: "stopped", CPUUsage: 100, CPUCount: 8, Running: false},
	}
	got := SplitVMWatts(100, vms)
	if len(got) != len(vms) {
		t.Fatalf("result = %v", got)
	}
	for name, watts := range got {
		if watts != 0 {
			t.Fatalf("%s = %v, want 0", name, watts)
		}
	}
}

func testMedianWPerCore(t *testing.T, family string) float64 {
	t.Helper()
	records, err := csv.NewReader(bytes.NewReader(cpuTDPCSV)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	var values []float64
	for _, record := range records[1:] {
		if len(record) < energyCSVExpectedFields || !strings.Contains(record[energyCSVFamilyColumn], family) {
			continue
		}
		cores, err := strconv.Atoi(strings.TrimSpace(record[energyCSVCoresColumn]))
		if err != nil {
			t.Fatal(err)
		}
		tdp, err := strconv.ParseFloat(strings.TrimSpace(record[energyCSVTDPColumn]), 64)
		if err != nil {
			t.Fatal(err)
		}
		if cores > 0 {
			values = append(values, tdp/float64(cores))
		}
	}
	if len(values) == 0 {
		t.Fatalf("no rows for family %q", family)
	}
	sort.Float64s(values)
	middle := len(values) / 2
	if len(values)%2 == 1 {
		return values[middle]
	}
	return (values[middle-1] + values[middle]) / 2
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestEnergyFullLoadOnBothSocketsRaisesHostAndVMWatts(t *testing.T) {
	socket := EnergySocket{Model: "Intel(R) Xeon(R) CPU E5-2673 v4 @ 2.30GHz", Cores: 20}
	hw := EnergyHardware{Sockets: []EnergySocket{socket, socket}}
	vms := []EnergyVMLoad{{Name: "busy", CPUUsage: 100, CPUCount: 40, Running: true}}

	idleW, idleLoad, _ := EstimateHostWatts(hw, MeanCPUUsageFraction([]float64{0, 0}))
	_, fullLoad, _ := EstimateHostWatts(hw, MeanCPUUsageFraction([]float64{100, 100}))

	if idleLoad != 0 {
		t.Fatalf("idle load = %v", idleLoad)
	}
	if !almostEqual(fullLoad, 229.5) {
		t.Fatalf("full load = %v, want 229.5", fullLoad)
	}
	if !almostEqual(idleW+fullLoad, 300) {
		t.Fatalf("full host = %v, want 300", idleW+fullLoad)
	}
	if got := SplitVMWatts(fullLoad, vms)["busy"]; !almostEqual(got, 229.5) {
		t.Fatalf("vm watts = %v, want 229.5", got)
	}
}

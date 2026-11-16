package services

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"testing"
	"time"

	"512SvMan/db"
	"512SvMan/protocol"

	infoGrpc "github.com/Maruqes/512SvMan/api/proto/info"
	virshGrpc "github.com/Maruqes/512SvMan/api/proto/virsh"
	"google.golang.org/grpc"
)

func openEnergyServiceDB(t *testing.T) {
	t.Helper()
	previous := db.DB
	fixture, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	fixture.SetMaxOpenConns(1)
	db.DB = fixture
	t.Cleanup(func() {
		fixture.Close()
		db.DB = previous
	})
	if err := db.CreateEnergyTables(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type fakeEnergySlave struct {
	cpu         *infoGrpc.CPUCoreInfo
	cpuErr      error
	hardware    *infoGrpc.PowerHardware
	hardwareErr error
	vms         *virshGrpc.GetAllVmsResponse
	vmsErr      error
}

func fakeEnergyService(now time.Time, slaves map[string]*fakeEnergySlave) *EnergyService {
	connections := make([]protocol.ConnectionsStruct, 0, len(slaves))
	for name := range slaves {
		connections = append(connections, protocol.ConnectionsStruct{
			MachineName: name,
			Connection:  &grpc.ClientConn{},
		})
	}
	service := NewEnergyService()
	service.Now = func() time.Time { return now }
	service.Readers = EnergyReaders{
		Connections: func() []protocol.ConnectionsStruct { return connections },
		CPU: func(_ context.Context, con protocol.ConnectionsStruct) (*infoGrpc.CPUCoreInfo, error) {
			slave := slaves[con.MachineName]
			if slave == nil {
				return nil, errors.New("missing fake slave")
			}
			return slave.cpu, slave.cpuErr
		},
		Hardware: func(_ context.Context, con protocol.ConnectionsStruct) (*infoGrpc.PowerHardware, error) {
			slave := slaves[con.MachineName]
			if slave == nil {
				return nil, errors.New("missing fake slave")
			}
			return slave.hardware, slave.hardwareErr
		},
		VMs: func(_ context.Context, con protocol.ConnectionsStruct) (*virshGrpc.GetAllVmsResponse, error) {
			slave := slaves[con.MachineName]
			if slave == nil {
				return nil, errors.New("missing fake slave")
			}
			return slave.vms, slave.vmsErr
		},
	}
	return service
}

func energyTestHardware() *infoGrpc.PowerHardware {
	return &infoGrpc.PowerHardware{
		Sockets: []*infoGrpc.CpuSocket{{Model: "AMD EPYC 7302 16-Core Processor", Cores: 16}},
		RamMb:   32768,
		Disks:   []*infoGrpc.PowerDisk{{Name: "sda", Kind: "ssd"}},
	}
}

func energyTestCPU(usage float64) *infoGrpc.CPUCoreInfo {
	return &infoGrpc.CPUCoreInfo{Cores: []*infoGrpc.Core{{Usage: usage}, {Usage: usage}}}
}

func energyRowsByMachineAndVM(t *testing.T, month string) map[string]map[string]db.EnergyUsageRow {
	t.Helper()
	rows, err := db.GetEnergyUsageForMonth(context.Background(), month)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]map[string]db.EnergyUsageRow)
	for _, row := range rows {
		if out[row.Machine] == nil {
			out[row.Machine] = make(map[string]db.EnergyUsageRow)
		}
		out[row.Machine][row.VM] = row
	}
	return out
}

func energyAlmostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestEnergyTickSkipsFailedSlaveAndKeepsLiveState(t *testing.T) {
	openEnergyServiceDB(t)
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.Local)
	service := fakeEnergyService(now, map[string]*fakeEnergySlave{
		"node-a": {
			cpu:      energyTestCPU(50),
			hardware: energyTestHardware(),
			vms: &virshGrpc.GetAllVmsResponse{Vms: []*virshGrpc.Vm{
				{Name: "vm1", State: virshGrpc.VmState_RUNNING, CpuCount: 4, CurrentCpuUsage: 50},
				{Name: "vm2", State: virshGrpc.VmState_SHUTOFF, CpuCount: 4},
			}},
		},
		"node-b": {cpuErr: errors.New("slave is down")},
	})

	service.Tick(context.Background())

	rows := energyRowsByMachineAndVM(t, "2026-11")
	if _, ok := rows["node-b"]; ok {
		t.Fatalf("failed slave wrote rows: %+v", rows["node-b"])
	}
	hostRow, ok := rows["node-a"][""]
	if !ok {
		t.Fatalf("missing host row: %+v", rows["node-a"])
	}
	_, idleW, loadW, _ := energyExpectedHost(0.5)
	wantHostWh := (idleW + loadW) * energyLoopInterval.Hours()
	if !energyAlmostEqual(hostRow.Wh, wantHostWh) {
		t.Fatalf("host wh = %v, want %v", hostRow.Wh, wantHostWh)
	}
	if hostRow.Cost != 0 {
		t.Fatalf("cost without price = %v", hostRow.Cost)
	}

	if _, ok := rows["node-a"]["vm2"]; ok {
		t.Fatal("stopped VM wrote a row")
	}
	vmRow, ok := rows["node-a"]["vm1"]
	if !ok || !energyAlmostEqual(vmRow.Wh, loadW*energyLoopInterval.Hours()) {
		t.Fatalf("running VM row = %+v, want %v", vmRow, loadW*energyLoopInterval.Hours())
	}

	live := service.liveSnapshot()
	if _, ok := live["node-b"]; ok {
		t.Fatal("failed slave stayed in live state")
	}
	machine, ok := live["node-a"]
	if !ok || !energyAlmostEqual(machine.hostW, idleW+loadW) {
		t.Fatalf("live machine = %+v", machine)
	}
	if !energyAlmostEqual(machine.vms["vm1"], loadW) || machine.vms["vm2"] != 0 {
		t.Fatalf("live VMs = %+v", machine.vms)
	}
}

func energyExpectedHost(hostCPU float64) (EnergyHardware, float64, float64, bool) {
	hardware, _ := energyHardwareFromProto(energyTestHardware())
	idleW, loadW, generic := EstimateHostWatts(hardware, hostCPU)
	return hardware, idleW, loadW, generic
}

func TestEnergyTickCostFollowsPrice(t *testing.T) {
	openEnergyServiceDB(t)
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.Local)
	service := fakeEnergyService(now, map[string]*fakeEnergySlave{
		"node-a": {
			cpu:      energyTestCPU(25),
			hardware: energyTestHardware(),
			vms:      &virshGrpc.GetAllVmsResponse{},
		},
	})
	ctx := context.Background()

	service.Tick(ctx)
	_, idleW, loadW, _ := energyExpectedHost(0.25)
	hostWh := (idleW + loadW) * energyLoopInterval.Hours()
	rows := energyRowsByMachineAndVM(t, "2026-11")
	if rows["node-a"][""].Cost != 0 {
		t.Fatalf("unset price cost = %v", rows["node-a"][""].Cost)
	}

	if err := db.SetEnergyPrice(ctx, 0.25); err != nil {
		t.Fatal(err)
	}
	service.Tick(ctx)

	rows = energyRowsByMachineAndVM(t, "2026-11")
	row := rows["node-a"][""]
	if !energyAlmostEqual(row.Wh, 2*hostWh) {
		t.Fatalf("accumulated wh = %v, want %v", row.Wh, 2*hostWh)
	}
	wantCost := hostWh / 1000 * 0.25
	if !energyAlmostEqual(row.Cost, wantCost) {
		t.Fatalf("accumulated cost = %v, want %v", row.Cost, wantCost)
	}

	summary, err := service.Summary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if summary.PricePerKWh == nil || *summary.PricePerKWh != 0.25 {
		t.Fatalf("summary price = %v", summary.PricePerKWh)
	}
}

func TestEnergySummarySumsMovedVMByName(t *testing.T) {
	openEnergyServiceDB(t)
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.Local)
	ctx := context.Background()
	service := fakeEnergyService(now, map[string]*fakeEnergySlave{
		"node-a": {
			cpu:      energyTestCPU(50),
			hardware: energyTestHardware(),
			vms: &virshGrpc.GetAllVmsResponse{Vms: []*virshGrpc.Vm{
				{Name: "moved", State: virshGrpc.VmState_RUNNING, CpuCount: 4, CurrentCpuUsage: 50},
			}},
		},
		"node-b": {
			cpu:      energyTestCPU(50),
			hardware: energyTestHardware(),
			vms: &virshGrpc.GetAllVmsResponse{Vms: []*virshGrpc.Vm{
				{Name: "moved", State: virshGrpc.VmState_RUNNING, CpuCount: 4, CurrentCpuUsage: 50},
			}},
		},
	})
	if err := db.AddEnergyUsage(ctx, "2026-11", "node-c", "moved", 100, 0.2); err != nil {
		t.Fatal(err)
	}

	service.Tick(ctx)

	_, _, loadW, _ := energyExpectedHost(0.5)
	summary, err := service.Summary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.VMs) != 1 || summary.VMs[0].Name != "moved" {
		t.Fatalf("vms = %+v", summary.VMs)
	}
	if !energyAlmostEqual(summary.VMs[0].LiveWatts, 2*loadW) {
		t.Fatalf("live watts = %v, want %v", summary.VMs[0].LiveWatts, 2*loadW)
	}
	wantKWh := (2 * (loadW * energyLoopInterval.Hours()) / 1000) + 0.1
	if !energyAlmostEqual(summary.VMs[0].MonthKWh, wantKWh) {
		t.Fatalf("month kWh = %v, want %v", summary.VMs[0].MonthKWh, wantKWh)
	}
	if !energyAlmostEqual(summary.VMs[0].MonthCost, 0.2) {
		t.Fatalf("month cost = %v", summary.VMs[0].MonthCost)
	}

	if len(summary.Machines) != 2 {
		t.Fatalf("machines = %+v, want only live hosts", summary.Machines)
	}
	if summary.Machines[0].Machine != "node-a" || summary.Machines[1].Machine != "node-b" {
		t.Fatalf("machine order = %+v", summary.Machines)
	}
	if summary.Machines[0].CPUEstimate == "" || summary.Totals.LiveWatts != 2*(idleWFrom(0.5)+loadW) {
		t.Fatalf("machine summary = %+v totals = %+v", summary.Machines[0], summary.Totals)
	}
	if !energyAlmostEqual(summary.Totals.MonthKWh, summary.Machines[0].MonthKWh+summary.Machines[1].MonthKWh) {
		t.Fatalf("totals = %+v", summary.Totals)
	}
}

func idleWFrom(hostCPU float64) float64 {
	_, idleW, _, _ := energyExpectedHost(hostCPU)
	return idleW
}

func TestEnergyRunLoopStopsOnCancel(t *testing.T) {
	openEnergyServiceDB(t)
	service := NewEnergyService()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		service.RunLoop(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunLoop did not return after cancel")
	}
}

func TestEnergyTickHours(t *testing.T) {
	start := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		last time.Time
		now  time.Time
		want time.Duration
	}{
		{"first tick", time.Time{}, start, energyLoopInterval},
		{"clock did not advance", start, start, energyLoopInterval},
		{"slow tick counts real gap", start, start.Add(25 * time.Second), 25 * time.Second},
		{"downtime is capped", start, start.Add(time.Hour), energyMaxTickGap},
	}
	for _, tc := range cases {
		if got := energyTickHours(tc.last, tc.now); !energyAlmostEqual(got, tc.want.Hours()) {
			t.Fatalf("%s: hours = %v, want %v", tc.name, got, tc.want.Hours())
		}
	}
}

func TestEnergySummaryMonthsCoverLastTwelve(t *testing.T) {
	openEnergyServiceDB(t)
	now := time.Date(2026, 3, 31, 12, 0, 0, 0, time.Local)
	ctx := context.Background()
	service := fakeEnergyService(now, map[string]*fakeEnergySlave{})
	for _, row := range []struct {
		month, machine, vm string
		wh, cost           float64
	}{
		{"2025-03", "node-a", "", 9000, 9}, // 13 months back: outside the window
		{"2025-04", "node-a", "", 1000, 0.2},
		{"2025-04", "node-b", "", 500, 0.1},
		{"2025-04", "node-a", "vm", 400, 0.08}, // VM rows are part of the host row
		{"2026-03", "node-a", "", 2000, 0.5},
	} {
		if err := db.AddEnergyUsage(ctx, row.month, row.machine, row.vm, row.wh, row.cost); err != nil {
			t.Fatal(err)
		}
	}

	summary, err := service.Summary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	months := summary.Months
	if len(months) != 12 || months[0].Month != "2025-04" || months[10].Month != "2026-02" || months[11].Month != "2026-03" {
		t.Fatalf("months = %+v", months)
	}
	if !energyAlmostEqual(months[0].KWh, 1.5) || !energyAlmostEqual(months[0].Cost, 0.3) {
		t.Fatalf("first month = %+v", months[0])
	}
	if months[10].KWh != 0 || !energyAlmostEqual(months[11].KWh, 2) || !energyAlmostEqual(months[11].Cost, 0.5) {
		t.Fatalf("last months = %+v %+v", months[10], months[11])
	}
}

func TestEnergyTickWritesSamplesAndRetention(t *testing.T) {
	openEnergyServiceDB(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	service := fakeEnergyService(now, map[string]*fakeEnergySlave{
		"node-a": {
			cpu:      energyTestCPU(50),
			hardware: energyTestHardware(),
			vms: &virshGrpc.GetAllVmsResponse{Vms: []*virshGrpc.Vm{
				{Name: "vm1", State: virshGrpc.VmState_RUNNING, CpuCount: 4, CurrentCpuUsage: 50},
				{Name: "vm2", State: virshGrpc.VmState_SHUTOFF, CpuCount: 4},
			}},
		},
	})
	// One row just inside and one just outside each tier's retention.
	for _, tier := range energySampleTiers {
		for _, age := range []time.Duration{tier.keep - time.Hour, tier.keep + 2*time.Hour} {
			if _, err := db.DB.Exec(`INSERT INTO energy_samples (step, ts, kind, machine, name, wh, running_s) VALUES (?, ?, 'host', 'old', '', 1, 1)`,
				tier.step, now.Add(-age).Unix()); err != nil {
				t.Fatal(err)
			}
		}
	}

	service.Tick(ctx)

	_, idleW, loadW, _ := energyExpectedHost(0.5)
	for _, tier := range energySampleTiers {
		rows, err := db.GetEnergySamples(ctx, tier.step, 0, "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]db.EnergySample{}
		for _, row := range rows {
			got[row.Kind+"/"+row.Machine+"/"+row.Name] = row
		}
		if len(got) != 3 {
			t.Fatalf("step %d rows = %+v, want kept old row, host and running VM", tier.step, rows)
		}
		host, vm := got["host/node-a/"], got["vm/node-a/vm1"]
		if host.Ts != now.Unix() || host.RunningS != 10 || !energyAlmostEqual(host.Wh, (idleW+loadW)*energyLoopInterval.Hours()) {
			t.Fatalf("step %d host = %+v", tier.step, host)
		}
		if vm.RunningS != 10 || !energyAlmostEqual(vm.Wh, loadW*energyLoopInterval.Hours()) {
			t.Fatalf("step %d vm = %+v", tier.step, vm)
		}
	}
}

func TestEnergyTimelineGapsAndMerge(t *testing.T) {
	openEnergyServiceDB(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 5, 12, 20, 0, 0, time.UTC)
	service := fakeEnergyService(now, nil)
	hour := now.Unix() - now.Unix()%3600
	if err := db.AddEnergySamples(ctx, []db.EnergySample{
		// Two hours ago: both hosts up, "moved" runs on both, "idle" is stopped.
		{Ts: hour - 7200, Kind: "host", Machine: "node-a", Wh: 100, RunningS: 3600},
		{Ts: hour - 7200, Kind: "host", Machine: "node-b", Wh: 50, RunningS: 3600},
		{Ts: hour - 7200, Kind: "vm", Machine: "node-a", Name: "moved", Wh: 10, RunningS: 1800},
		{Ts: hour - 7200, Kind: "vm", Machine: "node-b", Name: "moved", Wh: 5, RunningS: 1800},
		// Last hour: nothing (master down). This hour: only "idle" runs.
		{Ts: hour, Kind: "host", Machine: "node-a", Wh: 20, RunningS: 600},
		{Ts: hour, Kind: "vm", Machine: "node-a", Name: "idle", Wh: 2, RunningS: 600},
	}); err != nil {
		t.Fatal(err)
	}

	overview, err := service.Timeline(ctx, "6m", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if overview.StepSeconds != 3600 || overview.Count != 183*24 || overview.Start+int64(overview.Count-1)*3600 != hour {
		t.Fatalf("overview = %+v", overview)
	}
	names := []string{}
	byName := map[string]EnergyTimelineSeries{}
	for _, series := range overview.Series {
		if len(series.Wh) != overview.Count || len(series.RunningS) != overview.Count {
			t.Fatalf("%s has %d values, want %d", series.Name, len(series.Wh), overview.Count)
		}
		names = append(names, series.Kind+":"+series.Name)
		byName[series.Name] = series
	}
	if len(names) != 4 || names[0] != "host:node-a" || names[1] != "host:node-b" || names[2] != "vm:idle" || names[3] != "vm:moved" {
		t.Fatalf("series = %v", names)
	}
	last := overview.Count - 1
	moved, idle := byName["moved"], byName["idle"]
	if moved.Machine != "" || *moved.Wh[last-2] != 15 || *moved.RunningS[last-2] != 3600 {
		t.Fatalf("merged vm = %v %v", *moved.Wh[last-2], *moved.RunningS[last-2])
	}
	if moved.Wh[last-1] != nil || idle.Wh[last-1] != nil || byName["node-a"].Wh[last-1] != nil {
		t.Fatal("hour with no host row is not null")
	}
	if moved.Wh[last] == nil || *moved.Wh[last] != 0 || *idle.Wh[last-2] != 0 {
		t.Fatal("stopped VM bucket is not 0")
	}
	if byName["node-b"].Wh[last] != nil {
		t.Fatal("host without a row is not null")
	}

	one, err := service.Timeline(ctx, "30d", "host", "", "node-a")
	if err != nil {
		t.Fatal(err)
	}
	if one.StepSeconds != 300 || one.Count != 30*24*12 || len(one.Series) != 1 || one.Series[0].Name != "node-a" {
		t.Fatalf("single series = %+v", one)
	}
	for _, bad := range [][3]string{{"1y", "", ""}, {"30d", "disk", ""}, {"30d", "", "vm1"}} {
		if _, err := service.Timeline(ctx, bad[0], bad[1], "", bad[2]); !errors.Is(err, ErrEnergyTimelineInput) {
			t.Fatalf("Timeline(%v) error = %v", bad, err)
		}
	}
}

func TestEnergySummaryProjection(t *testing.T) {
	// 100 Wh per hour for the last 48 hours on node-a, a tenth of it on vm1.
	seed := func(t *testing.T, now time.Time, hours int) {
		t.Helper()
		ctx := context.Background()
		hour := now.Unix() - now.Unix()%3600
		for i := 1; i <= hours; i++ {
			if err := db.AddEnergySamples(ctx, []db.EnergySample{
				{Ts: hour - int64(i)*3600, Kind: "host", Machine: "node-a", Wh: 100, RunningS: 3600},
				{Ts: hour - int64(i)*3600, Kind: "vm", Machine: "node-a", Name: "vm1", Wh: 10, RunningS: 3600},
			}); err != nil {
				t.Fatal(err)
			}
		}
		if err := db.AddEnergyUsage(ctx, "2026-11", "node-a", "", 5000, 1); err != nil {
			t.Fatal(err)
		}
		if err := db.AddEnergyUsage(ctx, "2026-11", "node-a", "vm1", 500, 0.1); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name      string
		now       time.Time
		hours     int
		price     float64
		wantKWh   float64 // node-a and totals
		wantCost  float64
		wantVMKWh float64
	}{
		// 100 W for the 384 h left of November.
		{"mid month", time.Date(2026, 11, 15, 0, 0, 0, 0, time.Local), 48, 0.25, 5 + 38.4, 1 + 38.4*0.25, 0.5 + 3.84},
		{"no price", time.Date(2026, 11, 15, 0, 0, 0, 0, time.Local), 48, -1, 5 + 38.4, 1, 0.5 + 3.84},
		{"no samples", time.Date(2026, 11, 15, 0, 0, 0, 0, time.Local), 0, 0.25, 5, 1, 0.5},
		{"last hour", time.Date(2026, 11, 30, 23, 30, 0, 0, time.Local), 48, 0.25, 5, 1, 0.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			openEnergyServiceDB(t)
			ctx := context.Background()
			seed(t, tc.now, tc.hours)
			if tc.price >= 0 {
				if err := db.SetEnergyPrice(ctx, tc.price); err != nil {
					t.Fatal(err)
				}
			}
			summary, err := fakeEnergyService(tc.now, nil).Summary(ctx)
			if err != nil {
				t.Fatal(err)
			}
			machine, vm, totals := summary.Machines[0], summary.VMs[0], summary.Totals
			near := func(a, b float64) bool { return math.Abs(a-b) < 1e-6 }
			if !near(machine.ProjectedMonthKWh, tc.wantKWh) || !near(machine.ProjectedMonthCost, tc.wantCost) {
				t.Fatalf("machine = %+v, want %v kWh, %v cost", machine, tc.wantKWh, tc.wantCost)
			}
			// Totals come from host rows only; the VM is never added again.
			if !near(totals.ProjectedMonthKWh, tc.wantKWh) || !near(totals.ProjectedMonthCost, tc.wantCost) {
				t.Fatalf("totals = %+v", totals)
			}
			if !near(vm.ProjectedMonthKWh, tc.wantVMKWh) {
				t.Fatalf("vm = %+v, want %v kWh", vm, tc.wantVMKWh)
			}
			if machine.MonthKWh != 5 || totals.MonthKWh != 5 || vm.MonthKWh != 0.5 {
				t.Fatalf("month to date changed: %+v %+v %+v", machine, totals, vm)
			}
		})
	}
}

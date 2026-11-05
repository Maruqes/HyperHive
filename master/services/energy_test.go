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
	wantHostWh := (idleW + loadW) / 60
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
	if !ok || !energyAlmostEqual(vmRow.Wh, loadW/60) {
		t.Fatalf("running VM row = %+v, want %v", vmRow, loadW/60)
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
	hostWh := (idleW + loadW) / 60
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
	wantKWh := (2 * (loadW / 60) / 1000) + 0.1
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

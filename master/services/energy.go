package services

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"512SvMan/db"
	"512SvMan/info"
	"512SvMan/protocol"
	"512SvMan/virsh"

	infoGrpc "github.com/Maruqes/512SvMan/api/proto/info"
	virshGrpc "github.com/Maruqes/512SvMan/api/proto/virsh"
	"github.com/Maruqes/512SvMan/logger"
)

const (
	energyLoopInterval       = 10 * time.Second
	energyMaxTickGap         = time.Minute
	energySlaveTimeout       = 45 * time.Second
	energyHardwareTTL        = time.Hour
	energyWhPerKWh           = 1000.0
	energyMonthFormat        = "2006-01"
	energyRetentionMonths    = 24
	energyHistoryMonths      = 12
	energyEstimateFromTable  = "table"
	energyEstimateFromFallbk = "generic"
)

// EnergyReaders is the per-host input of one energy tick. Replacing it makes a
// tick testable without gRPC or real slaves.
type EnergyReaders struct {
	Connections func() []protocol.ConnectionsStruct
	CPU         func(ctx context.Context, con protocol.ConnectionsStruct) (*infoGrpc.CPUCoreInfo, error)
	Hardware    func(ctx context.Context, con protocol.ConnectionsStruct) (*infoGrpc.PowerHardware, error)
	VMs         func(ctx context.Context, con protocol.ConnectionsStruct) (*virshGrpc.GetAllVmsResponse, error)
}

type energyLiveMachine struct {
	hostW       float64
	cpuEstimate string
	vms         map[string]float64
}

type energyHardwareCache struct {
	hardware  EnergyHardware
	fetchedAt time.Time
}

type energyTickResult struct {
	machine string
	live    energyLiveMachine
	hostWh  float64
	vmRows  map[string]float64
	ok      bool
}

// EnergyService samples the connected hosts every few seconds, stores the estimated
// host and VM energy and keeps a live snapshot for the summary endpoint.
type EnergyService struct {
	Readers EnergyReaders
	Now     func() time.Time

	mu             sync.Mutex
	machines       map[string]energyLiveMachine
	hardware       map[string]energyHardwareCache
	ticking        bool
	retentionMonth string
	lastTick       time.Time
}

// Energy is the process-wide instance shared by main and the HTTP API.
var Energy = NewEnergyService()

// NewEnergyService wires the service to the live slaves and the local clock.
func NewEnergyService() *EnergyService {
	return &EnergyService{
		Readers:  defaultEnergyReaders(),
		Now:      time.Now,
		machines: make(map[string]energyLiveMachine),
		hardware: make(map[string]energyHardwareCache),
	}
}

func defaultEnergyReaders() EnergyReaders {
	return EnergyReaders{
		Connections: protocol.GetConnectionsSnapshot,
		CPU: func(ctx context.Context, con protocol.ConnectionsStruct) (*infoGrpc.CPUCoreInfo, error) {
			return info.GetCPUInfoCtx(ctx, con.Connection, &infoGrpc.Empty{})
		},
		Hardware: func(ctx context.Context, con protocol.ConnectionsStruct) (*infoGrpc.PowerHardware, error) {
			return info.GetPowerHardware(ctx, con.Connection)
		},
		VMs: func(ctx context.Context, con protocol.ConnectionsStruct) (*virshGrpc.GetAllVmsResponse, error) {
			return virsh.GetAllVms(ctx, con.Connection, &virshGrpc.Empty{})
		},
	}
}

// RunLoop ticks every energyLoopInterval until ctx ends. A tick is synchronous, so ticks
// never overlap and a slow host is bounded by its own timeout.
func (s *EnergyService) RunLoop(ctx context.Context) {
	ticker := time.NewTicker(energyLoopInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Tick(ctx)
		}
	}
}

// Tick records one sample for every connected slave. A slave that fails any
// read is skipped without affecting the others.
func (s *EnergyService) Tick(ctx context.Context) {
	if !s.beginTick() {
		logger.Debug("energy: previous tick still running, skipping")
		return
	}
	defer s.endTick()

	now := s.now()
	month := now.Format(energyMonthFormat)
	hours := energyTickHours(s.lastTick, now)
	s.lastTick = now

	price, priceSet, err := db.GetEnergyPrice(ctx)
	if err != nil {
		logger.Errorf("energy: read price: %v", err)
		price, priceSet = 0, false
	}

	s.applyRetention(ctx, now)

	connections := s.Readers.Connections()
	results := make(chan energyTickResult, len(connections))
	var wg sync.WaitGroup
	for _, con := range connections {
		if con.Connection == nil {
			continue
		}
		wg.Add(1)
		go func(con protocol.ConnectionsStruct) {
			defer wg.Done()
			results <- s.readMachine(ctx, con, now, hours)
		}(con)
	}
	wg.Wait()
	close(results)

	live := make(map[string]energyLiveMachine)
	for result := range results {
		if !result.ok {
			continue
		}
		live[result.machine] = result.live
		if err := db.AddEnergyUsage(ctx, month, result.machine, "", result.hostWh, energyCost(result.hostWh, price, priceSet)); err != nil {
			logger.Errorf("energy: save host usage %s: %v", result.machine, err)
		}
		for name, wh := range result.vmRows {
			if err := db.AddEnergyUsage(ctx, month, result.machine, name, wh, energyCost(wh, price, priceSet)); err != nil {
				logger.Errorf("energy: save vm usage %s/%s: %v", result.machine, name, err)
			}
		}
	}
	s.setLive(live)
}

func (s *EnergyService) readMachine(ctx context.Context, con protocol.ConnectionsStruct, now time.Time, hours float64) (result energyTickResult) {
	ctx, cancel := context.WithTimeout(ctx, energySlaveTimeout)
	defer cancel()

	cpu, err := s.Readers.CPU(ctx, con)
	if err != nil {
		logger.Debugf("energy: cpu info %s: %v", con.MachineName, err)
		return result
	}
	vms, err := s.Readers.VMs(ctx, con)
	if err != nil {
		logger.Debugf("energy: list vms %s: %v", con.MachineName, err)
		return result
	}
	hardware, ok := s.hardwareFor(ctx, con, now)
	if !ok {
		return result
	}

	perCore := make([]float64, 0, len(cpu.GetCores()))
	for _, core := range cpu.GetCores() {
		if core == nil {
			continue
		}
		perCore = append(perCore, core.GetUsage())
	}
	hostCPU := MeanCPUUsageFraction(perCore)

	loads := make([]EnergyVMLoad, 0, len(vms.GetVms()))
	for _, vm := range vms.GetVms() {
		if vm == nil {
			continue
		}
		loads = append(loads, EnergyVMLoad{
			Name:     vm.GetName(),
			CPUUsage: float64(vm.GetCurrentCpuUsage()),
			CPUCount: int(vm.GetCpuCount()),
			Running:  vm.GetState() == virshGrpc.VmState_RUNNING,
		})
	}

	idleW, loadW, generic := EstimateHostWatts(hardware, hostCPU)
	hostW := idleW + loadW
	vmWatts := SplitVMWatts(loadW, loads)

	estimate := energyEstimateFromTable
	if generic {
		estimate = energyEstimateFromFallbk
	}
	liveVMs := make(map[string]float64, len(loads))
	vmRows := make(map[string]float64, len(loads))
	for _, vm := range loads {
		watts := vmWatts[vm.Name]
		liveVMs[vm.Name] = watts
		if vm.Running {
			vmRows[vm.Name] = watts * hours
		}
	}

	return energyTickResult{
		machine: con.MachineName,
		live: energyLiveMachine{
			hostW:       hostW,
			cpuEstimate: estimate,
			vms:         liveVMs,
		},
		hostWh: hostW * hours,
		vmRows: vmRows,
		ok:     true,
	}
}

func (s *EnergyService) hardwareFor(ctx context.Context, con protocol.ConnectionsStruct, now time.Time) (EnergyHardware, bool) {
	if hardware, ok := s.cachedHardware(con.MachineName, now); ok {
		return hardware, true
	}
	hardwareProto, err := s.Readers.Hardware(ctx, con)
	if err != nil {
		logger.Debugf("energy: hardware %s: %v", con.MachineName, err)
		s.forgetHardware(con.MachineName)
		return EnergyHardware{}, false
	}
	hardware, ok := energyHardwareFromProto(hardwareProto)
	if !ok {
		logger.Debugf("energy: hardware %s: no CPU sockets", con.MachineName)
		s.forgetHardware(con.MachineName)
		return EnergyHardware{}, false
	}
	s.cacheHardware(con.MachineName, hardware, now)
	return hardware, true
}

func energyHardwareFromProto(pb *infoGrpc.PowerHardware) (EnergyHardware, bool) {
	sockets := pb.GetSockets()
	if len(sockets) == 0 {
		return EnergyHardware{}, false
	}
	hardware := EnergyHardware{RAMMB: pb.GetRamMb()}
	for _, socket := range sockets {
		if socket == nil {
			continue
		}
		hardware.Sockets = append(hardware.Sockets, EnergySocket{
			Model: socket.GetModel(),
			Cores: int(socket.GetCores()),
		})
	}
	for _, disk := range pb.GetDisks() {
		if disk == nil {
			continue
		}
		switch disk.GetKind() {
		case "hdd":
			hardware.HDDs++
		case "ssd":
			hardware.SSDs++
		case "nvme":
			hardware.NVMes++
		}
	}
	return hardware, len(hardware.Sockets) > 0
}

func (s *EnergyService) cachedHardware(machine string, now time.Time) (EnergyHardware, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.hardware[machine]
	if !ok || now.Sub(entry.fetchedAt) >= energyHardwareTTL {
		return EnergyHardware{}, false
	}
	return entry.hardware, true
}

func (s *EnergyService) cacheHardware(machine string, hardware EnergyHardware, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hardware[machine] = energyHardwareCache{hardware: hardware, fetchedAt: now}
}

func (s *EnergyService) forgetHardware(machine string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.hardware, machine)
}

func (s *EnergyService) applyRetention(ctx context.Context, now time.Time) {
	month := now.Format(energyMonthFormat)
	s.mu.Lock()
	due := s.retentionMonth != month
	s.mu.Unlock()
	if !due {
		return
	}
	cutoff := now.AddDate(0, -energyRetentionMonths, 0).Format(energyMonthFormat)
	if err := db.DeleteEnergyUsageBefore(ctx, cutoff); err != nil {
		logger.Errorf("energy: retention before %s: %v", cutoff, err)
		return
	}
	s.mu.Lock()
	s.retentionMonth = month
	s.mu.Unlock()
}

func (s *EnergyService) beginTick() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ticking {
		return false
	}
	s.ticking = true
	return true
}

func (s *EnergyService) endTick() {
	s.mu.Lock()
	s.ticking = false
	s.mu.Unlock()
}

func (s *EnergyService) setLive(machines map[string]energyLiveMachine) {
	s.mu.Lock()
	s.machines = machines
	s.mu.Unlock()
}

func (s *EnergyService) liveSnapshot() map[string]energyLiveMachine {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]energyLiveMachine, len(s.machines))
	for name, machine := range s.machines {
		vms := make(map[string]float64, len(machine.vms))
		for vmName, watts := range machine.vms {
			vms[vmName] = watts
		}
		out[name] = energyLiveMachine{hostW: machine.hostW, cpuEstimate: machine.cpuEstimate, vms: vms}
	}
	return out
}

func (s *EnergyService) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

// energyTickHours is the time one sample stands for. It uses the real gap
// because a slow tick makes the ticker drop ticks; downtime is capped, not billed.
func energyTickHours(last, now time.Time) float64 {
	elapsed := now.Sub(last)
	if last.IsZero() || elapsed <= 0 {
		return energyLoopInterval.Hours()
	}
	return min(elapsed, energyMaxTickGap).Hours()
}

func energyCost(wh, price float64, priceSet bool) float64 {
	if !priceSet {
		return 0
	}
	return wh / energyWhPerKWh * price
}

// EnergySummary is the /energy/summary response. Live VM watts are a subset of
// the machine totals and are never added to them.
type EnergySummary struct {
	PricePerKWh *float64               `json:"pricePerKWh"`
	Totals      EnergyTotals           `json:"totals"`
	Machines    []EnergyMachineSummary `json:"machines"`
	VMs         []EnergyVMSummary      `json:"vms"`
	// Months is the last 12 months, oldest first, ending with the current one.
	// Months with no stored usage are present with zeros.
	Months []EnergyMonth `json:"months"`
}

type EnergyMonth struct {
	Month string  `json:"month"`
	KWh   float64 `json:"kWh"`
	Cost  float64 `json:"cost"`
}

type EnergyTotals struct {
	LiveWatts float64 `json:"liveWatts"`
	MonthKWh  float64 `json:"monthKWh"`
	MonthCost float64 `json:"monthCost"`
}

type EnergyMachineSummary struct {
	Machine     string  `json:"machine"`
	LiveWatts   float64 `json:"liveWatts"`
	MonthKWh    float64 `json:"monthKWh"`
	MonthCost   float64 `json:"monthCost"`
	CPUEstimate string  `json:"cpuEstimate"`
}

type EnergyVMSummary struct {
	Name      string  `json:"name"`
	LiveWatts float64 `json:"liveWatts"`
	MonthKWh  float64 `json:"monthKWh"`
	MonthCost float64 `json:"monthCost"`
}

// Summary merges the live snapshot with the current month's stored rows. VM
// rows are summed by name so a VM moved between hosts has one line.
func (s *EnergyService) Summary(ctx context.Context) (EnergySummary, error) {
	month := s.now().Format(energyMonthFormat)

	price, priceSet, err := db.GetEnergyPrice(ctx)
	if err != nil {
		return EnergySummary{}, fmt.Errorf("get energy price: %w", err)
	}
	rows, err := db.GetEnergyUsageForMonth(ctx, month)
	if err != nil {
		return EnergySummary{}, fmt.Errorf("get energy usage: %w", err)
	}

	machines := make(map[string]*EnergyMachineSummary)
	vmTotals := make(map[string]*EnergyVMSummary)
	machineFor := func(name string) *EnergyMachineSummary {
		machine, ok := machines[name]
		if !ok {
			machine = &EnergyMachineSummary{Machine: name}
			machines[name] = machine
		}
		return machine
	}
	vmFor := func(name string) *EnergyVMSummary {
		vm, ok := vmTotals[name]
		if !ok {
			vm = &EnergyVMSummary{Name: name}
			vmTotals[name] = vm
		}
		return vm
	}

	for _, row := range rows {
		if row.VM == "" {
			machine := machineFor(row.Machine)
			machine.MonthKWh += row.Wh / energyWhPerKWh
			machine.MonthCost += row.Cost
			continue
		}
		vm := vmFor(row.VM)
		vm.MonthKWh += row.Wh / energyWhPerKWh
		vm.MonthCost += row.Cost
	}

	for name, live := range s.liveSnapshot() {
		machine := machineFor(name)
		machine.LiveWatts += live.hostW
		machine.CPUEstimate = live.cpuEstimate
		for vmName, watts := range live.vms {
			vmFor(vmName).LiveWatts += watts
		}
	}

	summary := EnergySummary{
		Totals:   EnergyTotals{},
		Machines: make([]EnergyMachineSummary, 0, len(machines)),
		VMs:      make([]EnergyVMSummary, 0, len(vmTotals)),
	}
	if priceSet {
		value := price
		summary.PricePerKWh = &value
	}
	for _, machine := range machines {
		summary.Totals.LiveWatts += machine.LiveWatts
		summary.Totals.MonthKWh += machine.MonthKWh
		summary.Totals.MonthCost += machine.MonthCost
		summary.Machines = append(summary.Machines, *machine)
	}
	for _, vm := range vmTotals {
		summary.VMs = append(summary.VMs, *vm)
	}
	// Day 1 keeps the month arithmetic away from end-of-month overflow.
	now := s.now()
	summary.Months = make([]EnergyMonth, energyHistoryMonths)
	for i := range summary.Months {
		offset := energyHistoryMonths - 1 - i
		summary.Months[i].Month = time.Date(now.Year(), now.Month()-time.Month(offset), 1, 0, 0, 0, 0, now.Location()).Format(energyMonthFormat)
	}
	history, err := db.GetEnergyMonthlyTotals(ctx, summary.Months[0].Month)
	if err != nil {
		return EnergySummary{}, fmt.Errorf("get energy history: %w", err)
	}
	for _, row := range history {
		for i := range summary.Months {
			if summary.Months[i].Month == row.Month {
				summary.Months[i].KWh = row.Wh / energyWhPerKWh
				summary.Months[i].Cost = row.Cost
			}
		}
	}

	sort.Slice(summary.Machines, func(i, j int) bool {
		return summary.Machines[i].Machine < summary.Machines[j].Machine
	})
	sort.Slice(summary.VMs, func(i, j int) bool {
		return summary.VMs[i].Name < summary.VMs[j].Name
	})
	return summary, nil
}

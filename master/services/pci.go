package services

import (
	"512SvMan/db"
	"512SvMan/nots"
	pciClient "512SvMan/pci"
	"512SvMan/protocol"
	virshClient "512SvMan/virsh"
	"context"
	"fmt"
	"strings"

	pciGrpc "github.com/Maruqes/512SvMan/api/proto/pci"
	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
	"github.com/Maruqes/512SvMan/logger"
)

type PCIService struct{}

func (s *PCIService) machineConnection(machineName string) (*protocol.ConnectionsStruct, error) {
	machineName = strings.TrimSpace(machineName)
	if machineName == "" {
		return nil, fmt.Errorf("machine name is required")
	}

	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return nil, fmt.Errorf("machine %s is not connected", machineName)
	}

	return machine, nil
}

func (s *PCIService) vmOnMachine(machine *protocol.ConnectionsStruct, vmName string) (*grpcVirsh.Vm, error) {
	vmName = strings.TrimSpace(vmName)
	if vmName == "" {
		return nil, fmt.Errorf("vm name is required")
	}

	vm, err := virshClient.GetVmByName(machine.Connection, &grpcVirsh.GetVmByNameRequest{Name: vmName})
	if err != nil {
		return nil, fmt.Errorf("vm %s not found on machine %s: %w", vmName, machine.MachineName, err)
	}
	if vm == nil {
		return nil, fmt.Errorf("vm %s not found on machine %s", vmName, machine.MachineName)
	}

	if vmMachine := strings.TrimSpace(vm.GetMachineName()); vmMachine != "" && !strings.EqualFold(vmMachine, machine.MachineName) {
		return nil, fmt.Errorf("vm %s belongs to machine %s, not %s", vmName, vmMachine, machine.MachineName)
	}

	return vm, nil
}

func findGPUByReference(gpus []*pciGrpc.HostGPU, gpuRef string) *pciGrpc.HostGPU {
	gpuRef = strings.TrimSpace(gpuRef)
	if gpuRef == "" {
		return nil
	}

	refLower := strings.ToLower(gpuRef)
	for _, gpu := range gpus {
		if gpu == nil {
			continue
		}

		candidates := []string{
			strings.TrimSpace(gpu.GetAddress()),
			strings.TrimSpace(gpu.GetNodeName()),
			strings.TrimSpace(gpu.GetPath()),
		}

		for _, c := range candidates {
			if c == "" {
				continue
			}
			cLower := strings.ToLower(c)
			if cLower == refLower || strings.HasSuffix(cLower, "/"+refLower) || strings.HasSuffix(cLower, refLower) {
				return gpu
			}
		}
	}

	return nil
}

func (s *PCIService) ListHostGPUs(ctx context.Context, machineName string) (*pciGrpc.HostGPUList, error) {
	machine, err := s.machineConnection(machineName)
	if err != nil {
		return nil, err
	}

	list, err := pciClient.ListHostGPUs(ctx, machine.Connection)
	if err != nil {
		return nil, err
	}
	return withDesiredModes(ctx, machine.MachineName, list), nil
}

func (s *PCIService) ListHostGPUsWithIOMMU(ctx context.Context, machineName string) (*pciGrpc.HostGPUList, error) {
	machine, err := s.machineConnection(machineName)
	if err != nil {
		return nil, err
	}

	list, err := pciClient.ListHostGPUsWithIOMMU(ctx, machine.Connection)
	if err != nil {
		return nil, err
	}
	return withDesiredModes(ctx, machine.MachineName, list), nil
}

func (s *PCIService) ListVMGPUs(ctx context.Context, machineName, vmName string) (*pciGrpc.VMGPUList, error) {
	machine, err := s.machineConnection(machineName)
	if err != nil {
		return nil, err
	}

	if _, err := s.vmOnMachine(machine, vmName); err != nil {
		return nil, err
	}

	return pciClient.ListVMGPUs(ctx, machine.Connection, vmName)
}

// SetGPUMode switches a GPU between none, host and vm mode and stores the choice. The host
// goes first: a refused switch changes nothing. If the host switched but the row could not
// be saved, the error is returned and the next reconcile puts the GPU back in its stored mode.
func (s *PCIService) SetGPUMode(ctx context.Context, machineName, gpuRef, mode, vmName string) error {
	machine, err := s.machineConnection(machineName)
	if err != nil {
		return err
	}

	hostGPUs, err := pciClient.ListHostGPUs(ctx, machine.Connection)
	if err != nil {
		return err
	}
	gpu := findGPUByReference(hostGPUs.GetGpus(), gpuRef)
	if gpu == nil {
		return fmt.Errorf("gpu %s not found on machine %s", strings.TrimSpace(gpuRef), machine.MachineName)
	}

	mode = strings.ToLower(strings.TrimSpace(mode))
	vmName = strings.TrimSpace(vmName)
	if mode != gpuModeVM {
		vmName = ""
	}
	if _, err := pciClient.SetGPUMode(ctx, machine.Connection, gpu.GetAddress(), mode, vmName); err != nil {
		return err
	}
	if err := db.SaveGPUMode(ctx, db.GPUMode{MachineName: machine.MachineName, GPUAddress: gpu.GetAddress(), Mode: mode, VMName: vmName}); err != nil {
		return fmt.Errorf("gpu %s switched to %s but the mode was not saved: %w", gpu.GetAddress(), mode, err)
	}
	return nil
}

// ReconcileGPUs applies the stored mode of every GPU of a machine. It runs when the slave
// connects, before VMs autostart: driver pins do not survive a reboot. A GPU that cannot be
// switched is left as it is, keeps its stored mode and gets its error recorded and notified.
func (s *PCIService) ReconcileGPUs(ctx context.Context, machineName string) error {
	machine, err := s.machineConnection(machineName)
	if err != nil {
		return err
	}
	hostGPUs, err := pciClient.ListHostGPUs(ctx, machine.Connection)
	if err != nil {
		return fmt.Errorf("list gpus of %s: %w", machine.MachineName, err)
	}
	stored, err := db.GetGPUModes(ctx, machine.MachineName)
	if err != nil {
		return fmt.Errorf("read gpu modes of %s: %w", machine.MachineName, err)
	}

	for _, gpu := range hostGPUs.GetGpus() {
		address := gpu.GetAddress()
		row, ok := stored[address]
		if !ok {
			var warning string
			row = db.GPUMode{MachineName: machine.MachineName, GPUAddress: address}
			row.Mode, row.VMName, warning = adoptGPUMode(gpu)
			if warning != "" {
				s.warnGPU(ctx, machine.MachineName, address, warning)
			}
		}

		row.LastError = ""
		if _, err := pciClient.SetGPUMode(ctx, machine.Connection, address, row.Mode, row.VMName); err != nil {
			row.LastError = err.Error()
			s.warnGPU(ctx, machine.MachineName, address, fmt.Sprintf("could not apply mode %s: %v", gpuModeLabel(row), err))
		}
		if err := db.SaveGPUMode(ctx, row); err != nil {
			logger.Errorf("save gpu mode of %s on %s: %v", address, machine.MachineName, err)
		}
	}
	return nil
}

const (
	gpuModeNone = "none"
	gpuModeHost = "host"
	gpuModeVM   = "vm"
)

// adoptGPUMode picks the mode of a GPU seen for the first time from its current state, so an
// upgrade changes nothing: in one VM's definition -> that VM, bound to a host driver -> host,
// otherwise none. A GPU defined in several VMs is ambiguous and becomes none.
func adoptGPUMode(gpu *pciGrpc.HostGPU) (mode, vmName, warning string) {
	switch vms := gpu.GetAttachedToVms(); {
	case len(vms) == 1:
		return gpuModeVM, vms[0], ""
	case len(vms) > 1:
		return gpuModeNone, "", fmt.Sprintf("it is defined in several VMs (%s); it was set to mode none and removed from them", strings.Join(vms, ", "))
	}
	if gpu.GetActualMode() == gpuModeHost {
		return gpuModeHost, "", ""
	}
	return gpuModeNone, "", ""
}

func gpuModeLabel(row db.GPUMode) string {
	if row.Mode == gpuModeVM {
		return gpuModeVM + ":" + row.VMName
	}
	return row.Mode
}

func (s *PCIService) warnGPU(ctx context.Context, machineName, address, message string) {
	logger.Warnf("gpu %s on %s: %s", address, machineName, message)
	if err := nots.Notify(ctx, nots.Notification{
		Event:    "system.gpu_mode",
		Severity: nots.SeverityWarning,
		Machine:  machineName,
		Title:    "GPU mode not applied",
		Body:     fmt.Sprintf("GPU %s on %s: %s", address, machineName, message),
		URL:      "/gpus",
		DedupKey: "gpu_mode:" + machineName + ":" + address,
	}); err != nil {
		logger.Errorf("notify gpu mode of %s on %s: %v", address, machineName, err)
	}
}

// withDesiredModes adds the stored mode and last reconcile error to a GPU inventory.
func withDesiredModes(ctx context.Context, machineName string, list *pciGrpc.HostGPUList) *pciGrpc.HostGPUList {
	stored, err := db.GetGPUModes(ctx, machineName)
	if err != nil {
		// The inventory is still useful without the stored modes.
		logger.Errorf("read gpu modes of %s: %v", machineName, err)
		return list
	}
	for _, gpu := range list.GetGpus() {
		if row, ok := stored[gpu.GetAddress()]; ok {
			gpu.DesiredMode, gpu.DesiredVm, gpu.LastError = row.Mode, row.VMName, row.LastError
		}
	}
	return list
}

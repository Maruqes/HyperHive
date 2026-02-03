package services

import (
	pciClient "512SvMan/pci"
	"512SvMan/protocol"
	virshClient "512SvMan/virsh"
	"context"
	"fmt"
	"strings"

	pciGrpc "github.com/Maruqes/512SvMan/api/proto/pci"
	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
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

func ensureVMShutoff(vm *grpcVirsh.Vm, machineName string) error {
	if vm == nil {
		return fmt.Errorf("vm is nil")
	}
	if vm.GetState() != grpcVirsh.VmState_SHUTOFF {
		return fmt.Errorf("vm %s on machine %s must be shut down (current state: %s)", vm.GetName(), machineName, vm.GetState().String())
	}
	return nil
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

func (s *PCIService) ensureAttachedVMsAreShutoff(ctx context.Context, machine *protocol.ConnectionsStruct, gpuRef string) error {
	hostGPUs, err := pciClient.ListHostGPUs(ctx, machine.Connection)
	if err != nil {
		return err
	}

	target := findGPUByReference(hostGPUs.GetGpus(), gpuRef)
	if target == nil {
		return nil
	}

	for _, vmName := range target.GetAttachedToVms() {
		vmName = strings.TrimSpace(vmName)
		if vmName == "" {
			continue
		}

		vm, err := s.vmOnMachine(machine, vmName)
		if err != nil {
			return err
		}
		if vm.GetState() != grpcVirsh.VmState_SHUTOFF {
			return fmt.Errorf("gpu %s is attached to vm %s and this vm is not shut down", gpuRef, vmName)
		}
	}

	return nil
}

func (s *PCIService) ListHostGPUs(ctx context.Context, machineName string) (*pciGrpc.HostGPUList, error) {
	machine, err := s.machineConnection(machineName)
	if err != nil {
		return nil, err
	}

	return pciClient.ListHostGPUs(ctx, machine.Connection)
}

func (s *PCIService) ListHostGPUsWithIOMMU(ctx context.Context, machineName string) (*pciGrpc.HostGPUList, error) {
	machine, err := s.machineConnection(machineName)
	if err != nil {
		return nil, err
	}

	return pciClient.ListHostGPUsWithIOMMU(ctx, machine.Connection)
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

func (s *PCIService) AttachGPUToVM(ctx context.Context, machineName, vmName, gpuRef string) (*pciGrpc.OkResponse, error) {
	machine, err := s.machineConnection(machineName)
	if err != nil {
		return nil, err
	}

	vm, err := s.vmOnMachine(machine, vmName)
	if err != nil {
		return nil, err
	}
	if err := ensureVMShutoff(vm, machine.MachineName); err != nil {
		return nil, err
	}

	return pciClient.AttachGPUToVM(ctx, machine.Connection, vm.GetName(), gpuRef)
}

func (s *PCIService) DetachGPUFromVM(ctx context.Context, machineName, vmName, gpuRef string) (*pciGrpc.OkResponse, error) {
	machine, err := s.machineConnection(machineName)
	if err != nil {
		return nil, err
	}

	vm, err := s.vmOnMachine(machine, vmName)
	if err != nil {
		return nil, err
	}
	if err := ensureVMShutoff(vm, machine.MachineName); err != nil {
		return nil, err
	}

	return pciClient.DetachGPUFromVM(ctx, machine.Connection, vm.GetName(), gpuRef)
}

func (s *PCIService) ReturnGPUToHost(ctx context.Context, machineName, gpuRef string) (*pciGrpc.OkResponse, error) {
	machine, err := s.machineConnection(machineName)
	if err != nil {
		return nil, err
	}

	if err := s.ensureAttachedVMsAreShutoff(ctx, machine, gpuRef); err != nil {
		return nil, err
	}

	return pciClient.ReturnGPUToHost(ctx, machine.Connection, gpuRef)
}
